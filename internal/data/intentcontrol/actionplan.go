package intentcontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrAcceptedActionNotFound is returned when no approved durable decision
// established the requested accepted action.
var ErrAcceptedActionNotFound = errors.New("intentcontrol: accepted action not found")

// AcceptedAction is the immutable execution acceptance derived from one
// durable HUMAN_APPROVAL decision.
type AcceptedAction struct {
	TenantID           uuid.UUID
	DecisionID         uuid.UUID
	IntentID           uuid.UUID
	ActionID           string
	ProposalRevisionID string
	ProposalDigest     string
	AcceptedBy         string
	AcceptedAt         time.Time
	IdempotencyKey     string
	AcceptanceDigest   string
	RecordedAt         time.Time
}

// AcceptedActionStore reads and records immutable accepted-action facts.
type AcceptedActionStore struct{}

// RecordApprovedDecision records or resolves the first accepted execution
// action for an approved HUMAN_APPROVAL. Retrying the same decision requires
// byte-identical facts; later approvals sharing the same semantic action key
// resolve to the first row only when intent and proposal identity also match.
func (AcceptedActionStore) RecordApprovedDecision(ctx context.Context, ex Executor, decision Decision, proposalRevisionID, actionID, idempotencyKey string) (AcceptedAction, error) {
	if decision.Kind != DecisionHumanApproval || decision.Outcome != OutcomeApproved {
		return AcceptedAction{}, invalid("decision", "accepted actions require an approved HUMAN_APPROVAL decision")
	}
	if err := decision.Validate(); err != nil {
		return AcceptedAction{}, err
	}
	// Do not let a caller manufacture an approval merely by constructing a
	// Decision value. The acceptance must be derived from the durable decision
	// row in this transaction, and every field used to establish authority
	// must agree with that row.
	var durable Decision
	var durableRevision int64
	if err := ex.QueryRow(ctx, `
		SELECT tenant_id, decision_id, intent_id, revision, requirement_id,
			decision_kind, decision_outcome, proposal_digest, control_digest,
			materiality_class, decided_by, authority_ref, decision_reason,
			decided_at, recorded_at
		FROM intent_decision WHERE tenant_id=$1 AND decision_id=$2`,
		decision.TenantID, decision.DecisionID).Scan(
		&durable.TenantID, &durable.DecisionID, &durable.IntentID, &durableRevision,
		&durable.RequirementID, &durable.Kind, &durable.Outcome, &durable.ProposalDigest,
		&durable.ControlDigest, &durable.MaterialityClass, &durable.DecidedBy,
		&durable.AuthorityRef, &durable.Reason, &durable.DecidedAt, &durable.RecordedAt); err != nil {
		return AcceptedAction{}, fmt.Errorf("intentcontrol: verify durable approved decision %s: %w", decision.DecisionID, err)
	}
	durable.Revision = uint64(durableRevision)
	// decided_at is stored at the column's microsecond precision, so the
	// caller's instant is compared at that precision too: a decision stamped
	// from a nanosecond clock is the same decision as its own durable row.
	if durable.TenantID != decision.TenantID || durable.DecisionID != decision.DecisionID ||
		durable.IntentID != decision.IntentID || durable.Revision != decision.Revision ||
		durable.RequirementID != decision.RequirementID || durable.Kind != DecisionHumanApproval ||
		durable.Outcome != OutcomeApproved || durable.ProposalDigest != decision.ProposalDigest ||
		durable.ControlDigest != decision.ControlDigest || durable.MaterialityClass != decision.MaterialityClass ||
		durable.DecidedBy != decision.DecidedBy || durable.AuthorityRef != decision.AuthorityRef ||
		durable.Reason != decision.Reason || !durable.DecidedAt.Equal(decision.DecidedAt.Truncate(time.Microsecond)) {
		return AcceptedAction{}, fmt.Errorf("%w: decision %s does not match its durable HUMAN_APPROVAL row", ErrDuplicate, decision.DecisionID)
	}
	if strings.TrimSpace(proposalRevisionID) == "" || strings.TrimSpace(actionID) == "" || strings.TrimSpace(idempotencyKey) == "" {
		return AcceptedAction{}, invalid("accepted_action", "revision, action and idempotency key are required")
	}
	accepted := AcceptedAction{
		TenantID: decision.TenantID, DecisionID: decision.DecisionID, IntentID: decision.IntentID,
		ActionID: actionID, ProposalRevisionID: proposalRevisionID, ProposalDigest: decision.ProposalDigest,
		AcceptedBy: decision.DecidedBy, AcceptedAt: durable.DecidedAt.UTC(), IdempotencyKey: idempotencyKey,
	}
	accepted.AcceptanceDigest = accepted.computeDigest()
	row := ex.QueryRow(ctx, `
		INSERT INTO intent_accepted_action (
			tenant_id, decision_id, intent_id, action_id, proposal_revision_id,
			proposal_digest, accepted_by, accepted_at, idempotency_key, acceptance_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT DO NOTHING
		RETURNING tenant_id, decision_id, intent_id, action_id, proposal_revision_id,
			proposal_digest, accepted_by, accepted_at, idempotency_key, acceptance_digest, recorded_at`,
		accepted.TenantID, accepted.DecisionID, accepted.IntentID, accepted.ActionID,
		accepted.ProposalRevisionID, accepted.ProposalDigest, accepted.AcceptedBy,
		accepted.AcceptedAt, accepted.IdempotencyKey, accepted.AcceptanceDigest)
	stored, err := scanAcceptedAction(row)
	if err == nil {
		return stored, nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return AcceptedAction{}, fmt.Errorf("intentcontrol: record accepted action for decision %s: %w", accepted.DecisionID, err)
	}
	stored, err = (AcceptedActionStore{}).ByDecision(ctx, ex, accepted.TenantID, accepted.DecisionID)
	if err == nil {
		if stored.AcceptanceDigest != accepted.AcceptanceDigest || stored.computeDigest() != accepted.AcceptanceDigest {
			return AcceptedAction{}, fmt.Errorf("%w: decision %s already has different accepted-action facts", ErrDuplicate, accepted.DecisionID)
		}
		return stored, nil
	}
	if !errors.Is(err, ErrAcceptedActionNotFound) {
		return AcceptedAction{}, err
	}
	// Another approved HUMAN_APPROVAL decision may represent a subsequent
	// approver's vote on this same proposal action. The server-owned semantic
	// key has one canonical accepted row, so retries and later votes resolve
	// the first acceptance instead of attempting to replace its authority.
	stored, err = (AcceptedActionStore{}).ByActionKey(ctx, ex, accepted.TenantID, accepted.ActionID, accepted.IdempotencyKey)
	if err != nil {
		return AcceptedAction{}, err
	}
	if stored.IntentID != accepted.IntentID || stored.ProposalRevisionID != accepted.ProposalRevisionID ||
		stored.ProposalDigest != accepted.ProposalDigest {
		return AcceptedAction{}, fmt.Errorf("%w: semantic action key %q is already bound to different proposal facts", ErrDuplicate, accepted.IdempotencyKey)
	}
	return stored, nil
}

