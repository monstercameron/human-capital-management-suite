package approval

// WF-STEP-018: durable withdrawal of pending approval decisions.
//
// A decision recorded before its requirement reached quorum is immutable
// evidence (work_item_decision refuses UPDATE and DELETE), so a material change
// cannot erase it. It withdraws it instead: one append-only
// workflow_approval_withdrawal row (migration 00305) per withdrawn decision,
// naming the decision digest, the declared invalidator that fired, why, and
// the continuation it was pending against. The row commits in the same
// transaction that routes the approval node INVALIDATED.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// ErrInvalidWithdrawal reports a withdrawal record missing the evidence it
// must carry.
var ErrInvalidWithdrawal = errors.New("workflow approval: invalid decision withdrawal")

// Withdrawal is one withdrawn pending decision.
type Withdrawal struct {
	TenantID           uuid.UUID
	WorkItemID         uuid.UUID
	WorkflowInstanceID uuid.UUID
	NodeID             string
	RequirementID      string
	// DecisionDigest is the withdrawn work_item_decision body digest.
	DecisionDigest string
	DecidedBy      string

	Invalidator        humanwork.Invalidator
	Reason             string
	EvidenceRef        string
	ContinuationDigest string

	WithdrawnBy string
	WithdrawnAt time.Time
	RecordedAt  time.Time
}

func (w Withdrawal) validate() error {
	switch {
	case w.TenantID == uuid.Nil || w.WorkItemID == uuid.Nil || w.WorkflowInstanceID == uuid.Nil:
		return fmt.Errorf("%w: identity is incomplete", ErrInvalidWithdrawal)
	case w.NodeID == "" || w.RequirementID == "" || w.DecidedBy == "" || w.WithdrawnBy == "":
		return fmt.Errorf("%w: node, requirement, decider and withdrawer are required", ErrInvalidWithdrawal)
	case !workitem.ValidDigest(w.DecisionDigest):
		return fmt.Errorf("%w: decision digest %q is malformed", ErrInvalidWithdrawal, w.DecisionDigest)
	case !w.Invalidator.Kind.Valid() || w.Invalidator.RuleID == "":
		return fmt.Errorf("%w: invalidator %q is not declared", ErrInvalidWithdrawal, w.Invalidator.Kind)
	case w.Reason == "" || w.EvidenceRef == "" || w.ContinuationDigest == "":
		return fmt.Errorf("%w: reason, evidence and continuation are required", ErrInvalidWithdrawal)
	case w.WithdrawnAt.IsZero():
		return fmt.Errorf("%w: withdrawal instant is required", ErrInvalidWithdrawal)
	}
	return nil
}

// NewWithdrawal builds the withdrawal of item's recorded decision.
func NewWithdrawal(
	c Continuation, item workitem.WorkItem, decision intentapproval.ApprovalDecision, invalidator humanwork.Invalidator,
	reason, evidenceRef, withdrawnBy string, at time.Time,
) Withdrawal {
	return Withdrawal{
		TenantID: item.TenantID, WorkItemID: item.WorkItemID, WorkflowInstanceID: item.WorkflowInstanceID,
		NodeID: item.NodeID, RequirementID: item.ApprovalRequirementRef,
		DecisionDigest: decision.Digest(), DecidedBy: decision.Approver.PrincipalID,
		Invalidator: invalidator, Reason: reason, EvidenceRef: evidenceRef, ContinuationDigest: c.Digest,
		WithdrawnBy: withdrawnBy, WithdrawnAt: at.UTC(),
	}
}

const withdrawalColumns = `tenant_id, work_item_id, workflow_instance_id, node_id, requirement_id,
	decision_body_digest, decided_by, invalidator_kind, invalidator_rule_id, reason, evidence_ref,
	continuation_digest, withdrawn_by, withdrawn_at, recorded_at`

