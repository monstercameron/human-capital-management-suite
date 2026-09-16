package intervention

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

const decisionProfile = "hcmnext.workflow.intervention.Decision/v1"

// decisionNamespace is the fixed UUIDv5 namespace decision identities derive
// under, so a retried executor recomputes the same identity.
var decisionNamespace = uuid.MustParse("3a8e5c1f-9b7d-5f42-8c6e-1d2b4a7f9e03")

// Binding is the governed authority an accepted intervention was executed
// under: the operator gateway kind, the operational intent (and so the
// journaled operator receipt) and the idempotency key.
type Binding struct {
	TenantID         uuid.UUID
	OperatorKind     string
	IntentInstanceID string
	IdempotencyKey   string
}

// Observed is the durable state read back after the runtime transition.
type Observed struct {
	InstanceStatus  runtime.InstanceStatus `json:"instance_status"`
	InstanceVersion int64                  `json:"instance_version"`
	Node            NodeRef                `json:"node,omitzero"`
}

// Decision is the immutable record of one accepted intervention: what was
// requested, under which authority, the plan the executor performed and the
// transition observed afterwards.
type Decision struct {
	TenantID         uuid.UUID `json:"tenant_id"`
	DecisionID       uuid.UUID `json:"decision_id"`
	InstanceID       uuid.UUID `json:"instance_id"`
	Kind             Kind      `json:"kind"`
	Capability       string    `json:"capability"`
	OperatorKind     string    `json:"operator_kind"`
	IntentInstanceID string    `json:"intent_instance_id"`
	IdempotencyKey   string    `json:"idempotency_key"`
	RequestedBy      string    `json:"requested_by"`
	Reason           string    `json:"reason"`
	EvidenceRefs     []string  `json:"evidence_refs"`
	Plan             Plan      `json:"plan"`
	Observed         Observed  `json:"observed"`
	DecidedAt        time.Time `json:"decided_at"`
	Digest           string    `json:"digest"`
}

// DecisionID derives the identity of the decision one request would produce.
func DecisionID(tenantID uuid.UUID, r Request, idempotencyKey string) uuid.UUID {
	name := tenantID.String() + "\x00" + r.InstanceID.String() + "\x00" + strconv.FormatInt(r.ExpectedVersion, 10) +
		"\x00" + string(r.Kind) + "\x00" + idempotencyKey
	return uuid.NewSHA1(decisionNamespace, []byte(name))
}

// NewDecision seals the decision for an accepted plan and the transition
// observed after it was performed.
func NewDecision(r Request, p Plan, b Binding, observed Observed) (Decision, error) {
	switch {
	case b.TenantID == uuid.Nil || b.OperatorKind == "" || b.IntentInstanceID == "" || b.IdempotencyKey == "":
		return Decision{}, refuse(CodeUnauthorized, r.InstanceID.String(), "a decision is bound to a tenant, an operator kind, an operational intent and an idempotency key")
	case p.Kind != r.Kind || p.InstanceID != r.InstanceID || p.ExpectedVersion != r.ExpectedVersion:
		return Decision{}, refuse(CodeInvalidRequest, r.InstanceID.String(), "the plan was not evaluated for this request")
	case observed.InstanceVersion <= p.ExpectedVersion:
		// An accepted intervention always moves the instance version: a
		// decision without an observable transition would be a no-op.
		return Decision{}, refuse(CodeNoOp, r.InstanceID.String(), "the instance version did not move; no transition was observed")
	}
	d := Decision{
		TenantID: b.TenantID, DecisionID: DecisionID(b.TenantID, r, b.IdempotencyKey), InstanceID: r.InstanceID,
		Kind: r.Kind, Capability: r.Kind.Capability(), OperatorKind: b.OperatorKind,
		IntentInstanceID: b.IntentInstanceID, IdempotencyKey: b.IdempotencyKey,
		RequestedBy: r.RequestedBy, Reason: r.Reason, EvidenceRefs: normalizedEvidence(r.EvidenceRefs),
		Plan: p, Observed: observed, DecidedAt: r.RequestedAt.UTC().Truncate(time.Microsecond),
	}
	d.Digest = decisionDigest(d)
	return d, nil
}

// Verify reports whether the decision's content matches its digest.
func (d Decision) Verify() error {
	if d.Digest == "" || d.Digest != decisionDigest(d) {
		return refuse(CodeDecisionMutated, d.DecisionID.String(), "intervention decision content does not match its digest")
	}
	return nil
}

func decisionDigest(d Decision) string {
	d.Digest = ""
	d.DecidedAt = d.DecidedAt.UTC()
	d.EvidenceRefs = slices.Clone(d.EvidenceRefs)
	return "sha256:" + digest(decisionProfile, d)
}

// DecisionStore is the append-only decision ledger over
// migrations/00307_workflow_intervention_decision.sql. It holds no state.
type DecisionStore struct{}

