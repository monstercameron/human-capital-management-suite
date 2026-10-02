package application

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
)

// ErrVoiceOutsideServiceBarred marks a call that the channel or the workspace
// does not allow to leave the deployment. Nothing was sent.
var ErrVoiceOutsideServiceBarred = errors.New("application: this conversation never uses an outside service")

// VoiceExternalSettings answers one question for the audio port: may this
// conversation's audio or text be sent to an outside service. The answer comes
// from the settings the administration page writes ("Never use an outside
// service" on the channel, the workspace's external switch), the same ones the
// translation worker honours.
type VoiceExternalSettings interface {
	ExternalAllowed(ctx context.Context, tenant, conversation string) (bool, error)
}

// ChatlangVoiceExternal reads those settings from the translation governance.
type ChatlangVoiceExternal struct{ Governance *ChatlangGovernance }

// ExternalAllowed fails closed: a setting that cannot be read bars the call.
func (c ChatlangVoiceExternal) ExternalAllowed(ctx context.Context, tenant, conversation string) (bool, error) {
	if c.Governance == nil || tenant == "" || conversation == "" {
		return false, chatlang.ErrUnavailable
	}
	workspace, channel, err := c.Governance.settings(ctx, tenant, conversation)
	if err != nil {
		return false, err
	}
	return chatlang.ExternalOK(workspace, channel), nil
}

// VoiceBudgetPolicy is the ceiling for audio spend, in millionths of the
// currency: a tenant's month and one person's day.
type VoiceBudgetPolicy struct{ TenantMonthlyMicros, UserDailyMicros int64 }

// DefaultVoiceBudgetPolicy is five units a month per workspace and one a day
// per person; both are configurable.
func DefaultVoiceBudgetPolicy() VoiceBudgetPolicy {
	return VoiceBudgetPolicy{TenantMonthlyMicros: 5_000_000, UserDailyMicros: 1_000_000}
}

// VoiceAudioGuard is the agentmodel.AudioGuard of the product: it applies the
// outside-service switch before any byte leaves and meters every call through
// the same agentbudget ledger type the typed model gateway reserves against.
type VoiceAudioGuard struct {
	External VoiceExternalSettings
	Ledger   *agentbudget.Ledger

	mu       sync.Mutex
	reserved map[string]*agentbudget.Reservation
}

// NewVoiceAudioGuard builds the guard with a dedicated ledger, so voice spend
// cannot consume an agent's ceilings and an agent cannot consume voice's.
func NewVoiceAudioGuard(external VoiceExternalSettings, policy VoiceBudgetPolicy, now func() time.Time) (*VoiceAudioGuard, error) {
	if external == nil || policy.TenantMonthlyMicros <= 0 || policy.UserDailyMicros <= 0 {
		return nil, agentmodel.ErrAudioNotConfigured
	}
	huge := int64(1_000_000_000_000)
	limit := func(spend int64) agentbudget.Limits {
		return agentbudget.Limits{Steps: huge, Tokens: huge, WallClock: 1_000_000 * time.Hour, SpendMicros: spend}
	}
	ledger, err := agentbudget.NewWithClock(agentbudget.Policy{TaskDefault: limit(policy.UserDailyMicros), UserDaily: limit(policy.UserDailyMicros), TenantMonthly: limit(policy.TenantMonthlyMicros)}, now)
	if err != nil {
		return nil, err
	}
	return &VoiceAudioGuard{External: external, Ledger: ledger, reserved: map[string]*agentbudget.Reservation{}}, nil
}

var _ agentmodel.AudioGuard = (*VoiceAudioGuard)(nil)

// Admit refuses a call the conversation does not allow outside, then reserves
// its estimated cost.
func (g *VoiceAudioGuard) Admit(ctx context.Context, call agentmodel.AudioCall) error {
	if g == nil || g.External == nil || g.Ledger == nil || call.CallID == "" || call.TenantID == "" || call.ConversationID == "" || call.Actor == "" {
		return agentmodel.ErrAudioNotConfigured
	}
	allowed, err := g.External.ExternalAllowed(ctx, call.TenantID, call.ConversationID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrVoiceOutsideServiceBarred, err)
	}
	if !allowed {
		return ErrVoiceOutsideServiceBarred
	}
	// The worker's calls are charged to the workspace as a whole; a person's
	// daily ceiling applies to the person who asked to listen.
	task := agentbudget.TaskSpec{ID: call.CallID, TenantID: call.TenantID, UserID: call.Actor, Limit: agentbudget.Limits{Steps: 2, Tokens: 1, WallClock: time.Hour, SpendMicros: max(call.EstimatedCostMicros, 1)}}
	if err = g.Ledger.OpenTask(task); err != nil {
		return err
	}
	reservation, err := g.Ledger.Reserve(ctx, agentbudget.Request{TaskID: call.CallID, StepID: string(call.Kind), Fingerprint: string(call.Kind) + ":" + call.CallID, Estimate: agentbudget.Usage{Steps: 1, Tokens: 1, WallClock: time.Second, SpendMicros: max(call.EstimatedCostMicros, 1)}})
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.reserved[call.CallID] = reservation
	g.mu.Unlock()
	return nil
}

// Settle records what a call cost and releases the rest of its reservation.
func (g *VoiceAudioGuard) Settle(_ context.Context, call agentmodel.AudioCall, costMicros int64) error {
	g.mu.Lock()
	reservation := g.reserved[call.CallID]
	delete(g.reserved, call.CallID)
	g.mu.Unlock()
	if reservation == nil {
		return nil
	}
	cost := minInt64(max(costMicros, 0), max(call.EstimatedCostMicros, 1))
	return reservation.Settle(agentbudget.Usage{Steps: 1, Tokens: 1, WallClock: time.Second, SpendMicros: cost})
}