// ByDecision loads the accepted action anchored to one decision identity.
func (AcceptedActionStore) ByDecision(ctx context.Context, ex Executor, tenantID, decisionID uuid.UUID) (AcceptedAction, error) {
	if tenantID == uuid.Nil || decisionID == uuid.Nil {
		return AcceptedAction{}, invalid("identity", "tenant and decision ids are required")
	}
	row := ex.QueryRow(ctx, `
		SELECT tenant_id, decision_id, intent_id, action_id, proposal_revision_id,
			proposal_digest, accepted_by, accepted_at, idempotency_key, acceptance_digest, recorded_at
		FROM intent_accepted_action WHERE tenant_id=$1 AND decision_id=$2`, tenantID, decisionID)
	accepted, err := scanAcceptedAction(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return AcceptedAction{}, fmt.Errorf("%w: decision %s", ErrAcceptedActionNotFound, decisionID)
		}
		return AcceptedAction{}, fmt.Errorf("intentcontrol: load accepted action for decision %s: %w", decisionID, err)
	}
	if accepted.computeDigest() != accepted.AcceptanceDigest {
		return AcceptedAction{}, fmt.Errorf("intentcontrol: accepted action %s digest mismatch", decisionID)
	}
	return accepted, nil
}

// ByActionKey loads the first immutable accepted action for its server-owned
// semantic identity. The schema's unique constraint guarantees at most one.
func (AcceptedActionStore) ByActionKey(ctx context.Context, ex Executor, tenantID uuid.UUID, actionID, idempotencyKey string) (AcceptedAction, error) {
	if tenantID == uuid.Nil || strings.TrimSpace(actionID) == "" || strings.TrimSpace(idempotencyKey) == "" {
		return AcceptedAction{}, invalid("identity", "tenant, action and idempotency key are required")
	}
	row := ex.QueryRow(ctx, `
		SELECT tenant_id, decision_id, intent_id, action_id, proposal_revision_id,
			proposal_digest, accepted_by, accepted_at, idempotency_key, acceptance_digest, recorded_at
		FROM intent_accepted_action WHERE tenant_id=$1 AND action_id=$2 AND idempotency_key=$3`,
		tenantID, actionID, idempotencyKey)
	accepted, err := scanAcceptedAction(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return AcceptedAction{}, fmt.Errorf("%w: action %s key %q", ErrAcceptedActionNotFound, actionID, idempotencyKey)
		}
		return AcceptedAction{}, fmt.Errorf("intentcontrol: load accepted action %s key %q: %w", actionID, idempotencyKey, err)
	}
	if accepted.computeDigest() != accepted.AcceptanceDigest {
		return AcceptedAction{}, fmt.Errorf("intentcontrol: accepted action %s key %q digest mismatch", actionID, idempotencyKey)
	}
	return accepted, nil
}

