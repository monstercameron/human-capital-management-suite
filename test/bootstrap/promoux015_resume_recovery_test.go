package bootstrap_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
)

// failFirstApprovalVote wraps the composed executor's approval kernel and
// fails the first vote after its decision rows are written and before the
// workflow advances, inside the kernel's own transaction.
type failFirstApprovalVote struct {
	app.ProposalExecutor
	kernel app.ApprovalKernel
	failed atomic.Bool
}

func (f *failFirstApprovalVote) CompleteApproval(ctx context.Context, req app.ApprovalVoteRequest) (app.ApprovalVoteResult, error) {
	if record := req.Record; record != nil && !f.failed.Load() {
		req.Record = func(ctx context.Context, ex workitem.Executor, completed workitem.WorkItem) error {
			if err := record(ctx, ex, completed); err != nil {
				return err
			}
			f.failed.Store(true)
			return errors.New("injected failure after the approval decision was written")
		}
	}
	return f.kernel.CompleteApproval(ctx, req)
}

func (f *failFirstApprovalVote) InvalidateApproval(ctx context.Context, req app.ApprovalInvalidationRequest) (app.ExecutionResult, error) {
	return f.kernel.InvalidateApproval(ctx, req)
}

// WF-STEP-018 (formerly PROMOUX-015's two-step recovery): the WorkItem
// decision and the driver advancement are one transaction. A failure between
// them leaves no decision behind, and the same authorized decision then
// completes the promotion with exactly one terminal fact.
func TestTodo_PROMOUX_015_Recovery_ApprovalResumeAfterCommittedDecision(t *testing.T) {
	var failing *failFirstApprovalVote
	h := newJourneyHarness(t, func(cfg *app.CellConfig) {
		kernel, ok := cfg.Executor.(app.ApprovalKernel)
		if !ok {
			t.Fatal("the composed executor provides no approval kernel")
		}
		failing = &failFirstApprovalVote{ProposalExecutor: cfg.Executor, kernel: kernel}
		cfg.Executor = failing
	})
	proposer := h.operatorCtx(t)
	proposal, err := h.engine.Propose(proposer, journeyProposalFor("omar-reyes"))
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if _, err := h.engine.Execute(proposer, proposal.IntentID); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	decision := workspace.Decision{Approve: true, Reason: "approved for recovery test"}
	if _, err := h.engine.Decide(h.approverCtx(t), proposal.IntentID, decision); err == nil {
		t.Fatal("first Decide succeeded despite the injected failure")
	}
	if failing == nil || !failing.failed.Load() {
		t.Fatal("the injected failure was not exercised")
	}
	parked, err := h.engine.Inspect(proposer, proposal.IntentID)
	if err != nil {
		t.Fatalf("Inspect after the failed decision: %v", err)
	}
	if parked.Summary.Stage != workspace.JourneyStageAwaitingApproval || len(parked.WorkItems) != 1 || parked.WorkItems[0].Status == workitem.StatusCompleted {
		t.Fatalf("the failed decision committed apart from the advancement: stage=%s items=%+v", parked.Summary.Stage, parked.WorkItems)
	}
	completed, err := h.engine.Decide(h.approverCtx(t), proposal.IntentID, decision)
	if err != nil {
		t.Fatalf("authorized retry failed: %v", err)
	}
	if completed.Summary.Stage != workspace.JourneyStageCompleted || completed.Instance == nil || completed.Ledger == nil {
		t.Fatalf("recovered promotion did not complete: %+v", completed.Summary)
	}
	if got := ledgerEventsOn(t, h.cell, completed.Instance.InstanceID); got != 1 {
		t.Fatalf("recovery recorded %d terminal ledger events, want one", got)
	}
}
