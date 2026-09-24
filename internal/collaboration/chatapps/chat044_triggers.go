package chatapps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

const triggerBudgetWindow = time.Minute

// TenantTriggerBudget limits admitted autonomous work for one tenant. Both
// limits are required so a caller cannot accidentally configure an unbounded
// lane by leaving one dimension at its zero value.
type TenantTriggerBudget struct {
	RatePerMinute int   `json:"rate_per_minute"`
	CostPerMinute int64 `json:"cost_per_minute"`
}

func (b TenantTriggerBudget) validate() error {
	if b.RatePerMinute <= 0 || b.CostPerMinute <= 0 {
		return ErrInvalid
	}
	return nil
}

type triggerBudgetEvent struct {
	at      time.Time
	cost    int64
	key     string
	install string
}

type triggerBudgetState struct {
	policy   TenantTriggerBudget
	events   []triggerBudgetEvent
	installs map[string]bool
}

// TriggerBudget owns the concurrency-safe, one-minute admission window for
// autonomous chat triggers. Durable deployments should persist equivalent
// policy and admission state in the chat store before enabling cross-process
// dispatch; this value is the deterministic single-process gate.
type TriggerBudget struct {
	mu    sync.Mutex
	now   func() time.Time
	state map[string]*triggerBudgetState
}

func NewTriggerBudget(now func() time.Time) *TriggerBudget {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &TriggerBudget{now: now, state: map[string]*triggerBudgetState{}}
}

// ConfigureTenant installs an explicit ceiling. Changing the ceiling retains
// usage already admitted in the current minute, so lowering a limit cannot
// reset consumed budget.
func (b *TriggerBudget) ConfigureTenant(tenant string, policy TenantTriggerBudget) error {
	if b == nil || tenant == "" || policy.validate() != nil {
		return ErrInvalid
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == nil {
		b.state = map[string]*triggerBudgetState{}
	}
	s := b.state[tenant]
	if s == nil {
		s = &triggerBudgetState{installs: map[string]bool{}}
		b.state[tenant] = s
	}
	s.policy = policy
	return nil
}

func (b *TriggerBudget) setInstallationStopped(tenant, installation string, stopped bool) error {
	if b == nil || tenant == "" || installation == "" {
		return ErrInvalid
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.state[tenant]
	if s == nil || s.policy.validate() != nil {
		return ErrBudget
	}
	s.installs[installation] = stopped
	return nil
}

func (b *TriggerBudget) admit(t Trigger, manifest AgentManifest, now time.Time, recordSeen func(time.Time) error) error {
	if b == nil || t.Tenant == "" || t.AgentInstallation == "" || t.IdempotencyKey == "" ||
		len(t.IdempotencyKey) > 256 || t.Depth < 0 || t.Cost < 0 || now.IsZero() || recordSeen == nil {
		return ErrInvalid
	}
	if manifest.MaxDepth <= 0 || t.Depth >= manifest.MaxDepth {
		return ErrLoop
	}
	if manifest.RatePerMinute <= 0 || manifest.CostCeiling < 0 ||
		manifest.CostCeiling > 0 && t.Cost > manifest.CostCeiling {
		return ErrBudget
	}
	switch TriggerSource(t.Source) {
	case TriggerMention, TriggerDirectMessage, TriggerEvent:
	default:
		return ErrDenied
	}
	declared := false
	for _, source := range manifest.Triggers {
		if string(source) == t.Source {
			declared = true
			break
		}
	}
	if !declared {
		return ErrDenied
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == nil {
		return ErrBudget
	}
	s := b.state[t.Tenant]
	if s == nil || s.policy.validate() != nil {
		return ErrBudget
	}
	if s.installs[t.AgentInstallation] {
		return ErrSuspended
	}
	cutoff := now.Add(-triggerBudgetWindow)
	active := s.events[:0]
	var cost int64
	var installCount int
	duplicate := false
	for _, event := range s.events {
		if event.at.Before(cutoff) {
			continue
		}
		active = append(active, event)
		if cost > int64(^uint64(0)>>1)-event.cost {
			cost = int64(^uint64(0) >> 1)
		} else {
			cost += event.cost
		}
		if event.install == t.AgentInstallation {
			installCount++
		}
		if event.install == t.AgentInstallation && event.key == t.IdempotencyKey {
			duplicate = true
		}
	}
	s.events = active
	if duplicate {
		return ErrReplay
	}
	if len(active) >= s.policy.RatePerMinute || cost > s.policy.CostPerMinute-t.Cost {
		return ErrBudget
	}
	if manifest.RatePerMinute > 0 && installCount >= manifest.RatePerMinute {
		return ErrBudget
	}
	if err := recordSeen(now); err != nil {
		return err
	}
	s.events = append(s.events, triggerBudgetEvent{at: now, cost: t.Cost, key: t.IdempotencyKey, install: t.AgentInstallation})
	return nil
}

// AdmitTriggerWithBudget is the bounded admission seam for autonomous chat
// dispatch. Existing installation, tenant, conversation, source, depth, and
// per-invocation cost checks run first; the trigger is admitted to the shared
// tenant and installation windows only after those checks pass.
func (s *Service) AdmitTriggerWithBudget(ctx context.Context, t Trigger, budget *TriggerBudget) error {
	if s == nil || s.Repo == nil || budget == nil {
		return ErrInvalid
	}
	if err := s.AdmitTrigger(ctx, t); err != nil {
		return err
	}
	installation, err := s.Repo.Get(ctx, t.AgentInstallation)
	if err != nil {
		return err
	}
	if installation.ID != t.AgentInstallation || installation.Status != Active || installation.Tenant != t.Tenant || installation.Conversation != t.Conversation ||
		installation.AppID == "" || installation.Manifest.AppID != installation.AppID || installation.Manifest.Version != installation.Version || installation.Manifest.Agent == nil {
		return ErrDenied
	}
	now := time.Now().UTC()
	if budget.now != nil {
		now = budget.now().UTC()
	}
	return budget.admit(t, *installation.Manifest.Agent, now, func(at time.Time) error {
		return s.Repo.MarkSeen(ctx, triggerReplayID(t), at)
	})
}

func triggerReplayID(t Trigger) string {
	encoded, _ := json.Marshal([3]string{t.Tenant, t.AgentInstallation, t.IdempotencyKey})
	digest := sha256.Sum256(encoded)
	return "agent-trigger:" + hex.EncodeToString(digest[:])
}

// SetTriggerKillSwitch pauses or resumes one installed agent after confirming
// the caller is a manager of the installation's tenant and conversation.
func (s *Service) SetTriggerKillSwitch(ctx context.Context, actor Actor, installationID string, budget *TriggerBudget, stopped bool) error {
	if s == nil || s.Repo == nil || s.Authority == nil || budget == nil || actor.Principal == "" {
		return ErrDenied
	}
	installation, err := s.Repo.Get(ctx, installationID)
	if err != nil {
		return err
	}
	if installation.Tenant != actor.Tenant || installation.Conversation != actor.Conversation || installation.Manifest.Agent == nil {
		return ErrDenied
	}
	if err := s.Authority.CanManageApp(ctx, actor, installation.AppID); err != nil {
		return ErrDenied
	}
	if err := budget.setInstallationStopped(installation.Tenant, installation.ID, stopped); err != nil {
		return err
	}
	return nil
}
