package execution

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/logging"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/testexport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/progress"
)

// TestProgressObserverRecordsASweepSpanAndBoundedLogLines proves WF-RUN-020's
// sweep is observable on real exporters: one span per sweep carrying its
// outcome and error status, one sweep log line with the counts, and one
// warning line per stuck instance naming the missed expectations and the
// incident key -- with no finding detail text, which can carry row
// identifiers, ever reaching a log line.
func TestProgressObserverRecordsASweepSpanAndBoundedLogLines(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	now := start
	clock := func() time.Time { return now }
	_, provider, spanRec, logRec := newTestInstrumentation(t, clock)
	logger := slog.New(logging.NewHandler(logRec, logging.WithService("test-workflow-progress"), logging.WithClock(clock)))
	obs := NewProgressObserver(provider, logger, clock)
	tenant := "tenant-7"

	ctx, end := obs.StartSweep(fixedTraceContext(context.Background()), tenant)
	snap := progress.Snapshot{Instance: progress.Instance{TenantID: tenant, InstanceID: "inst-1", WorkflowID: "wf.promotion", RuntimeStatus: "RUNNING"}}
	findings := []progress.Finding{{Kind: progress.KindTimerOverdue, NodeID: "wait", Ref: "workflow_timer:t1", Detail: "SECRET-ROW-DETAIL"}}
	obs.Stuck(ctx, snap, findings, "workflow-progress:v1:inst-1:abc", true)
	now = now.Add(40 * time.Millisecond)
	end(progress.SweepResult{Instances: 5, Stuck: 1, Opened: 1}, nil)

	if report := provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatalf("ForceFlush: %v", report.Err())
	}
	span, ok := spanRec.SpanNamed(spanWorkflowProgressSweep)
	if !ok {
		t.Fatalf("no %s span exported", spanWorkflowProgressSweep)
	}
	if span.Status().Code != codes.Ok {
		t.Errorf("sweep span status = %v, want Ok", span.Status())
	}

	sweep := logRec.LinesNamed(logEventProgressSweep)
	if len(sweep) != 1 {
		t.Fatalf("sweep log lines = %d, want 1", len(sweep))
	}
	fields, _ := sweep[0]["attrs"].(map[string]any)
	if fields["stuck"] != float64(1) || fields["instances"] != float64(5) || fields["incidents_opened"] != float64(1) || fields["duration_ms"] != float64(40) {
		t.Errorf("sweep log fields = %v", fields)
	}
	if level, _ := sweep[0]["level"].(string); level != "WARN" {
		t.Errorf("a sweep that found a stuck instance logged at %q, want WARN", level)
	}

	stuck := logRec.LinesNamed(logEventProgressStuck)
	if len(stuck) != 1 {
		t.Fatalf("stuck log lines = %d, want 1", len(stuck))
	}
	stuckFields, _ := stuck[0]["attrs"].(map[string]any)
	if stuckFields["instance_id"] != "inst-1" || stuckFields["finding_kinds"] != "TIMER_OVERDUE" || stuckFields["incident_opened"] != true {
		t.Errorf("stuck log fields = %v", stuckFields)
	}
	for _, line := range logRec.Lines() {
		if strings.Contains(stringify(line), "SECRET-ROW-DETAIL") {
			t.Fatal("finding detail text reached a log line")
		}
	}
}

// TestProgressObserverMarksAFailedSweep proves a failed sweep ends its span
// with an error status and logs at ERROR with a bounded error class, not the
// raw error message.
func TestProgressObserverMarksAFailedSweep(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC) }
	_, provider, spanRec, logRec := newTestInstrumentation(t, clock)
	logger := slog.New(logging.NewHandler(logRec, logging.WithService("test-workflow-progress"), logging.WithClock(clock)))
	obs := NewProgressObserver(provider, logger, clock)

	_, end := obs.StartSweep(context.Background(), "tenant-7")
	end(progress.SweepResult{Stuck: 2, Failed: 2}, errors.New("progress: raise incident for instance 7f3a: incident: alert storm"))
	if report := provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatalf("ForceFlush: %v", report.Err())
	}
	span, ok := spanRec.SpanNamed(spanWorkflowProgressSweep)
	if !ok || span.Status().Code != codes.Error {
		t.Fatalf("failed sweep span = %v (found %v), want Error status", span, ok)
	}
	lines := logRec.LinesNamed(logEventProgressSweep)
	if len(lines) != 1 {
		t.Fatalf("sweep log lines = %d, want 1", len(lines))
	}
	if level, _ := lines[0]["level"].(string); level != "ERROR" {
		t.Errorf("failed sweep logged at %q, want ERROR", level)
	}
	fields, _ := lines[0]["attrs"].(map[string]any)
	if fields["error_class"] != "incident_storm" || strings.Contains(stringify(lines[0]), "7f3a") {
		t.Errorf("failed sweep log fields = %v, want a bounded error class and no row identifier", fields)
	}
}

// TestProgressObserverToleratesMissingSinks proves a composition without a
// provider or logger still sweeps: the observer is a no-op rather than a panic.
func TestProgressObserverToleratesMissingSinks(t *testing.T) {
	obs := NewProgressObserver(nil, nil, nil)
	ctx, end := obs.StartSweep(context.Background(), "tenant-7")
	obs.Stuck(ctx, progress.Snapshot{}, nil, "", false)
	end(progress.SweepResult{}, errors.New("begin read: boom"))
	if got := OutcomeFor(progress.SweepResult{}, nil); got != "SUCCESS" {
		t.Errorf("OutcomeFor(clean) = %q", got)
	}
	if got := errorClass(errors.New("incident conflict")); got != "incident_conflict" {
		t.Errorf("errorClass(conflict) = %q", got)
	}
	if got := errorClass(errors.New("something else")); got != "sweep_failed" {
		t.Errorf("errorClass(other) = %q", got)
	}
}

func stringify(line testexport.LogLine) string {
	var b strings.Builder
	for k, v := range line {
		b.WriteString(k)
		b.WriteString("=")
		switch val := v.(type) {
		case map[string]any:
			for ik, iv := range val {
				b.WriteString(ik + ":")
				if s, ok := iv.(string); ok {
					b.WriteString(s)
				}
				b.WriteString(";")
			}
		case string:
			b.WriteString(val)
		}
		b.WriteString(" ")
	}
	return b.String()
}
