package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transactionplan "github.com/monstercameron/human-capital-management-suite/internal/transaction/plan"
)

// ALIGN-030: an accepted action is bound to the transaction plan that commits
// it. The binding pins the exact proposal revision and digest the action was
// accepted against, the semantic idempotency key the acceptance carries, and
// the digest of the prepared plan. A later acceptance, a re-prepared plan, or
// a plan whose content no longer digests to its recorded digest each makes the
// binding non-current, so execution can never commit a plan the acceptance no
// longer describes.

const actionPlanBindingVersion = 1

// Version identifies the accepted-action binding contract.
func (b ActionPlanBinding) Version() int { return actionPlanBindingVersion }

// ActionPlanStatus is the currency of an accepted-action binding.
type ActionPlanStatus string

// Binding statuses.
const (
	ActionPlanCurrent    ActionPlanStatus = "CURRENT"
	ActionPlanChanged    ActionPlanStatus = "ACTION_CHANGED"
	ActionPlanSuperseded ActionPlanStatus = "SUPERSEDED"
	ActionPlanTampered   ActionPlanStatus = "TAMPERED"
)

// ErrActionPlanBinding reports an acceptance that cannot be bound to a plan.
var ErrActionPlanBinding = errors.New("app: accepted action cannot be bound to this transaction plan")

// AcceptedAction is the tenant-scoped record that a projected semantic action
// was accepted in the governed work loop. It names what was accepted (the
// action and the exact proposal revision with its material digest), who
// accepted it, when, and under which semantic idempotency key.
type AcceptedAction struct {
	Tenant             values.TenantId `json:"tenant"`
	ActionID           string          `json:"action_id"`
	IntentID           string          `json:"intent_id"`
	ProposalRevisionID string          `json:"proposal_revision_id"`
	ProposalDigest     string          `json:"proposal_digest"`
	AcceptedBy         string          `json:"accepted_by"`
	AcceptedAt         values.Instant  `json:"accepted_at"`
	IdempotencyKey     string          `json:"idempotency_key"`
}

// ActionPlanBinding is the durable link between one acceptance and the
// prepared transaction plan that commits it.
type ActionPlanBinding struct {
	Tenant             values.TenantId `json:"tenant"`
	ActionID           string          `json:"action_id"`
	IntentID           string          `json:"intent_id"`
	ProposalRevisionID string          `json:"proposal_revision_id"`
	ProposalDigest     string          `json:"proposal_digest"`
	AcceptedBy         string          `json:"accepted_by"`
	AcceptedAt         values.Instant  `json:"accepted_at"`
	IdempotencyKey     string          `json:"idempotency_key"`
	PlanID             string          `json:"plan_id"`
	PlanDigest         string          `json:"plan_digest"`
	Digest             string          `json:"digest"`
}

func (a AcceptedAction) validate() error {
	if err := a.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrActionPlanBinding, err)
	}
	for _, req := range []struct{ field, value string }{
		{"action_id", a.ActionID},
		{"intent_id", a.IntentID},
		{"proposal_revision_id", a.ProposalRevisionID},
		{"proposal_digest", a.ProposalDigest},
		{"accepted_by", a.AcceptedBy},
		{"idempotency_key", a.IdempotencyKey},
	} {
		if req.value == "" {
			return fmt.Errorf("%w: %s is required", ErrActionPlanBinding, req.field)
		}
	}
	if err := a.AcceptedAt.Validate(); err != nil {
		return fmt.Errorf("%w: accepted_at: %v", ErrActionPlanBinding, err)
	}
	return nil
}

