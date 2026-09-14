package app

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

func TestTodo_PROMOUX_007OrdinaryJourneyProjectionWithholdsDiagnostics(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	detail := workspace.JourneyDetail{
		Summary: workspace.JourneySummary{
			IntentID: "intent-visible-route", CorrelationID: "correlation-secret",
			ProposalRevisionID: "revision-secret", MaterialDigest: "digest-secret",
			InstanceID: "instance-secret", InstanceVersion: 17, Approver: "principal-secret",
		},
		Findings:      []workspace.JourneyFinding{{Code: "rule-secret", Message: "Business explanation"}},
		PlannedWrites: []string{"SET secret.stream"},
		Instance:      &workspace.JourneyInstance{InstanceID: "instance-secret", PlanDigest: "plan-secret"},
		Nodes:         []workspace.JourneyNode{{NodeID: "node-secret", TraceID: "trace-secret"}},
		WorkItems: []workitem.WorkItem{{
			WorkItemID: uuid.MustParse("11111111-1111-4111-8111-111111111111"),
			Kind:       workitem.KindApproval, Status: workitem.StatusAssigned, WorkType: "promotion.review",
			OwnerRef: "owner-secret", DeadlineAt: now.Add(time.Hour), CreatedAt: now,
		}},
		Transitions: []workspace.JourneyTransition{{WorkItemID: "work-secret", Actor: "actor-secret"}},
		Ledger: &workspace.JourneyLedgerEvent{
			StreamKey: "stream-secret", Digest: "ledger-secret", EffectiveAt: now, RecordedAt: now,
		},
		EvidenceIDs: []string{"evidence-secret"},
		Timeline: []workspace.JourneyEvent{
			{Kind: JourneyEventNode, Title: "node-secret", Ref: "node-secret"},
			{Kind: JourneyEventWorkItem, Title: "Assigned", Detail: "route-secret", Ref: "work-secret", Actor: "actor-secret"},
		},
	}

	redactJourneyDiagnostics(&detail)

	if detail.Summary.IntentID != "intent-visible-route" {
		t.Fatalf("route identity was removed: %+v", detail.Summary)
	}
	if detail.Summary.CorrelationID != "" || detail.Summary.MaterialDigest != "" || detail.Instance != nil ||
		len(detail.Nodes) != 0 || len(detail.Transitions) != 0 || len(detail.EvidenceIDs) != 0 || len(detail.PlannedWrites) != 0 {
		t.Fatalf("diagnostics crossed the ordinary projection: %+v", detail)
	}
	if len(detail.WorkItems) != 1 || detail.WorkItems[0].WorkItemID != uuid.Nil || detail.WorkItems[0].OwnerRef != "" || detail.WorkItems[0].Status != workitem.StatusAssigned {
		t.Fatalf("work item was not reduced to safe action state: %+v", detail.WorkItems)
	}
	if detail.Ledger == nil || detail.Ledger.StreamKey != "" || detail.Ledger.RecordedAt != now {
		t.Fatalf("ledger outcome did not retain only safe business timing: %+v", detail.Ledger)
	}
	if len(detail.Timeline) != 1 || detail.Timeline[0].Ref != "" || detail.Timeline[0].Actor != "" || detail.Timeline[0].Detail != "" {
		t.Fatalf("timeline diagnostics remain: %+v", detail.Timeline)
	}
	if detail.Findings[0].Code != "" || detail.Findings[0].Message != "Business explanation" {
		t.Fatalf("finding projection = %+v", detail.Findings[0])
	}
}
