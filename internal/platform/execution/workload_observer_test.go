package execution

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/logging"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/workload"
)

// TestWorkloadObserverTracesAndLogsEveryAdmissionVerdict proves WF-RUN-021's
// admission decisions are observable: every verdict gets a span, and the log
// level escalates with severity -- INFO admitted, WARN deferred, ERROR
// overloaded -- carrying the violated dimensions and the limits' snapshot.
func TestWorkloadObserverTracesAndLogsEveryAdmissionVerdict(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC) }
	_, provider, spanRec, logRec := newTestInstrumentation(t, clock)
	logger := slog.New(logging.NewHandler(logRec, logging.WithService("test-workflow-admission"), logging.WithClock(clock)))
	obs := NewWorkloadObserver(provider, logger)
	ctx := fixedTraceContext(context.Background())

	obs.Observe(ctx, "tenant-1", "wf.promotion", workload.Verdict{Outcome: workload.OutcomeAdmitted, SnapshotVersion: "v1", Source: "default"})
	obs.Observe(ctx, "tenant-1", "wf.promotion", workload.Verdict{Outcome: workload.OutcomeAdmissionDeferred, SnapshotVersion: "v1", Source: "default",
		Violations: []workload.Violation{{Dimension: workload.DimensionConcurrentInstance, Limit: 5, Demand: 6}}})
	obs.Observe(ctx, "tenant-1", "wf.promotion", workload.Verdict{Outcome: workload.OutcomeOverloaded, SnapshotVersion: "v1", Source: "default",
		Violations: []workload.Violation{{Dimension: workload.DimensionCostUnits, Limit: 10, Demand: 11}}})

	if report := provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatalf("ForceFlush: %v", report.Err())
	}
	spans := spanRec.Spans()
	if len(spans) != 3 {
		t.Fatalf("exported %d admission spans, want 3", len(spans))
	}
	if spans[2].Status().Code != codes.Error || spans[0].Status().Code != codes.Ok {
		t.Errorf("span statuses = %v, %v, %v; want the overloaded one marked Error", spans[0].Status(), spans[1].Status(), spans[2].Status())
	}
	lines := logRec.LinesNamed(logEventWorkflowAdmission)
	if len(lines) != 3 {
		t.Fatalf("admission log lines = %d, want 3", len(lines))
	}
	for i, want := range []string{"INFO", "WARN", "ERROR"} {
		if level, _ := lines[i]["level"].(string); level != want {
			t.Errorf("verdict %d logged at %q, want %s", i, level, want)
		}
	}
	fields, _ := lines[2]["attrs"].(map[string]any)
	if fields["violated_dimensions"] != string(workload.DimensionCostUnits) || fields["limits_snapshot"] != "v1" {
		t.Errorf("overloaded log fields = %v", fields)
	}
	if NewWorkloadObserver(nil, nil).logger == nil {
		t.Error("a nil logger was not replaced by the default, so a refusal could be silent")
	}
}
