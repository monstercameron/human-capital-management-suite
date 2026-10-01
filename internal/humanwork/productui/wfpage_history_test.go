package productui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_WFPAGE_007(t *testing.T) {
	view := testView(PageWorkflowHistory)
	view.Work = []WorkItem{
		{ID: "started", Title: "Promotion", Person: "Avery Patel", Requester: "Avery Patel", Participants: "Jordan Lee", StatusKey: "COMPLETED", ViewerRelationships: []string{"INITIATOR"}},
		{ID: "participated", Title: "Promotion", Person: "Jordan Lee", StatusKey: "MANAGER_APPROVAL", ViewerRelationships: []string{"ASSIGNEE"}},
		{ID: "withheld", Title: "Promotion", Person: "Private Person", StatusKey: "COMPLETED", ViewerRelationships: []string{"INITIATOR"}},
	}
	view.RecordVerdicts = map[string]AuthorizedRecord{
		"started":      {ID: "started", Disclosable: true},
		"participated": {ID: "participated", Disclosable: true},
		"withheld":     {ID: "withheld", Disclosable: false},
	}
	runs := workflowHistoryRuns(view)
	if len(runs) != 2 {
		t.Fatalf("authorized history rows = %d, want 2", len(runs))
	}
	started := FilterWorkflowHistoryRuns(runs, WorkflowRunHistoryFilter{Relationship: workflowHistoryRelationshipStarted})
	if len(started) != 1 || started[0].ID != "started" {
		t.Fatalf("started filter = %+v, want only started run", started)
	}
	for _, run := range runs {
		if run.Subject == "Private Person" || run.ID == "withheld" {
			t.Fatal("withheld run crossed the authorized history projection")
		}
	}
	if got := runs[0].StatusGroup; got != JourneyHomeGroupClosed && got != JourneyHomeGroupInProgress {
		t.Fatalf("status group %q is not from the shared taxonomy", got)
	}
	if runs[0].Requester != "Avery Patel" || runs[0].Participants != "Jordan Lee" {
		t.Fatalf("server-projected requester/participants = %q/%q, want authorized display fields", runs[0].Requester, runs[0].Participants)
	}
}

func TestTodo_WFPAGE_007_Integration(t *testing.T) {
	view := testView(PageWorkflowHistory)
	view.Work = []WorkItem{
		{ID: "initiated", Person: "Visible", StatusKey: "RECORDED", ViewerRelationships: []string{"INITIATOR"}},
		{ID: "unrelated", Person: "Not visible", StatusKey: "RECORDED"},
	}
	view.RecordVerdicts = map[string]AuthorizedRecord{
		"initiated": {ID: "initiated", Disclosable: true},
		"unrelated": {ID: "unrelated", Disclosable: false},
	}
	runs := workflowHistoryRuns(view)
	if len(runs) != 1 || runs[0].ID != "initiated" {
		t.Fatalf("server-authorized projection = %+v, want only initiated", runs)
	}
	if got := FilterWorkflowHistoryRuns(runs, WorkflowRunHistoryFilter{StatusGroup: JourneyHomeGroupClosed}); len(got) != 1 {
		t.Fatalf("closed status filter = %d, want 1", len(got))
	}
}

func TestTodo_WFPAGE_008(t *testing.T) {
	view := testView(PageWorkflowHistory)
	view.HistoryRequester = "Avery Patel"
	view.HistoryDirection = "desc"
	view.Work = []WorkItem{{ID: "run-1", Title: "Promotion", Person: "Avery Patel", Requester: "Avery Patel", StatusKey: "COMPLETED", EffectiveDate: "2026-01-02", CompletedAt: "2026-01-03", InstanceVersion: 4, ViewerRelationships: []string{"INITIATOR"}}}
	view.HistoryPageSize = 10
	markup, err := ui.RenderToString(workflowHistoryPage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`workflow-history-filters`, `name="history_q"`, `name="workflow_q"`, `name="outcome"`, `name="history_person"`, `name="history_requester"`, `name="history_dir"`, `name="history_page_size"`, `workflow-history-table`, `Avery Patel`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("workflow history missing %q: %s", want, markup)
		}
	}
}

func TestTodo_WFPAGE_008_Browser(t *testing.T) {
	view := testView(PageWorkflowHistory)
	view.HistoryQuery = "Promotion"
	view.Work = []WorkItem{{ID: "run-1", Title: "Promotion", Person: "Avery Patel", StatusKey: "COMPLETED", ViewerRelationships: []string{"CANDIDATE"}}}
	view.RecordVerdicts = map[string]AuthorizedRecord{"run-1": {ID: "run-1", Disclosable: true}}
	markup, err := ui.RenderToString(workflowHistoryPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `role="search"`) || !strings.Contains(markup, `/workspace/app/workflows/history`) || !strings.Contains(markup, `role="region"`) {
		t.Fatalf("history page is not keyboard/progressive-enhancement addressable: %s", markup)
	}
	if !strings.Contains(markup, `value="Promotion"`) {
		t.Fatalf("history query was not rendered into its address-backed control: %s", markup)
	}
	if !strings.Contains(markup, `option selected value=""`) {
		t.Fatalf("all-status option must submit an explicit empty value so it cannot invalidate the other URL filters: %s", markup)
	}
}

