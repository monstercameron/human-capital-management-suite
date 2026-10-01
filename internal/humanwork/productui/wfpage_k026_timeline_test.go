package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	workflowinspect "github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
)

func TestTodo_WFPAGE_026_Browser(t *testing.T) {
	doc, err := ui.RenderToString(workflowHistoryTimelinePage(WorkflowHistoryTimelineProps{
		RunID: "run-1", BackHref: "/workspace/app/workflows/history",
		Timeline: workflowinspect.PageUseTimeline{
			RunID: "run-1", Page: workflowinspect.PageVersionRef{WorkflowID: "workflow.change", WorkflowVersion: 4, PageID: "workflow.change.input", PageVersion: 2},
			Fields: []workflowinspect.PageFieldView{{ID: "public", Value: "shown"}}, Rules: []string{"rule:manager"}, SOPVersions: []string{"sop:2"},
			Audit: []workflowinspect.PageTimelineAuditView{{EventID: "event-1", Operation: "SUBMIT", PageVersion: 2, RecordedAt: "2026-09-29T12:00:00Z"}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"workflow-history-timeline", "page v2", "Submitted page (read only)", "Rules evaluated", "SOP versions", "Page use audit", `data-workflow-page-version="2"`, `aria-label="Submitted page, read only"`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("timeline markup missing %q: %s", want, doc)
		}
	}
	if strings.Contains(doc, "salary") {
		t.Fatal("timeline rendered a field that was not in the authorized projection")
	}
}

func TestTodo_WFPAGE_026(t *testing.T) {
	href := workflowHistoryPageVersionHref("run-1", workflowinspect.PageVersionRef{WorkflowID: "workflow.change", WorkflowVersion: 4, PageID: "workflow.change.input", PageVersion: 2})
	if !strings.Contains(href, "workflow_version=4") || !strings.Contains(href, "page_version=2") || strings.Contains(href, "latest") {
		t.Fatalf("reopen href = %q, want exact page identity", href)
	}
}
