package journey

import (
	"slices"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// TestJourneyWorkItemSummaryReachesTheWire pins toJourney's UXAUDIT-017
// projection: the engine's viewer-scoped summary is copied field for field
// (regardless of diagnostics authorization, since the engine already applied
// the work item rules), a zero deadline stays unset, and an absent summary
// stays absent rather than becoming an empty message.
func TestJourneyWorkItemSummaryReachesTheWire(t *testing.T) {
	due := time.Date(2026, 10, 1, 17, 0, 0, 0, time.UTC)
	summary := workspace.JourneySummary{IntentID: "intent-1", CurrentWorkItem: &workspace.JourneyWorkItemSummary{
		Kind: "APPROVAL", Status: "ASSIGNED", AssigneePrincipalID: "principal:approver", AssigneeDisplayName: "Ada Approver",
		DueAt: due, ViewerPermittedActions: []string{"claim"}, ViewerMembership: "ASSIGNEE",
	}}
	for _, diag := range []bool{false, true} {
		wire := toJourney(summary, diag).GetCurrentWorkItem()
		if wire == nil {
			t.Fatalf("diag=%t: summary dropped", diag)
		}
		if wire.GetAssigneePrincipalId() != "principal:approver" || wire.GetAssigneeDisplayName() != "Ada Approver" ||
			wire.GetKind() != "APPROVAL" || wire.GetStatus() != "ASSIGNED" || wire.GetViewerMembership() != "ASSIGNEE" ||
			!slices.Equal(wire.GetViewerPermittedActions(), []string{"claim"}) || !wire.GetDueAt().AsTime().Equal(due) {
			t.Fatalf("diag=%t: wire summary = %+v", diag, wire)
		}
	}
	summary.CurrentWorkItem.DueAt = time.Time{}
	if toJourney(summary, false).GetCurrentWorkItem().GetDueAt() != nil {
		t.Fatal("a zero deadline must stay unset on the wire")
	}
	summary.CurrentWorkItem = nil
	if toJourney(summary, false).GetCurrentWorkItem() != nil {
		t.Fatal("an absent summary must stay absent")
	}
}