// ForPlan resolves the unique acceptance whose exact action, proposal and
// semantic key are named by the prepared transaction plan.
func (AcceptedActionStore) ForPlan(ctx context.Context, ex Executor, tenantID, intentID uuid.UUID, actionID, proposalRevisionID, proposalDigest, idempotencyKey string) (AcceptedAction, error) {
	if tenantID == uuid.Nil || intentID == uuid.Nil {
		return AcceptedAction{}, invalid("identity", "tenant and intent ids are required")
	}
	for field, value := range map[string]string{"action_id": actionID, "proposal_revision_id": proposalRevisionID, "proposal_digest": proposalDigest, "idempotency_key": idempotencyKey} {
		if strings.TrimSpace(value) == "" {
			return AcceptedAction{}, invalid(field, "value is required")
		}
	}
	row := ex.QueryRow(ctx, `
		SELECT tenant_id, decision_id, intent_id, action_id, proposal_revision_id,
			proposal_digest, accepted_by, accepted_at, idempotency_key, acceptance_digest, recorded_at
		FROM intent_accepted_action
		WHERE tenant_id=$1 AND intent_id=$2 AND action_id=$3 AND proposal_revision_id=$4
		  AND proposal_digest=$5 AND idempotency_key=$6`,
		tenantID, intentID, actionID, proposalRevisionID, proposalDigest, idempotencyKey)
	accepted, err := scanAcceptedAction(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return AcceptedAction{}, fmt.Errorf("%w: intent %s proposal %s", ErrAcceptedActionNotFound, intentID, proposalRevisionID)
		}
		return AcceptedAction{}, fmt.Errorf("intentcontrol: resolve accepted action for intent %s: %w", intentID, err)
	}
	if accepted.computeDigest() != accepted.AcceptanceDigest {
		return AcceptedAction{}, fmt.Errorf("intentcontrol: accepted action %s digest mismatch", accepted.DecisionID)
	}
	return accepted, nil
}

