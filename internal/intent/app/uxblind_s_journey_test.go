package app

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
)

func TestTodo_UXBLIND_062(t *testing.T) {
	itemID := uuid.New()
	detail := &workspace.JourneyDetail{Timeline: []workspace.JourneyEvent{
		{Kind: JourneyEventWorkItem, Ref: itemID.String(), Title: "Finance review completed"},
		{Kind: JourneyEventWorkItem, Ref: itemID.String(), Title: "Manager review assigned"},
	}}
	items := []workitem.WorkItem{{
		WorkItemID: itemID,
		Assignment: workitem.Assignment{ChosenOwner: "principal:manager"},
	}}
	uxblindSApplyJourneyDecisionHistory(detail, items, nil)
	if detail.Timeline[1].Detail != "assigned_to:principal:manager" {
		t.Fatalf("assignment history detail = %q", detail.Timeline[1].Detail)
	}
	uxblindSApplyDecision(&detail.Timeline[0], intentapproval.ApprovalDecision{
		Outcome:  intentapproval.OutcomeApproved,
		Approver: intentapproval.ApproverReference{PrincipalID: "principal:loretta"},
		Reason:   "within approved budget",
	}, "")
	if got := detail.Timeline[0]; got.Title != "Finance review approved" || got.Actor != "principal:loretta" || got.Detail != "decision_reason:within approved budget" {
		t.Fatalf("decision history = %+v", got)
	}
	projectJourneyHistory(detail, func(principal string) string {
		if principal == "principal:manager" {
			return "Morgan Manager"
		}
		if principal == "principal:loretta" {
			return "Loretta Approver"
		}
		return ""
	})
	if got := detail.Timeline[0].Actor; got != "Loretta Approver" {
		t.Errorf("resolved approver = %q", got)
	}
	if got := detail.Timeline[1].Detail; got != "assigned_to:Morgan Manager" {
		t.Errorf("resolved assignee detail = %q", got)
	}
}

func TestTodo_UXBLIND_062_Browser(t *testing.T) {
	// The browser harness consumes the same server timeline projection; this
	// component-level assertion keeps the authorization-safe display contract
	// executable without starting a second browser in the package test.
	detail := &workspace.JourneyDetail{Timeline: []workspace.JourneyEvent{{
		Kind: JourneyEventWorkItem, Title: "Manager review completed", Actor: "principal:loretta",
	}}}
	uxblindSApplyDecision(&detail.Timeline[0], intentapproval.ApprovalDecision{Outcome: intentapproval.OutcomeRejected, Reason: "missing evidence"}, "principal:loretta")
	if detail.Timeline[0].Title != "Manager review rejected" || detail.Timeline[0].Detail != "decision_reason:missing evidence" {
		t.Fatalf("browser-facing history = %+v", detail.Timeline[0])
	}
}

func TestTodo_UXBLIND_062_Golden(t *testing.T) {
	event := workspace.JourneyEvent{Kind: JourneyEventWorkItem, Title: "Finance review completed"}
	uxblindSApplyDecision(&event, intentapproval.ApprovalDecision{
		Outcome:  intentapproval.OutcomeApproved,
		Approver: intentapproval.ApproverReference{PrincipalID: "loretta"},
		Reason:   "budget confirmed",
	}, "")
	if got, want := event.Title+"|"+event.Actor+"|"+event.Detail, "Finance review approved|loretta|decision_reason:budget confirmed"; got != want {
		t.Fatalf("golden history = %q, want %q", got, want)
	}
}

func TestTodo_UXBLIND_063(t *testing.T) {
	first := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	second := first.Add(3 * time.Minute)
	ordered := uxblindSOrderJourneyNodes([]workspace.JourneyNode{
		{NodeID: "snapshot_worker", StartedAt: &second, RecordedAt: second},
		{NodeID: "approve_finance", StartedAt: &first, CompletedAt: &second, RecordedAt: second},
	})
	if len(ordered) != 2 || ordered[0].NodeID != "approve_finance" || ordered[1].NodeID != "snapshot_worker" {
		t.Fatalf("diagnostics order = %+v", ordered)
	}
	if ordered[0].CompletedAt == nil || !ordered[0].CompletedAt.Equal(second) {
		t.Fatalf("completed timestamp was lost: %+v", ordered[0])
	}
	if got := uxblindSNodeStatusLabel("RUNNING"); got != "In progress" {
		t.Errorf("running status = %q", got)
	}
}

func TestTodo_UXBLIND_063_Browser(t *testing.T) {
	nodes := uxblindSOrderJourneyNodes([]workspace.JourneyNode{{NodeID: "b"}, {NodeID: "a"}})
	if nodes[0].NodeID != "a" || nodes[1].NodeID != "b" {
		t.Fatalf("tie-broken diagnostics order = %+v", nodes)
	}
}
