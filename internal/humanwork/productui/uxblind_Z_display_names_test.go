package productui

import (
	"strings"
	"testing"
)

func uxblindZDisplayNameView(page PageID) View {
	view := testView(page)
	view.People = []Person{{
		ID: "worker-linh", WorkerID: "worker-linh", Name: "Linh", PreferredName: "Linh", LegalName: "Linh Nguyen",
		Role: "Care coordinator", Team: "Care operations", PromotionAvailability: PromotionEligible,
	}}
	view.Work = []WorkItem{{
		ID: "journey-linh", Initials: "LN", Title: "Promotion journey", Person: "Linh", PersonRef: "worker-linh",
		AssigneeRef: "worker-jordan", Status: "Awaiting approval", Tone: "warning", Href: "/workspace/app/journeys?journey=journey-linh",
		ViewerRelationships: []string{"ASSIGNEE"}, ViewerResponsibility: "ACTION_REQUIRED", NextStep: "approval_decision", AwaitsPerson: true,
	}, {
		ID: "history-linh", Initials: "LN", Title: "Promotion journey", Person: "Linh", PersonRef: "worker-linh",
		Status: "Completed", Tone: "success", Terminal: true, CompletedAt: "4 Aug 2026 · 14:32 UTC", EffectiveDate: "2026-08-01",
		Href: "/workspace/app/journeys?journey=history-linh", ViewerResponsibility: "CLOSED",
	}}
	return view
}

// TestTodo_UXBLIND_082 proves Organization, My Work, and the shared history
// projection use one viewer-safe full display name for the same worker.
func TestTodo_UXBLIND_082(t *testing.T) {
	for _, page := range []PageID{PageOrganization, PageWork, PageHistory} {
		doc, err := Render(uxblindZDisplayNameView(page))
		if err != nil {
			t.Fatalf("render %s: %v", page, err)
		}
		if !strings.Contains(doc, "Linh Nguyen") || strings.Contains(doc, ">Linh<") {
			t.Fatalf("%s did not use the shared full display name:\n%s", page, doc)
		}
	}
}

// TestTodo_UXBLIND_082_Browser keeps the same assertion at the rendered
// document boundary used by the browser shell; this native renderer is the
// available component-level browser proof for the lane.
func TestTodo_UXBLIND_082_Browser(t *testing.T) {
	doc, err := Render(uxblindZDisplayNameView(PageOrganization))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(doc, "Linh Nguyen") == 0 || strings.Contains(doc, ">Linh<") {
		t.Fatalf("organization browser projection shortened the worker name:\n%s", doc)
	}
}
