package effects

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	ledgerport "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// PromotionOutcomeSchema names the payload schema a terminal settlement
// records its governed business fact under. The canonical owner is the
// promotion settlement capability
// ([promotioncommit.PromotionOutcomeSchema]); this alias keeps the
// workflow's own readers on the same reference without redefining it.
const PromotionOutcomeSchema = promotioncommit.PromotionOutcomeSchema

// PromotionSettler is the promotion settlement capability the terminal
// effect invokes. The production implementation is
// [promotioncommit.Settler], which owns guard release, budget release,
// payload-schema registration and the outcome ledger write; tests supply a
// recording double. The workflow layer records only the typed
// [promotioncommit.SettleResult] the capability returns and writes no
// non-workflow table itself.
type PromotionSettler interface {
	Settle(context.Context, dbport.Tx, promotioncommit.SettleRequest) (promotioncommit.SettleResult, error)
}

// StreamKeyFor names the ledger stream one workflow instance's governed
// outcome is recorded on. The canonical owner is the settlement
// capability; this alias keeps the workflow's own readers on the same
// reference.
func StreamKeyFor(workflowID, instanceID string) string {
	return promotioncommit.StreamKeyFor(workflowID, instanceID)
}

// CorrelationUUID derives the ledger's uuid correlation identifier from
// the workflow instance's own free-text correlation id. The canonical
// owner is the settlement capability; this alias keeps the workflow's own
// readers on the same mapping.
func CorrelationUUID(correlation string) uuid.UUID {
	return promotioncommit.CorrelationUUID(correlation)
}

// decisionRefsForInstance reads every work_item_decision row WORK-010
// recorded for the instance's completed work items and splits their ids by
// kind. Work items are the workflow plane's own approvals and tasks, so
// this read stays in the workflow layer: it builds the capability's input,
// never a domain write. It runs inside the same transaction as the
// terminal settlement, never a second copy of that evidence.
func decisionRefsForInstance(ctx context.Context, tx dbport.Tx, tenantID, instanceID uuid.UUID) (approvalIDs, taskIDs []string, err error) {
	store := workitem.Store{}
	items, err := store.ListForInstance(ctx, tx, tenantID, instanceID)
	if err != nil {
		return nil, nil, fmt.Errorf("effects: list work items for instance %s: %w", instanceID, err)
	}
	for _, item := range items {
		if item.Status != workitem.StatusCompleted {
			continue
		}
		rec, loadErr := workitem.LoadDecision(ctx, tx, tenantID, item.WorkItemID)
		if loadErr != nil {
			if workitem.CodeOf(loadErr) == workitem.CodeWorkItemNotFound {
				// No WORK-010 decision row exists for this item. That is a
				// gap this write reports rather than papers over.
				return nil, nil, fmt.Errorf("effects: completed work item %s has no recorded decision", item.WorkItemID)
			}
			return nil, nil, fmt.Errorf("effects: load decision for work item %s: %w", item.WorkItemID, loadErr)
		}
		switch rec.Kind {
		case workitem.DecisionKindApproval:
			approvalIDs = append(approvalIDs, rec.DecisionID.String())
		case workitem.DecisionKindTask:
			taskIDs = append(taskIDs, rec.DecisionID.String())
		}
	}
	sort.Strings(approvalIDs)
	sort.Strings(taskIDs)
	return approvalIDs, taskIDs, nil
}

// LedgerTerminalWriter is the [execute.TerminalWriter] for a workflow
// instance's COMPLETE continuation. It reads the instance's own completed
// work-item decisions and invokes the promotion settlement capability
// ([promotioncommit.Settler]), which owns the governed business writes --
// the outcome ledger event, projection checkpoint and outbox message, the
// payload-schema registration and the admission-guard and budget-hold
// releases -- inside tx. The writer records only the capability's typed
// result.
//
// It performs no idempotency reservation of its own: the caller
// (internal/workflow/execute's own continuation sink) already wraps this
// call in [idempotency.Guard], and the settlement's own
// IdempotencyKey makes a literal retry of the exact settlement a safe
// no-op at the ledger's own layer too, belt-and-braces with the guard
// above it.
type LedgerTerminalWriter struct {
	Appender       ledgerport.Appender
	ProjectionName string
	SourceRef      string
	// Settler is the promotion settlement capability. A nil Settler uses
	// the production capability built from Appender, ProjectionName and
	// SourceRef; tests supply a recording double to prove the writer
	// records only the typed capability result.
	Settler PromotionSettler
}

var _ execute.TerminalWriter = (*LedgerTerminalWriter)(nil)

// Write implements [execute.TerminalWriter].
func (w *LedgerTerminalWriter) Write(ctx context.Context, tx dbport.Tx, req execute.TerminalWriteRequest) (ret0 idempotency.ResultIdentity, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.effects.ledger_terminal_write", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	settler := w.Settler
	if settler == nil {
		if w.Appender == nil {
			return idempotency.ResultIdentity{}, fmt.Errorf("effects: LedgerTerminalWriter has no ledger Appender bound")
		}
		settler = promotioncommit.Settler{
			Appender:       w.Appender,
			ProjectionName: w.ProjectionName,
			SourceRef:      w.SourceRef,
		}
	}
	approvalIDs, taskIDs, err := decisionRefsForInstance(ctx, tx, req.TenantID, req.InstanceID)
	if err != nil {
		return idempotency.ResultIdentity{}, err
	}
	result, err := settler.Settle(ctx, tx, promotioncommit.SettleRequest{
		TenantID:            req.TenantID,
		WorkflowID:          req.WorkflowID,
		PlanDigest:          req.PlanDigest,
		InstanceID:          req.InstanceID,
		Proposal:            req.Proposal.Revision,
		TerminalCode:        req.TerminalCode,
		CorrelationID:       req.CorrelationID,
		IdempotencyKey:      req.IdempotencyKey,
		RecordedAt:          req.RecordedAt,
		EndNodeID:           req.EndNodeID,
		EndOutputDigest:     req.EndOutputDigest,
		ApprovalDecisionIDs: approvalIDs,
		TaskSubmissionIDs:   taskIDs,
	})
	if err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("effects: settle the terminal promotion: %w", err)
	}
	return idempotency.ResultIdentity{
		ResultRef: result.ResultRef,
		EventRef:  result.EventRef,
	}, nil
}