func TestWorkflowHistorySortRoundTripAndLegacyIsolation(t *testing.T) {
	for _, sort := range []string{"started", "updated", "status"} {
		t.Run(sort, func(t *testing.T) {
			view := ApplyRequest(testView(PageWorkflowHistory), PageRequest{Page: PageWorkflowHistory, HistorySort: sort})
			if view.HistorySort != sort {
				t.Fatalf("workflow history sort = %q, want %q", view.HistorySort, sort)
			}
			markup, err := ui.RenderToString(workflowHistoryPage(view))
			if err != nil {
				t.Fatal(err)
			}
			wantOption := `option selected value="` + sort + `"`
			if !strings.Contains(markup, wantOption) {
				t.Fatalf("workflow history did not render selected sort %q: %s", sort, markup)
			}
		})
	}
	legacy := ApplyRequest(testView(PageHistory), PageRequest{Page: PageHistory, HistorySort: "updated"})
	if legacy.HistorySort != "" {
		t.Fatalf("legacy history accepted workflow-only sort %q", legacy.HistorySort)
	}
	legacySort := ApplyRequest(testView(PageHistory), PageRequest{Page: PageHistory, HistorySort: historySortPerson})
	if legacySort.HistorySort != historySortPerson {
		t.Fatalf("legacy history sort = %q, want %q", legacySort.HistorySort, historySortPerson)
	}
}

func TestTodo_WFPAGE_008_Performance(t *testing.T) {
	runs := make([]WorkflowHistoryRun, 10000)
	for i := range runs {
		runs[i] = WorkflowHistoryRun{ID: fmt.Sprintf("run-%05d", i), Workflow: "Promotion", Subject: fmt.Sprintf("Worker %05d", i), StatusGroup: JourneyHomeGroupClosed, Stage: "COMPLETED", StartedAt: "2026-01-01"}
	}
	start := time.Now()
	filtered := FilterWorkflowHistoryRuns(runs, WorkflowRunHistoryFilter{StatusGroup: JourneyHomeGroupClosed, Sort: "started", Direction: "desc", Page: 2, PageSize: 100})
	if len(filtered) != len(runs) || time.Since(start) > 2*time.Second {
		t.Fatalf("10,000-run filter took %s or lost rows (%d)", time.Since(start), len(filtered))
	}
	window := workflowHistoryWindowFor(filtered, 2, 100)
	if len(window.Items) != 100 || window.TotalPages != 100 {
		t.Fatalf("10,000-run page = %d rows/%d pages, want 100/100", len(window.Items), window.TotalPages)
	}
}

func TestTodo_WFPAGE_009(t *testing.T) {
	runs := []WorkflowHistoryRun{{ID: "one", Workflow: "Promotion", Version: "v1", Subject: "Avery Patel", Stage: "COMPLETED", StatusGroup: JourneyHomeGroupClosed}}
	result, err := ExportWorkflowHistoryCSV(runs, WorkflowRunHistoryFilter{})
	if err != nil || result.Audit.RowCount != 1 || !strings.Contains(string(result.CSV), "workflow,version,subject") {
		t.Fatalf("workflow history export = %+v, err=%v", result.Audit, err)
	}
}

func TestTodo_WFPAGE_009_Security(t *testing.T) {
	runs := []WorkflowHistoryRun{{ID: "redacted", Workflow: "Promotion", Subject: "", Requester: "", Participants: "", Stage: "COMPLETED", StatusGroup: JourneyHomeGroupClosed}}
	result, err := ExportWorkflowHistoryCSV(runs, WorkflowRunHistoryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	row := string(result.CSV)
	if !strings.Contains(row, "Promotion") || strings.Contains(row, "person-ref") || strings.Contains(row, "principal-ref") {
		t.Fatalf("export included protected identifiers: %s", row)
	}
}

func TestTodo_WFPAGE_009_Fault(t *testing.T) {
	runs := make([]WorkflowHistoryRun, WorkflowHistoryExportLimit+1)
	for i := range runs {
		runs[i] = WorkflowHistoryRun{ID: fmt.Sprintf("run-%d", i), Workflow: "Promotion", StatusGroup: JourneyHomeGroupClosed}
	}
	result, err := ExportWorkflowHistoryCSV(runs, WorkflowRunHistoryFilter{})
	if err != nil || !result.Audit.Truncated || result.Audit.RowCount != WorkflowHistoryExportLimit {
		t.Fatalf("capped export audit = %+v, err=%v", result.Audit, err)
	}
	if !strings.Contains(result.Notice, "10,000") || len(result.CSV) == 0 {
		t.Fatalf("capped export did not return a complete file and notice")
	}
	failed, err := (WorkflowHistoryReportExportService{Record: func(context.Context, WorkflowHistoryExportAudit) error {
		return fmt.Errorf("audit store unavailable")
	}}).Export(context.Background(), WorkflowHistoryExportRequest{Runs: runs[:1], Now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)})
	if err != ErrWorkflowHistoryExport || len(failed.CSV) != 0 {
		t.Fatalf("interrupted governed export = bytes %d, err %v; want no partial artifact", len(failed.CSV), err)
	}
}
