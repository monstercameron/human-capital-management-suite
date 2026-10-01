package productui

import (
	"strings"
	"testing"
)

func TestTodo_UXSCAN_005(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`@media (min-width:1081px){.people-directory .data-table-scroll{max-height:none;min-height:0;overflow:visible;}`,
		`@media (min-width:1200px){.history-filter-controls{display:grid;grid-template-columns:`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("missing desktop scroll/filter contract %q", want)
		}
	}
}

func TestTodo_UXSCAN_005_Browser(t *testing.T) {
	for _, page := range []PageID{PagePeople, PageHistory} {
		view := NewView(page, "HarborCare", "viewer", "scope")
		if page == PagePeople {
			view.People = []Person{{ID: "worker-1", Name: "Ari", WorkerNumber: "HC-1"}}
		}
		markup, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if page == PagePeople && (!strings.Contains(markup, `id="people-directory-table-viewport"`) || !strings.Contains(markup, `main-scroll`)) {
			t.Fatal("People lost its shared table and page scroll regions")
		}
		if page == PageHistory && (!strings.Contains(markup, `class="history-filter workflow-history-filters"`) || !strings.Contains(markup, `id="workflow-history-query"`) || !strings.Contains(markup, `placeholder="Search workflow history"`)) {
			t.Fatal("History lost its shared filter controls")
		}
		if page == PageHistory {
			for _, label := range []string{"Workflow", "Requester", "Status", "Subject", "Started from", "Started through", "Sort by"} {
				if !strings.Contains(markup, ">"+label+"<") {
					t.Fatalf("History filter lost full label %q", label)
				}
			}
		}
	}
}

func TestTodo_UXSCAN_005_Accessibility(t *testing.T) {
	view := NewView(PagePeople, "HarborCare", "viewer", "scope")
	view.People = []Person{{ID: "worker-1", Name: "Ari", WorkerNumber: "HC-1"}}
	markup, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `aria-label="Authorized people"`) || !strings.Contains(markup, `tabIndex="0"`) {
		t.Fatal("table viewport is not named and keyboard reachable")
	}
	historyView := NewView(PageHistory, "HarborCare", "viewer", "scope")
	historyView.Work = []WorkItem{{ID: "history-1", Title: "Promotion", Person: "Ari", StatusKey: "COMPLETED", ViewerRelationships: []string{"INITIATOR"}}}
	historyMarkup, err := Render(historyView)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`role="search"`, `for="workflow-history-query"`, `class="labeled-control"`, `aria-label="Workflow history table"`} {
		if !strings.Contains(historyMarkup, want) {
			t.Fatalf("History accessibility contract missing %q", want)
		}
	}
}

func TestTodo_UXSCAN_005_Regression(t *testing.T) {
	css := Stylesheet()
	if !strings.Contains(css, `@media (max-width:760px){.history-filter-controls{grid-template-columns:1fr;}`) {
		t.Fatal("narrow History filters lost their single-column layout")
	}
	for _, want := range []string{
		`.workflow-history-page{overflow:visible;}`,
		`.workflow-history-page .history-filter.workflow-history-filters .history-filter-controls>.labeled-control>span{overflow:visible;overflow-wrap:anywhere;text-overflow:clip;white-space:normal;}`,
		`.workflow-history-page .history-filter.workflow-history-filters .history-filter-controls input,.workflow-history-page .history-filter.workflow-history-filters .history-filter-controls select{overflow:visible;overflow-wrap:anywhere;text-overflow:clip;white-space:normal;}`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("History full-label contract missing %q", want)
		}
	}
}