const decisionColumns = `tenant_id, decision_id, instance_id, kind, capability, operator_kind, intent_instance_id,
	idempotency_key, requested_by, reason, evidence_refs, expected_instance_version, resulting_instance_version,
	instance_from, instance_to, node_id, plan, observed, decided_at, decision_digest`

// Record inserts one sealed decision. A second decision for the same instance
// version or idempotency key records nothing and is refused with
// [CodeStaleVersion]: concurrent interventions on one instance converge to one
// decision.
func (DecisionStore) Record(ctx context.Context, ex runtime.Executor, d Decision) (retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.intervention.record_decision", d)
	defer func() { observe.DoneWith(obsOp, retErr) }()
	if err := d.Verify(); err != nil {
		return err
	}
	plan, err := json.Marshal(d.Plan)
	if err != nil {
		return &Error{Code: CodeStorageFailed, Ref: d.DecisionID.String(), Detail: "encode plan", Err: err}
	}
	observed, err := json.Marshal(d.Observed)
	if err != nil {
		return &Error{Code: CodeStorageFailed, Ref: d.DecisionID.String(), Detail: "encode observed transition", Err: err}
	}
	var id uuid.UUID
	err = ex.QueryRow(ctx, `INSERT INTO workflow_intervention_decision (`+decisionColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		ON CONFLICT DO NOTHING
		RETURNING decision_id`,
		d.TenantID, d.DecisionID, d.InstanceID, string(d.Kind), d.Capability, d.OperatorKind, d.IntentInstanceID,
		d.IdempotencyKey, d.RequestedBy, d.Reason, d.EvidenceRefs, d.Plan.ExpectedVersion, d.Observed.InstanceVersion,
		string(d.Plan.InstanceFrom), string(d.Observed.InstanceStatus), nullable(d.Plan.Node.NodeID),
		plan, observed, d.DecidedAt, d.Digest).Scan(&id)
	if errors.Is(err, dbport.ErrNoRows) {
		return refuse(CodeStaleVersion, d.InstanceID.String(), "a decision for instance version %d or idempotency key %q is already recorded", d.Plan.ExpectedVersion, d.IdempotencyKey)
	}
	if err != nil {
		return &Error{Code: CodeStorageFailed, Ref: d.DecisionID.String(), Detail: "insert intervention decision", Err: err}
	}
	return nil
}

// Load reads every decision recorded for an instance, oldest first, verifying
// each against its digest.
func (DecisionStore) Load(ctx context.Context, ex runtime.Executor, tenantID, instanceID uuid.UUID) (ret0 []Decision, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.intervention.load_decisions", tenantID, instanceID)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	rows, err := ex.Query(ctx, `SELECT `+decisionColumns+` FROM workflow_intervention_decision
		WHERE tenant_id = $1 AND instance_id = $2 ORDER BY expected_instance_version, decision_id`, tenantID, instanceID)
	if err != nil {
		return nil, &Error{Code: CodeStorageFailed, Ref: instanceID.String(), Detail: "read intervention decisions", Err: err}
	}
	defer rows.Close()
	out := []Decision{}
	for rows.Next() {
		var d Decision
		var kind, instanceFrom, instanceTo string
		var nodeID *string
		var expected, resulting int64
		var plan, observed []byte
		if err := rows.Scan(&d.TenantID, &d.DecisionID, &d.InstanceID, &kind, &d.Capability, &d.OperatorKind,
			&d.IntentInstanceID, &d.IdempotencyKey, &d.RequestedBy, &d.Reason, &d.EvidenceRefs, &expected, &resulting,
			&instanceFrom, &instanceTo, &nodeID, &plan, &observed, &d.DecidedAt, &d.Digest); err != nil {
			return nil, &Error{Code: CodeStorageFailed, Ref: instanceID.String(), Detail: "scan intervention decision", Err: err}
		}
		d.Kind = Kind(kind)
		if err := json.Unmarshal(plan, &d.Plan); err != nil {
			return nil, &Error{Code: CodeDecisionMutated, Ref: d.DecisionID.String(), Detail: "decode plan", Err: err}
		}
		if err := json.Unmarshal(observed, &d.Observed); err != nil {
			return nil, &Error{Code: CodeDecisionMutated, Ref: d.DecisionID.String(), Detail: "decode observed transition", Err: err}
		}
		d.DecidedAt = d.DecidedAt.UTC()
		// The lifted columns must agree with the sealed content they index.
		if expected != d.Plan.ExpectedVersion || resulting != d.Observed.InstanceVersion ||
			instanceFrom != string(d.Plan.InstanceFrom) || instanceTo != string(d.Observed.InstanceStatus) {
			return nil, refuse(CodeDecisionMutated, d.DecisionID.String(), "indexed columns disagree with the sealed decision")
		}
		if err := d.Verify(); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, &Error{Code: CodeStorageFailed, Ref: instanceID.String(), Detail: "iterate intervention decisions", Err: err}
	}
	return out, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
