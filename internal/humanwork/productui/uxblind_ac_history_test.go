package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_115(t *testing.T) {
	definition, ok := LookupPage(PageWorkflowHistory)
	if !ok {
		t.Fatal("workflow history page is not registered")
	}
	if definition.Route != "/workspace/app/workflows/history" || definition.ParentNav != PageWorkflowStart {
		t.Fatalf("canonical history registration = %+v", definition)
	}
	if definition.LabelKey != "page.workflow_history.label" || definition.TitleKey != "page.workflow_history.title" || definition.SubtitleKey != "page.workflow_history.subtitle" {
		t.Fatalf("canonical history identity keys = %q/%q/%q", definition.LabelKey, definition.TitleKey, definition.SubtitleKey)
	}

	navigation := navigationForRoles(ResolveProductLocale("en-US"), []string{"manager"})
	var workflow NavItem
	historyEntries := 0
	for _, item := range navigation {
		if item.Page == PageWorkflowStart {
			workflow = item
		}
		for _, child := range item.Children {
			if child.Page == PageWorkflowHistory {
				historyEntries++
			}
			if child.Page == PageHistory {
				t.Fatal("legacy history page remained in navigation")
			}
		}
	}
	if workflow.Page != PageWorkflowStart || historyEntries != 1 {
		t.Fatalf("workflow navigation = %+v, history entries = %d", workflow, historyEntries)
	}

	view := ApplyLocale(testView(PageWorkflowHistory), ResolveProductLocale("en-US"))
	view.Work = []WorkItem{{ID: "run-1", Title: "Promotion", Person: "Avery Patel", Requester: "Taylor", Participants: "Jordan Lee", StatusKey: "COMPLETED", EffectiveDate: "2026-09-01", CompletedAt: "2026-09-02", ViewerRelationships: []string{"INITIATOR"}}}
	markup, err := ui.RenderToString(workflowHistoryPage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Workflow history", `class="history-filter workflow-history-filters"`, `class="history-filter-controls"`, `class="labeled-control"`, `class="data-table-scroll"`, `aria-label="Workflow history table"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("canonical history surface missing %q: %s", want, markup)
		}
	}
	if got := strings.Count(markup, "1 record"); got != 1 {
		t.Fatalf("record count occurrences = %d, want one: %s", got, markup)
	}
	if strings.Contains(markup, "Work History") || strings.Contains(markup, "My Work") {
		t.Fatalf("legacy page identity leaked into canonical markup: %s", markup)
	}
}

func TestTodo_UXBLIND_115_Browser(t *testing.T) {
	view := ApplyLocale(testView(PageWorkflowHistory), ResolveProductLocale("de-DE"))
	view.Work = []WorkItem{{ID: "run-1", Title: "Promotion", Person: "Avery Patel", StatusKey: "COMPLETED", ViewerRelationships: []string{"INITIATOR"}}}
	markup, err := ui.RenderToString(workflowHistoryPage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`role="search"`, `for="workflow-history-query"`, `for="workflow-history-status"`, `class="data-table-scroll"`, `role="region"`, "Workflowverlauf"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("responsive/localized history markup missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "Search history") || strings.Contains(markup, "Work History") {
		t.Fatalf("browser surface retained legacy English copy: %s", markup)
	}
}

func TestTodo_UXBLIND_115_Security(t *testing.T) {
	view := testView(PageWorkflowHistory)
	view.Work = []WorkItem{
		{ID: "visible", Title: "Promotion", Person: "Visible worker", Requester: "Taylor", Participants: "Jordan", StatusKey: "COMPLETED", ViewerRelationships: []string{"INITIATOR"}},
		{ID: "hidden", Title: "Promotion", Person: "Private worker", Requester: "Private requester", Participants: "Private participant", StatusKey: "COMPLETED", ViewerRelationships: []string{"INITIATOR"}},
	}
	view.RecordVerdicts = map[string]AuthorizedRecord{
		"visible": {ID: "visible", Disclosable: true},
		"hidden":  {ID: "hidden", Disclosable: false},
	}
	runs := workflowHistoryRuns(view)
	if len(runs) != 1 || runs[0].ID != "visible" {
		t.Fatalf("history projection = %+v, want only authorized run", runs)
	}
	if runs[0].Subject != "Visible worker" || runs[0].Requester != "Taylor" || runs[0].Participants != "Jordan" {
		t.Fatalf("authorized fields = %+v", runs[0])
	}
}
