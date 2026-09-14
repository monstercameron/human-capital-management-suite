package productui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXAUDIT_019(t *testing.T) {
	view := testView(PageHistory)
	view.Appearance.Density = "compact"
	view.HistoryQuery = "Avery"
	view.HistoryOutcome = "completed"
	view.HistorySort = historySortClosed
	view.HistoryDirection = "desc"
	markup, err := ui.RenderToString(ui.CreateElement(WorkflowHistory, workflowHistoryProps(view, "", "Workflow History", "Review past workflows.", true)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(markup, `id="workflow-history-title"`) != 1 || !strings.Contains(markup, `class="surface workflow-history history-density-compact"`) {
		t.Fatalf("History did not render one titled compact surface: %s", markup)
	}
	for _, want := range []string{`role="table"`, `aria-sort="descending"`, `Open record`, `Page 1 of 1`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("History missing inspectable contract %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "Jordan Lee") {
		t.Fatal("active work leaked into terminal History")
	}
}

func TestTodo_UXAUDIT_019_Integration(t *testing.T) {
	view := testView(PageHistory)
	view.Work = append(view.Work,
		WorkItem{ID: "authorized-terminal", Person: "Elena Ruiz", PersonRef: "worker-elena", Status: "Completed", Terminal: true, EffectiveDate: "2026-07-01", CompletedAt: "2 Jul 2026 · 09:00 UTC", Href: "/workspace/app/journeys?journey=authorized-terminal"},
		WorkItem{ID: "withheld-terminal", Person: "Jordan Lee", PersonRef: "worker-jordan", Status: "Completed", Terminal: true, EffectiveDate: "2026-06-01", CompletedAt: "2 Jun 2026 · 09:00 UTC"},
	)
	// This is the server adapter's record-level answer: a present verdict map
	// is authoritative for the work population, including terminal records.
	view.RecordVerdicts = map[string]AuthorizedRecord{
		"intent-2":            {ID: "intent-2", Disclosable: true},
		"authorized-terminal": {ID: "authorized-terminal", Disclosable: true},
		"withheld-terminal":   {ID: "withheld-terminal", Disclosable: false},
	}
	for i := 0; i < 9; i++ {
		id := fmt.Sprintf("authorized-%02d", i)
		view.Work = append(view.Work, WorkItem{ID: id, Person: fmt.Sprintf("Worker %02d", i), PersonRef: "worker-avery", Status: "Completed", Terminal: true, EffectiveDate: "2026-05-01", CompletedAt: "2 May 2026 · 09:00 UTC"})
		view.RecordVerdicts[id] = AuthorizedRecord{ID: id, Disclosable: true}
	}
	view = ApplyRequest(view, PageRequest{Page: PageHistory, HistorySort: historySortPerson, HistoryDirection: "asc", HistoryPageSize: 10})
	props := workflowHistoryProps(view, "", "History", "", true)
	if props.TotalCount != 11 || props.FilteredCount != 11 || len(props.Items) != 10 {
		t.Fatalf("authorized paged projection = total %d filtered %d rows %d, want 11/11/10", props.TotalCount, props.FilteredCount, len(props.Items))
	}
	if props.Items[0].Person != "Avery Patel" || props.Pagination == nil || props.Pagination.PageCount != 2 {
		t.Fatalf("server-backed page one = %+v pagination=%+v", props.Items, props.Pagination)
	}
	if strings.Contains(props.Items[0].Person, "Jordan") {
		t.Fatal("withheld terminal record was returned by History projection")
	}
}

func TestTodo_UXAUDIT_019_Browser(t *testing.T) {
	view := testView(PageHistory)
	view.HistorySort = historySortPerson
	view.HistoryDirection = "asc"
	markup, err := ui.RenderToString(ui.CreateElement(WorkflowHistory, workflowHistoryProps(view, "", "History", "", true)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`name="history_q"`, `name="outcome"`, `name="history_person"`, `name="history_year"`, `>Employee ↑</a>`, `href="/workspace/app/history?history_dir=desc&amp;history_sort=person"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("browser-addressable History control missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `href=""`) || strings.Contains(markup, "javascript:") {
		t.Fatal("History emitted a dead or script navigation target")
	}
}

func TestTodo_UXAUDIT_019_Accessibility(t *testing.T) {
	view := testView(PageHistory)
	view.HistoryQuery = "does-not-exist"
	markup, err := ui.RenderToString(ui.CreateElement(WorkflowHistory, workflowHistoryProps(view, "", "History", "", true)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`aria-labelledby="workflow-history-title"`, `role="search"`, `for="history-search"`, `aria-label="Search workflow history"`, `role="status"`, "No workflow records found", "No past workflows match this view."} {
		if !strings.Contains(markup, want) {
			t.Fatalf("History accessibility/empty contract missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `role="row"`) {
		t.Fatal("empty History rendered a data row")
	}
}

func TestTodo_UXAUDIT_019_Performance(t *testing.T) {
	view := testView(PageHistory)
	for i := 0; i < 500; i++ {
		view.Work = append(view.Work, WorkItem{ID: fmt.Sprintf("terminal-%03d", i), Person: fmt.Sprintf("Person %03d", i), PersonRef: "worker-avery", Status: "Completed", Terminal: true, EffectiveDate: "2026-01-01", CompletedAt: time.Date(2026, 1, 1, i%24, i%60, 0, 0, time.UTC).Format(time.RFC3339)})
	}
	start := time.Now()
	props := workflowHistoryProps(view, "", "History", "", true)
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("500-record History projection took %s", elapsed)
	}
	if props.FilteredCount != 501 || len(props.Items) != defaultPageSize {
		t.Fatalf("large History projection lost pagination: filtered=%d rows=%d", props.FilteredCount, len(props.Items))
	}
}

func TestTodo_UXAUDIT_019_Regression(t *testing.T) {
	global := testView(PageHistory)
	person := testView(PagePerson)
	global.HistoryQuery, person.HistoryQuery = "Promotion", "Promotion"
	global.HistorySort, person.HistorySort = historySortClosed, historySortClosed
	global.HistoryDirection, person.HistoryDirection = "desc", "desc"
	globalProps := workflowHistoryPropsForTarget(global, "", PageHistory, "History", "", true)
	personProps := workflowHistoryPropsForTarget(person, "worker-avery", PagePerson, "Past workflows", "", true)
	globalMarkup, err := ui.RenderToString(ui.CreateElement(WorkflowHistory, globalProps))
	if err != nil {
		t.Fatal(err)
	}
	personMarkup, err := ui.RenderToString(ui.CreateElement(WorkflowHistory, personProps))
	if err != nil {
		t.Fatal(err)
	}
	for _, markup := range []string{globalMarkup, personMarkup} {
		if strings.Count(markup, `class="history-table"`) != 1 || !strings.Contains(markup, `class="history-filter"`) {
			t.Fatalf("scope did not use the shared History table/filter components: %s", markup)
		}
	}
	if strings.Contains(personMarkup, "All employees") || strings.Contains(personMarkup, "Jordan Lee") {
		t.Fatal("person History diverged into a cross-employee or unrelated record view")
	}
}