func (a AcceptedAction) computeDigest() string {
	preimage, _ := json.Marshal(struct {
		TenantID           string    `json:"tenant_id"`
		DecisionID         string    `json:"decision_id"`
		IntentID           string    `json:"intent_id"`
		ActionID           string    `json:"action_id"`
		ProposalRevisionID string    `json:"proposal_revision_id"`
		ProposalDigest     string    `json:"proposal_digest"`
		AcceptedBy         string    `json:"accepted_by"`
		AcceptedAt         time.Time `json:"accepted_at"`
		IdempotencyKey     string    `json:"idempotency_key"`
	}{a.TenantID.String(), a.DecisionID.String(), a.IntentID.String(), a.ActionID,
		a.ProposalRevisionID, a.ProposalDigest, a.AcceptedBy, a.AcceptedAt.UTC(), a.IdempotencyKey})
	sum := sha256.Sum256(preimage)
	// acceptance_digest uses migration 00002's content_digest domain, which
	// stores exactly 64 lowercase hexadecimal characters without an algorithm
	// prefix.
	return hex.EncodeToString(sum[:])
}

func scanAcceptedAction(row dbport.Row) (AcceptedAction, error) {
	var a AcceptedAction
	err := row.Scan(&a.TenantID, &a.DecisionID, &a.IntentID, &a.ActionID,
		&a.ProposalRevisionID, &a.ProposalDigest, &a.AcceptedBy, &a.AcceptedAt,
		&a.IdempotencyKey, &a.AcceptanceDigest, &a.RecordedAt)
	if err != nil {
		return AcceptedAction{}, err
	}
	a.AcceptedAt = a.AcceptedAt.UTC()
	a.RecordedAt = a.RecordedAt.UTC()
	return a, nil
}

// ActionPlanBinding is the full immutable action-to-plan proof persisted at
// the commit boundary. BindingPayload contains its canonical JSON fields.
type ActionPlanBinding struct {
	TenantID           uuid.UUID
	DecisionID         uuid.UUID
	PlanID             uuid.UUID
	ActionID           string
	IntentID           uuid.UUID
	ProposalRevisionID string
	ProposalDigest     string
	AcceptedBy         string
	AcceptedAt         time.Time
	IdempotencyKey     string
	PlanDigest         string
	BindingDigest      string
	BindingPayload     []byte
	RecordedAt         time.Time
}

// ActionPlanBindingStore appends full action-plan binding evidence.
type ActionPlanBindingStore struct{}

// Record appends one full canonical binding. It replays only byte-identical
// binding content for the same acceptance and plan.
func (ActionPlanBindingStore) Record(ctx context.Context, ex Executor, b ActionPlanBinding) (ActionPlanBinding, error) {
	if b.TenantID == uuid.Nil || b.DecisionID == uuid.Nil || b.PlanID == uuid.Nil {
		return ActionPlanBinding{}, invalid("identity", "tenant, decision and plan ids are required")
	}
	if len(b.BindingPayload) == 0 || !json.Valid(b.BindingPayload) {
		return ActionPlanBinding{}, invalid("binding", "payload and digests are required")
	}
	planDigest, err := bareSHA256Digest(b.PlanDigest)
	if err != nil {
		return ActionPlanBinding{}, invalid("plan_digest", err.Error())
	}
	bindingDigest, err := bareSHA256Digest(b.BindingDigest)
	if err != nil {
		return ActionPlanBinding{}, invalid("binding_digest", err.Error())
	}
	payloadDigest := sha256.Sum256(b.BindingPayload)
	if hex.EncodeToString(payloadDigest[:]) != bindingDigest {
		return ActionPlanBinding{}, invalid("binding_payload", "payload does not match the canonical binding digest")
	}
	row := ex.QueryRow(ctx, `
		INSERT INTO intent_action_plan_binding (
			tenant_id, decision_id, plan_id, action_id, intent_id,
			proposal_revision_id, proposal_digest, accepted_by, accepted_at,
			idempotency_key, plan_digest, binding_digest, binding_payload)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb)
		ON CONFLICT (tenant_id, decision_id, plan_id) DO NOTHING
		RETURNING tenant_id, decision_id, plan_id, action_id, intent_id,
			proposal_revision_id, proposal_digest, accepted_by, accepted_at,
			idempotency_key, plan_digest, binding_digest, binding_payload, recorded_at`,
		b.TenantID, b.DecisionID, b.PlanID, b.ActionID, b.IntentID,
		b.ProposalRevisionID, b.ProposalDigest, b.AcceptedBy, b.AcceptedAt.UTC(),
		b.IdempotencyKey, planDigest, bindingDigest, b.BindingPayload)
	stored, err := scanActionPlanBinding(row)
	if err == nil {
		return stored, nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return ActionPlanBinding{}, fmt.Errorf("intentcontrol: record action-plan binding for plan %s: %w", b.PlanID, err)
	}
	stored, err = (ActionPlanBindingStore{}).ByPlan(ctx, ex, b.TenantID, b.PlanID)
	if err != nil {
		return ActionPlanBinding{}, err
	}
	if stored.DecisionID != b.DecisionID || stored.PlanDigest != planDigest || stored.BindingDigest != bindingDigest || string(stored.BindingPayload) != string(b.BindingPayload) {
		return ActionPlanBinding{}, fmt.Errorf("%w: plan %s already has a different action binding", ErrDuplicate, b.PlanID)
	}
	return stored, nil
}

