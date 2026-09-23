package bootstrap_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

func TestTodo_NAAS_001_Integration(t *testing.T) {
	h := newJourneyHarness(t)
	reader, ok := h.engine.(workspace.WorkflowNotificationReader)
	if !ok {
		t.Fatal("live journey engine has no notification reader")
	}
	proposed, err := h.engine.Propose(h.operatorCtx(t), journeyProposal())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.engine.Execute(h.operatorCtx(t), proposed.IntentID); err != nil {
		t.Fatal(err)
	}
	list := func(ctx context.Context) []workspace.WorkflowNotification {
		t.Helper()
		visible, err := h.engine.ListJourneys(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := reader.WorkflowNotifications(ctx, visible)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	rows := list(h.approverCtx(t))
	if len(rows) != 1 || rows[0].JourneyID != proposed.IntentID || rows[0].Status != "ASSIGNED" || rows[0].Read {
		t.Fatalf("approval inbox: %+v", rows)
	}
	if other := list(h.operatorCtx(t)); len(other) != 0 {
		t.Fatalf("requester received another principal's inbox: %+v", other)
	}
	if hidden, err := reader.WorkflowNotifications(h.approverCtx(t), nil); err != nil || len(hidden) != 0 {
		t.Fatalf("no visible workflows: %+v %v", hidden, err)
	}
	// Looking at the inbox is not a decision and cannot record the business write.
	inspected, err := h.engine.Inspect(h.approverCtx(t), proposed.IntentID)
	if err != nil {
		t.Fatal(err)
	}
	if inspected.Ledger != nil || inspected.Summary.Stage != workspace.JourneyStageAwaitingApproval {
		t.Fatal("reading notifications changed approval")
	}
	if _, err := h.engine.Decide(h.approverCtx(t), proposed.IntentID, workspace.Decision{Approve: true, Reason: "reason.promotion_supported/v1"}); err != nil {
		t.Fatal(err)
	}
	after := list(h.approverCtx(t))
	if len(after) != 1 || after[0].ID != rows[0].ID || after[0].Status != "COMPLETED" {
		t.Fatalf("decision not reflected in original notice: %+v", after)
	}
}

func TestTodo_NAAS_001_Security(t *testing.T) {
	h := newJourneyHarness(t)
	reader := h.engine.(workspace.WorkflowNotificationReader)
	if _, err := reader.WorkflowNotifications(context.Background(), nil); err == nil {
		t.Fatal("unauthenticated inbox read admitted")
	}
}