// BindAcceptedAction binds an acceptance to its prepared transaction plan.
// The plan must verify against its own digest, belong to the same tenant,
// address the same proposal revision and material digest, carry the same
// semantic idempotency key, and still be current at the acceptance instant.
func BindAcceptedAction(action AcceptedAction, plan transactionplan.TransactionPlan) (ActionPlanBinding, error) {
	if err := action.validate(); err != nil {
		return ActionPlanBinding{}, err
	}
	if err := plan.VerifyDigest(); err != nil {
		return ActionPlanBinding{}, fmt.Errorf("%w: plan: %v", ErrActionPlanBinding, err)
	}
	switch {
	case plan.Tenant != action.Tenant:
		return ActionPlanBinding{}, fmt.Errorf("%w: tenant mismatch", ErrActionPlanBinding)
	case plan.ProposalRevisionID != action.ProposalRevisionID:
		return ActionPlanBinding{}, fmt.Errorf("%w: plan addresses revision %q, acceptance is for %q", ErrActionPlanBinding, plan.ProposalRevisionID, action.ProposalRevisionID)
	case plan.ProposalDigest != action.ProposalDigest:
		return ActionPlanBinding{}, fmt.Errorf("%w: plan digest does not match the accepted proposal digest", ErrActionPlanBinding)
	case plan.IdempotencyKey != action.IdempotencyKey:
		return ActionPlanBinding{}, fmt.Errorf("%w: plan idempotency key does not match the acceptance", ErrActionPlanBinding)
	case plan.PlanID == "":
		return ActionPlanBinding{}, fmt.Errorf("%w: plan carries no identity", ErrActionPlanBinding)
	case action.AcceptedAt.After(plan.ExpiresAt):
		return ActionPlanBinding{}, fmt.Errorf("%w: acceptance is past plan expiry", ErrActionPlanBinding)
	}
	b := ActionPlanBinding{
		Tenant: action.Tenant, ActionID: action.ActionID, IntentID: action.IntentID,
		ProposalRevisionID: action.ProposalRevisionID, ProposalDigest: action.ProposalDigest,
		AcceptedBy: action.AcceptedBy, AcceptedAt: action.AcceptedAt, IdempotencyKey: action.IdempotencyKey,
		PlanID: plan.PlanID, PlanDigest: plan.Digest,
	}
	b.Digest = b.computeDigest()
	return b, nil
}

// Check re-evaluates a binding against the current acceptance and plan. An
// acceptance that no longer matches is ACTION_CHANGED; a different plan for
// the same acceptance is SUPERSEDED; the same plan with altered content is
// TAMPERED.
func (b ActionPlanBinding) Check(action AcceptedAction, plan transactionplan.TransactionPlan) ActionPlanStatus {
	if action.Tenant != b.Tenant || action.ActionID != b.ActionID || action.IntentID != b.IntentID ||
		action.ProposalRevisionID != b.ProposalRevisionID || action.ProposalDigest != b.ProposalDigest ||
		action.AcceptedBy != b.AcceptedBy || action.AcceptedAt.Compare(b.AcceptedAt) != 0 ||
		action.IdempotencyKey != b.IdempotencyKey {
		return ActionPlanChanged
	}
	if plan.PlanID != b.PlanID {
		return ActionPlanSuperseded
	}
	if plan.Digest != b.PlanDigest || plan.VerifyDigest() != nil {
		return ActionPlanTampered
	}
	return ActionPlanCurrent
}

// CanonicalBytes is the digest preimage: every pinned field, never the
// digest itself.
func (b ActionPlanBinding) CanonicalBytes() []byte {
	shadow := struct {
		Tenant             values.TenantId `json:"tenant"`
		ActionID           string          `json:"action_id"`
		IntentID           string          `json:"intent_id"`
		ProposalRevisionID string          `json:"proposal_revision_id"`
		ProposalDigest     string          `json:"proposal_digest"`
		AcceptedBy         string          `json:"accepted_by"`
		AcceptedAt         values.Instant  `json:"accepted_at"`
		IdempotencyKey     string          `json:"idempotency_key"`
		PlanID             string          `json:"plan_id"`
		PlanDigest         string          `json:"plan_digest"`
	}{b.Tenant, b.ActionID, b.IntentID, b.ProposalRevisionID, b.ProposalDigest,
		b.AcceptedBy, b.AcceptedAt, b.IdempotencyKey, b.PlanID, b.PlanDigest}
	out, err := json.Marshal(shadow)
	if err != nil {
		return nil
	}
	return out
}

func (b ActionPlanBinding) computeDigest() string {
	sum := sha256.Sum256(b.CanonicalBytes())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// DigestValue returns the recorded binding digest.
func (b ActionPlanBinding) DigestValue() string { return b.Digest }

// VerifyDigest reports whether the recorded digest matches the pinned fields.
func (b ActionPlanBinding) VerifyDigest() error {
	if got := b.computeDigest(); got != b.Digest {
		return fmt.Errorf("%w: binding records digest %q but its content hashes to %q", ErrActionPlanBinding, b.Digest, got)
	}
	return nil
}

// Explain returns a bounded, evidence-safe summary. It names the action, the
// intent, the plan and the pinned digests, never the accepting principal.
func (b ActionPlanBinding) Explain() string {
	return fmt.Sprintf("accepted action %s for intent %s bound to transaction plan %s (%s)", b.ActionID, b.IntentID, b.PlanID, b.PlanDigest)
}