// ByPlan loads and verifies one action-plan binding for the tenant's plan.
func (ActionPlanBindingStore) ByPlan(ctx context.Context, ex Executor, tenantID, planID uuid.UUID) (ActionPlanBinding, error) {
	if tenantID == uuid.Nil || planID == uuid.Nil {
		return ActionPlanBinding{}, invalid("identity", "tenant and plan ids are required")
	}
	row := ex.QueryRow(ctx, `
		SELECT tenant_id, decision_id, plan_id, action_id, intent_id,
			proposal_revision_id, proposal_digest, accepted_by, accepted_at,
			idempotency_key, plan_digest, binding_digest, binding_payload, recorded_at
		FROM intent_action_plan_binding WHERE tenant_id=$1 AND plan_id=$2`, tenantID, planID)
	binding, err := scanActionPlanBinding(row)
	if err != nil {
		return ActionPlanBinding{}, err
	}
	if len(binding.BindingPayload) == 0 || !json.Valid(binding.BindingPayload) {
		return ActionPlanBinding{}, invalid("binding_payload", "stored payload is not valid JSON")
	}
	payloadDigest := sha256.Sum256(binding.BindingPayload)
	if hex.EncodeToString(payloadDigest[:]) != binding.BindingDigest {
		return ActionPlanBinding{}, invalid("binding_digest", "stored payload does not match its digest")
	}
	return binding, nil
}

// bareSHA256Digest converts the canonical app-facing sha256:<hex> form to
// migration 00002's content_digest representation. The prefix remains part
// of the digest contract and canonical binding payload; only the SQL column
// representation omits it.
func bareSHA256Digest(value string) (string, error) {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) {
		return "", fmt.Errorf("digest must use the sha256: prefix")
	}
	bare := strings.TrimPrefix(value, prefix)
	if !digestPattern.MatchString(bare) {
		return "", fmt.Errorf("digest must contain 64 lowercase hexadecimal characters")
	}
	return bare, nil
}

func scanActionPlanBinding(row dbport.Row) (ActionPlanBinding, error) {
	var b ActionPlanBinding
	err := row.Scan(&b.TenantID, &b.DecisionID, &b.PlanID, &b.ActionID, &b.IntentID,
		&b.ProposalRevisionID, &b.ProposalDigest, &b.AcceptedBy, &b.AcceptedAt,
		&b.IdempotencyKey, &b.PlanDigest, &b.BindingDigest, &b.BindingPayload, &b.RecordedAt)
	if err != nil {
		return ActionPlanBinding{}, err
	}
	b.AcceptedAt = b.AcceptedAt.UTC()
	b.RecordedAt = b.RecordedAt.UTC()
	return b, nil
}
