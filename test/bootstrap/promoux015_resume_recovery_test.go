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

type failFirstApprovalResume struct {
	app.ProposalExecutor
	failed atomic.Bool
}

func (f *failFirstApprovalResume) Resume(ctx context.Context, req app.ExecutionResumeRequest) (app.ExecutionResult, error) {
	if f.failed.CompareAndSwap(false, true) {
		return app.ExecutionResult{}, errors.New("injected failure after approval commit")
	}
	return f.ProposalExecutor.Resume(ctx, req)
}

// The WorkItem decision and driver advancement are separate durable steps.
// A failed first resume must be recoverable through the same authorized
// decision, without a second decision or a second terminal fact.
func TestTodo_PROMOUX_015_Recovery_ApprovalResumeAfterCommittedDecision(t *testing.T) {
	var failing *failFirstApprovalResume
	h := newJourneyHarness(t, func(cfg *app.CellConfig) {
		failing = &failFirstApprovalResume{ProposalExecutor: cfg.Executor}
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
		t.Fatal("first Decide succeeded despite the injected resume failure")
	}
	if failing == nil || !failing.failed.Load() {
		t.Fatal("the first resume failure was not exercised")
	}
	parked, err := h.engine.Inspect(proposer, proposal.IntentID)
	if err != nil {
		t.Fatalf("Inspect after failed resume: %v", err)
	}
	if parked.Summary.Stage != workspace.JourneyStageAwaitingApproval || len(parked.WorkItems) != 1 || parked.WorkItems[0].Status != workitem.StatusCompleted {
		t.Fatalf("approval did not commit separately from resume: stage=%s items=%+v", parked.Summary.Stage, parked.WorkItems)
	}
	completed, err := h.engine.Decide(h.approverCtx(t), proposal.IntentID, decision)
	if err != nil {
		t.Fatalf("authorized replay failed to resume committed approval: %v", err)
	}
	if completed.Summary.Stage != workspace.JourneyStageCompleted || completed.Instance == nil || completed.Ledger == nil {
		t.Fatalf("recovered promotion did not complete: %+v", completed.Summary)
	}
	if got := ledgerEventsOn(t, h.cell, completed.Instance.InstanceID); got != 1 {
		t.Fatalf("recovery recorded %d terminal ledger events, want one", got)
	}
}