// RecordWithdrawal appends w in the caller's transaction. A decision is
// withdrawn at most once: a second withdrawal of the same work item collides
// on the table's primary key.
func RecordWithdrawal(ctx context.Context, ex workitem.Executor, w Withdrawal) (ret0 Withdrawal, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.steps.approval.record_withdrawal", w.WorkItemID)
	defer func() { observe.DoneWith(obsOp, retErr, ret0.WorkItemID) }()
	if err := w.validate(); err != nil {
		return Withdrawal{}, err
	}
	row := ex.QueryRow(ctx, `
		INSERT INTO workflow_approval_withdrawal (`+withdrawalColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, now())
		RETURNING `+withdrawalColumns,
		w.TenantID, w.WorkItemID, w.WorkflowInstanceID, w.NodeID, w.RequirementID,
		w.DecisionDigest, w.DecidedBy, string(w.Invalidator.Kind), w.Invalidator.RuleID, w.Reason, w.EvidenceRef,
		w.ContinuationDigest, w.WithdrawnBy, w.WithdrawnAt.UTC())
	stored, err := scanWithdrawal(row)
	if err != nil {
		return Withdrawal{}, fmt.Errorf("workflow steps/approval: record decision withdrawal: %w", err)
	}
	return stored, nil
}

// LoadWithdrawals reads every withdrawal recorded for one workflow instance,
// ordered by work item.
func LoadWithdrawals(ctx context.Context, ex workitem.Executor, tenantID, instanceID uuid.UUID) (ret0 []Withdrawal, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.steps.approval.load_withdrawals", instanceID)
	defer func() { observe.DoneWith(obsOp, retErr, len(ret0)) }()
	rows, err := ex.Query(ctx, `SELECT `+withdrawalColumns+` FROM workflow_approval_withdrawal
		WHERE tenant_id = $1 AND workflow_instance_id = $2 ORDER BY work_item_id`, tenantID, instanceID)
	if err != nil {
		return nil, fmt.Errorf("workflow steps/approval: list decision withdrawals: %w", err)
	}
	defer rows.Close()
	out := []Withdrawal{}
	for rows.Next() {
		w, scanErr := scanWithdrawal(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("workflow steps/approval: scan decision withdrawal: %w", scanErr)
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("workflow steps/approval: iterate decision withdrawals: %w", err)
	}
	return out, nil
}

// LoadDecisions reads the durable decision of every completed continuation
// slot in items, so a resolution counts exactly the votes that committed and
// never a caller-supplied list.
func LoadDecisions(ctx context.Context, ex workitem.Executor, c Continuation, items []workitem.WorkItem) (ret0 []intentapproval.ApprovalDecision, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.steps.approval.load_decisions", c.NodeID)
	defer func() { observe.DoneWith(obsOp, retErr, len(ret0)) }()
	slots, err := Slots(c, items)
	if err != nil {
		return nil, err
	}
	var out []intentapproval.ApprovalDecision
	for _, item := range slots {
		if item.Status != workitem.StatusCompleted {
			continue
		}
		rec, err := workitem.LoadDecision(ctx, ex, item.TenantID, item.WorkItemID)
		if err != nil {
			return nil, fmt.Errorf("%w: completed work item %s: %v", ErrInvalidEvidence, item.WorkItemID, err)
		}
		decision, err := DecisionFromRecord(rec)
		if err != nil {
			return nil, err
		}
		out = append(out, decision)
	}
	return out, nil
}

type withdrawalScanner interface {
	Scan(dest ...any) error
}

func scanWithdrawal(row withdrawalScanner) (Withdrawal, error) {
	var w Withdrawal
	var kind string
	if err := row.Scan(&w.TenantID, &w.WorkItemID, &w.WorkflowInstanceID, &w.NodeID, &w.RequirementID,
		&w.DecisionDigest, &w.DecidedBy, &kind, &w.Invalidator.RuleID, &w.Reason, &w.EvidenceRef,
		&w.ContinuationDigest, &w.WithdrawnBy, &w.WithdrawnAt, &w.RecordedAt); err != nil {
		return Withdrawal{}, err
	}
	w.Invalidator.Kind = humanwork.InvalidatorKind(kind)
	w.WithdrawnAt = w.WithdrawnAt.UTC()
	w.RecordedAt = w.RecordedAt.UTC()
	return w, nil
}
