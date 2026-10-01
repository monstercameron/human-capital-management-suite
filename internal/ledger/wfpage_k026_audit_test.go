package ledger

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTodo_WFPAGE_026(t *testing.T) {
	event := PageUseAuditEvent{
		Schema: PageUseAuditSchema, EventID: "event-1", TenantID: "tenant-a", RunID: "run-1", ActorRef: "principal:1",
		WorkflowID: "workflow.change", WorkflowVersion: 7, PageID: "workflow.change.input", PageVersion: 3,
		Operation: PageUseSubmit, RulesDigest: "sha256:rules", SOPVersions: []string{"sop:2"}, RecordedAt: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	payload, err := event.Canonical()
	if err != nil || !strings.Contains(string(payload), `"page_version":3`) || !strings.Contains(string(payload), `"operation":"SUBMIT"`) {
		t.Fatalf("Canonical() = %s, error = %v", payload, err)
	}
	stream, err := PageUseAuditStreamKey(event.TenantID, event.RunID)
	if err != nil || stream != "workflow-page-use/tenant-a/run-1" {
		t.Fatalf("stream = %q, error = %v", stream, err)
	}
}

func TestTodo_WFPAGE_026_Security(t *testing.T) {
	event := PageUseAuditEvent{Schema: PageUseAuditSchema, EventID: "event-1", TenantID: "tenant-a", RunID: "run-1", ActorRef: "principal:1", WorkflowID: "workflow.change", WorkflowVersion: 1, PageID: "page", PageVersion: 1, Operation: PageUseView, RecordedAt: time.Now().UTC()}
	event.PageVersion = 0
	if err := event.Validate(); !errors.Is(err, ErrPageUseAuditInvalid) {
		t.Fatalf("invalid page version error = %v, want ErrPageUseAuditInvalid", err)
	}
	if _, err := PageUseAuditStreamKey("", "run-1"); !errors.Is(err, ErrPageUseAuditInvalid) {
		t.Fatalf("empty tenant stream error = %v", err)
	}
}
