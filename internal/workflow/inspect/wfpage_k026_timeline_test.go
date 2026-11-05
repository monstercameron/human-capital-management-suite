package inspect

import (
	"errors"
	"testing"
	"time"
)

func TestTodo_WFPAGE_026_Security(t *testing.T) {
	event := PageUseAuditEvent{Schema: PageUseAuditSchema, EventID: "event-1", TenantID: "tenant-a", RunID: "run-1", ActorRef: "principal:1", WorkflowID: "workflow.change", WorkflowVersion: 4, PageID: "workflow.change.input", PageVersion: 2, Operation: PageUseSubmit, RecordedAt: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
	timeline, err := BuildPageUseTimeline(PageUseTimelineRequest{RunID: "run-1", Page: PageVersionRef{WorkflowID: "workflow.change", WorkflowVersion: 4, PageID: "workflow.change.input", PageVersion: 2}, Fields: map[string]string{"public": "shown", "salary": "secret"}, ReadableFields: map[string]bool{"public": true, "salary": false}, Audit: []PageUseAuditEvent{event}})
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline.Fields) != 1 || timeline.Fields[0].ID != "public" || timeline.Fields[0].Value != "shown" {
		t.Fatalf("read-only fields = %+v, want only authorized field", timeline.Fields)
	}
	if len(timeline.Audit) != 1 || timeline.Audit[0].PageVersion != 2 {
		t.Fatalf("audit timeline = %+v", timeline.Audit)
	}
}

func TestTodo_WFPAGE_026(t *testing.T) {
	event := PageUseAuditEvent{Schema: PageUseAuditSchema, EventID: "event-1", TenantID: "tenant-a", RunID: "run-1", ActorRef: "principal:1", WorkflowID: "workflow.change", WorkflowVersion: 4, PageID: "workflow.change.input", PageVersion: 2, Operation: PageUseRepair, RecordedAt: time.Now().UTC()}
	_, err := BuildPageUseTimeline(PageUseTimelineRequest{RunID: "run-1", Page: PageVersionRef{WorkflowID: "workflow.change", WorkflowVersion: 4, PageID: "workflow.change.input", PageVersion: 2}, Audit: []PageUseAuditEvent{event}})
	if err != nil {
		t.Fatalf("BuildPageUseTimeline() error = %v", err)
	}
	wrong := event
	wrong.PageVersion = 3

	bogus := event
	bogus.Operation = "BOGUS"
	if _, err := BuildPageUseTimeline(PageUseTimelineRequest{RunID: "run-1", Page: PageVersionRef{WorkflowID: "workflow.change", WorkflowVersion: 4, PageID: "workflow.change.input", PageVersion: 2}, Audit: []PageUseAuditEvent{bogus}}); !errors.Is(err, ErrPageUseAuditInvalid) {
		t.Fatalf("bad operation error = %v, want ErrPageUseAuditInvalid", err)
	}
	if _, err := BuildPageUseTimeline(PageUseTimelineRequest{RunID: "run-1", Page: PageVersionRef{WorkflowID: "workflow.change", WorkflowVersion: 4, PageID: "workflow.change.input", PageVersion: 2}, Audit: []PageUseAuditEvent{wrong}}); !errors.Is(err, ErrPageTimelineInvalid) {
		t.Fatalf("wrong page version error = %v, want ErrPageTimelineInvalid", err)
	}
}
