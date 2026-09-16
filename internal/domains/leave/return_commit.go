// ReturnFromLeave commit records the atomic local return and reconciles
// the restored effects without trusting them (LEAVE-013).
//
// The local transaction appends LeaveEnded plus availability restoration
// exactly once, preserves every active restriction, queues the
// normal-pay/benefit/schedule/access effects and closes only under a
// versioned obligation policy. Provider observations are reconciled into
// honest dimensions: a failed provider never rewrites local return truth.
package leave

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Return commit steps: every one of them commits once, or none do.
var returnCommitSteps = []string{
	"leave-ended-revision", "availability-restoration", "restriction-carryover",
	"ledger-events", "projections", "effect-outbox", "obligation-policy",
}

// Return effect systems: the closed queue the return must project.
var returnEffectSystems = []string{"payroll", "benefits", "schedule", "access"}

// ReturnCommitInput carries the readiness proof, the preserved
// restrictions, the queued effect systems, the versioned obligation
// policy, the typed step receipts and the observed return effects.
// Employment state is asserted, never mutated: anything but ACTIVE
// refuses.
type ReturnCommitInput struct {
	IdempotencyKey   string
	EmploymentState  string
	Readiness        ReturnToWorkResult
	Restrictions     []StructuredRestriction
	QueuedEffects    []string
	ObligationPolicy string
	StepReceipts     map[string]string
	ReturnEffects    []ExternalEffect
}

// ReturnRecord is the atomic return: business state becomes LEAVE_ENDED
// independently of external consistency, and historical revisions stay
// append-only because the record only references them.
type ReturnRecord struct {
	CommitID              string
	BusinessState         string
	Steps                 []string
	PreservedRestrictions []string
	QueuedEffects         []string
	ObligationPolicy      string
	ConsistencyState      string
	ObligationState       string
	Effects               []ExternalEffect
	RepairTargets         []string
	Digest                string
}

