package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_120(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`.workflow-history-filters .history-filter-controls{`,
		`grid-template-columns:repeat(2,minmax(0,1fr));`,
		`.workflow-history-filters .history-filter-actions{`,
		`grid-column:1/-1;`,
		`@media (max-width:1100px){.workflow-history-filters .history-filter-controls{grid-template-columns:repeat(2,minmax(0,1fr));}`,
		`@media (max-width:600px){.workflow-history-filters .history-filter-controls{grid-template-columns:minmax(0,1fr);}`,
		`.workflow-history-results .data-table-scroll{min-width:0;overflow-x:auto;overflow-y:visible;}`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("workflow history responsive contract missing %q", want)
		}
	}
}

func TestTodo_UXBLIND_120_Browser(t *testing.T) {
	view := ApplyLocale(testView(PageWorkflowHistory), ResolveProductLocale("en-US"))
	view.Work = []WorkItem{{ID: "run-1", Title: "Promotion", Person: "Avery Patel", Requester: "Taylor", StatusKey: "COMPLETED", ViewerRelationships: []string{"INITIATOR"}}}
	markup, err := ui.RenderToString(workflowHistoryPage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="history-filter-actions"`,
		`class="button primary"`,
		`class="button secondary"`,
		">Apply filters<",
		">Clear filters<",
		`class="data-table-scroll"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("responsive history markup missing %q: %s", want, markup)
		}
	}
	if !strings.Contains(markup, `class="history-filter-controls"><label class="labeled-control"`) {
		t.Fatal("history controls lost their labelled-control wrappers")
	}
}