func returnDigest(key string, input ReturnCommitInput, consistency, obligation string, repair []string) string {
	ordered := append([]ExternalEffect(nil), input.ReturnEffects...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].System < ordered[j].System })
	parts := []string{"leave-return-commit", key, input.Readiness.Digest, input.ObligationPolicy, consistency, obligation}
	parts = append(parts, repair...)
	restrictions := append([]StructuredRestriction(nil), input.Restrictions...)
	sort.Slice(restrictions, func(i, j int) bool { return restrictions[i].ID < restrictions[j].ID })
	for _, r := range restrictions {
		parts = append(parts, strings.Join([]string{r.ID, r.Code, r.Source}, "\x01"))
	}
	queued := append([]string(nil), input.QueuedEffects...)
	sort.Strings(queued)
	parts = append(parts, queued...)
	for _, step := range returnCommitSteps {
		parts = append(parts, step+"="+input.StepReceipts[step])
	}
	for _, effect := range ordered {
		parts = append(parts, strings.Join([]string{effect.System, fmt.Sprint(effect.Mandatory), effect.State, effect.Observation, effect.Owner}, "\x01"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ReturnCommitter is the owning atomic return commit: one mutex-guarded
// registry. Failpoints yield nothing: a return never exists without its
// availability restoration, restriction carryover, event and outbox
// counterparts.
type ReturnCommitter struct {
	mu      sync.Mutex
	records map[string]ReturnRecord
}

// NewReturnCommitter starts an empty return committer.
func NewReturnCommitter() *ReturnCommitter {
	return &ReturnCommitter{records: make(map[string]ReturnRecord)}
}

// Commit atomically records one return. Readiness must verify and be
// READY or READY_WITH_RESTRICTIONS; every restriction the readiness
// follow-ups name must be carried; the four return effects must be
// queued; the obligation policy must be versioned; every step receipt
// must be present. Observed effects reconcile into dimensions without
// touching local return truth; repeats return the identical record.
func (committer *ReturnCommitter) Commit(input ReturnCommitInput, inject func(string) error) (ReturnRecord, error) {
	if committer == nil {
		return ReturnRecord{}, fmt.Errorf("leave: nil return committer")
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return ReturnRecord{}, fmt.Errorf("leave: return commit needs an idempotency key")
	}
	if input.EmploymentState != "ACTIVE" {
		return ReturnRecord{}, fmt.Errorf("leave: return never mutates employment state %q", input.EmploymentState)
	}
	if err := input.Readiness.Verify(); err != nil {
		return ReturnRecord{}, fmt.Errorf("leave: return before readiness: %v", err)
	}
	if input.Readiness.Result != ReadinessReady && input.Readiness.Result != ReadinessReadyWithRestrictions {
		return ReturnRecord{}, fmt.Errorf("leave: return before readiness %q", input.Readiness.Result)
	}
	required := map[string]bool{}
	for _, followUp := range input.Readiness.FollowUps {
		if followUp.Kind == FollowUpEstablishWorkRestriction {
			required[followUp.RestrictionID] = false
		}
	}
	for _, r := range input.Restrictions {
		if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Code) == "" {
			return ReturnRecord{}, fmt.Errorf("leave: preserved restriction carries an id and a capability code")
		}
		switch strings.TrimSpace(r.Source) {
		case "MEDICAL", "SAFETY":
		default:
			return ReturnRecord{}, fmt.Errorf("leave: restriction source %q is not governed", r.Source)
		}
		if _, wanted := required[r.ID]; !wanted {
			return ReturnRecord{}, fmt.Errorf("leave: restriction %q is not an active readiness restriction", r.ID)
		}
		required[r.ID] = true
	}
	for id, kept := range required {
		if !kept {
			return ReturnRecord{}, fmt.Errorf("leave: return discards active restriction %q", id)
		}
	}
	if len(input.QueuedEffects) != len(returnEffectSystems) {
		return ReturnRecord{}, fmt.Errorf("leave: return queues normal-pay, benefit, schedule and access effects")
	}
	queued := map[string]bool{}
	for _, system := range input.QueuedEffects {
		queued[system] = true
	}
	for _, system := range returnEffectSystems {
		if !queued[system] {
			return ReturnRecord{}, fmt.Errorf("leave: return queues the %s effect", system)
		}
	}
	policy := strings.TrimSpace(input.ObligationPolicy)
	if !strings.HasPrefix(policy, "policy:v") || len(policy) <= len("policy:v") {
		return ReturnRecord{}, fmt.Errorf("leave: return closes only under a versioned obligation policy")
	}
	for _, c := range policy[len("policy:v"):] {
		if c < '0' || c > '9' {
			return ReturnRecord{}, fmt.Errorf("leave: return closes only under a versioned obligation policy")
		}
	}
	for _, step := range returnCommitSteps {
		if strings.TrimSpace(input.StepReceipts[step]) == "" {
			return ReturnRecord{}, fmt.Errorf("leave: return step %s has no receipt", step)
		}
	}
	if len(input.ReturnEffects) != len(returnEffectSystems) {
		return ReturnRecord{}, fmt.Errorf("leave: return observes every queued effect system")
	}
	seen := map[string]bool{}
	effects := make([]ExternalEffect, 0, len(input.ReturnEffects))
	for _, effect := range input.ReturnEffects {
		if !queued[effect.System] || seen[effect.System] {
			return ReturnRecord{}, fmt.Errorf("leave: return effect %q is not a queued effect", effect.System)
		}
		seen[effect.System] = true
		if strings.TrimSpace(effect.Owner) == "" {
			return ReturnRecord{}, fmt.Errorf("leave: return effect %s needs a repair owner", effect.System)
		}
		switch effect.State {
		case EffectPass, EffectUnknown, EffectFailed:
		default:
			return ReturnRecord{}, fmt.Errorf("leave: return effect %s state %q is not observed truth", effect.System, effect.State)
		}
		resolved := effect
		if effect.ProviderAccepted && strings.TrimSpace(effect.Observation) == "" {
			resolved.State = EffectUnknown
		}
		effects = append(effects, resolved)
	}
	consistency := "OK"
	obligation := "SATISFIED"
	var repair []string
	for _, effect := range effects {
		switch effect.State {
		case EffectFailed:
			consistency = "DEGRADED"
			repair = append(repair, effect.System)
			if effect.Mandatory {
				obligation = "PENDING"
			}
		case EffectUnknown:
			consistency = "DEGRADED"
			repair = append(repair, effect.System+":observe")
			if effect.Mandatory {
				obligation = "PENDING"
			}
		}
	}
	sort.Strings(repair)
	sort.Slice(effects, func(i, j int) bool { return effects[i].System < effects[j].System })

	committer.mu.Lock()
	defer committer.mu.Unlock()
	if prior, done := committer.records[input.IdempotencyKey]; done {
		return prior, nil
	}
	fail := func(step string) error {
		if inject == nil {
			return nil
		}
		return inject(step)
	}
	for _, step := range returnCommitSteps {
		if err := fail(step); err != nil {
			return ReturnRecord{}, fmt.Errorf("leave: rolled back at %s: %v", step, err)
		}
	}
	preserved := make([]string, 0, len(input.Restrictions))
	for _, r := range input.Restrictions {
		preserved = append(preserved, r.ID)
	}
	sort.Strings(preserved)
	sum := sha256.Sum256([]byte("leave-return-commit-id\x00" + input.IdempotencyKey))
	record := ReturnRecord{
		CommitID: "sha256:" + hex.EncodeToString(sum[:]), BusinessState: BusinessLeaveEnded,
		Steps: append([]string(nil), returnCommitSteps...), PreservedRestrictions: preserved,
		QueuedEffects: append([]string(nil), returnEffectSystems...), ObligationPolicy: policy,
		ConsistencyState: consistency, ObligationState: obligation,
		Effects: effects, RepairTargets: repair,
	}
	record.Digest = returnDigest(input.IdempotencyKey, input, consistency, obligation, repair)
	committer.records[input.IdempotencyKey] = record
	return record, nil
}

// RepairPlan is the targeted repair for one return effect system: it
// never reruns LeaveEnded or availability restoration and never opens
// a second local return transaction.
func (record ReturnRecord) RepairPlan(system string) (string, error) {
	for _, effect := range record.Effects {
		if effect.System != system {
			continue
		}
		if effect.State == EffectPass {
			return "", fmt.Errorf("leave: passing return effect %s needs no repair", system)
		}
		return "repair:" + system + ":owner=" + effect.Owner, nil
	}
	return "", fmt.Errorf("leave: unknown return effect %s", system)
}

// Verify recomputes the return seal from the full commit input: any
// mutated receipt, restriction, policy, effect or readiness breaks it.
func (record ReturnRecord) Verify(input ReturnCommitInput) error {
	if record.Digest == "" {
		return fmt.Errorf("leave: return seal is broken")
	}
	if record.BusinessState != BusinessLeaveEnded {
		return fmt.Errorf("leave: return reports %q instead of leave-ended business state", record.BusinessState)
	}
	want := returnDigest(input.IdempotencyKey, input, record.ConsistencyState, record.ObligationState, record.RepairTargets)
	if want != record.Digest {
		return fmt.Errorf("leave: return seal is broken")
	}
	return nil
}
