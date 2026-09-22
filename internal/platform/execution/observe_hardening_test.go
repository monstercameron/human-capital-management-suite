package execution

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/logging"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestTodo_OBS_025(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }
	_, provider, spans, logs := newTestInstrumentation(t, clock)
	logger := slog.New(logging.NewHandler(logs, logging.WithService("workflow-hardening"), logging.WithClock(clock), logging.WithMinLevel(slog.LevelDebug)))
	recorder := NewObserveRecorder(provider, logger, clock)
	ctx := logging.WithCorrelationID(fixedTraceContext(context.Background()), "request-correlation")
	ctx, outer := recorder.Start(ctx, "workflow.execute.execute", observe.Attrs{observe.KeyCorrelation: "run-correlation"})
	_, inner := recorder.Start(ctx, "workflow.runtime.advance", observe.Attrs{observe.KeyInstance: "instance-1", observe.KeyAttempt: "2"})
	inner.Set(observe.KeyNode, "approval")
	inner.End(observe.OutcomeRefused, &runtime.Error{Code: runtime.CodeStaleInstance, Detail: "secret-salary"})
	outer.Set(observe.KeyInstance, "instance-1")
	outer.Set(observe.KeyCode, "COMPLETE")
	outer.End(observe.OutcomeSuccess, nil)
	outer.Set(observe.KeyInstance, "late-mutation")
	outer.End(observe.OutcomeFailure, errors.New("double-end-secret"))
	if err := provider.ForceFlush(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	if len(spans.Spans()) != 2 {
		t.Fatalf("span count=%d", len(spans.Spans()))
	}
	for _, span := range spans.Spans() {
		attrs := map[string]string{}
		for _, attr := range span.Attributes() {
			attrs[string(attr.Key)] = attr.Value.AsString()
		}
		if attrs["logical_operation_id"] != "instance-1" || attrs["correlation_id"] != "run-correlation" {
			t.Errorf("span %s lost identity: %v", span.Name(), attrs)
		}
		if strings.HasSuffix(span.Name(), "advance") && (attrs["node_id"] != "approval" || attrs["attempt_id"] != "2" || attrs["error_type"] != runtime.CodeStaleInstance) {
			t.Errorf("advance attrs=%v", attrs)
		}
		lines := logs.LinesNamed(strings.TrimPrefix(span.Name(), "hcmnext."))
		if len(lines) != 1 {
			t.Fatalf("log count=%d", len(lines))
		}
		fields := lines[0]["attrs"].(map[string]any)
		if fields["trace_id"] != span.SpanContext().TraceID().String() || lines[0]["correlation_id"] != attrs["correlation_id"] {
			t.Errorf("log/trace correlation mismatch: %v / %v", lines[0], attrs)
		}
	}
}

func TestTodo_OBS_025_Security(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }
	_, provider, spans, logs := newTestInstrumentation(t, clock)
	recorder := NewObserveRecorder(provider, slog.New(logging.NewHandler(logs, logging.WithService("workflow-hardening"))), clock)
	_, op := recorder.Start(context.Background(), "workflow.runtime.advance", observe.Attrs{observe.KeyInstance: "instance-safe"})
	op.End(observe.OutcomeFailure, fmt.Errorf("secret-payroll-payload: %w", context.DeadlineExceeded))
	if err := provider.ForceFlush(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	if len(spans.Spans()) != 1 || len(logs.Lines()) != 1 {
		t.Fatal("missing telemetry")
	}
	for _, span := range spans.Spans() {
		if strings.Contains(fmt.Sprint(span.Attributes(), span.Events(), span.Status()), "secret-payroll-payload") {
			t.Fatal("span leaked error detail")
		}
	}
	if strings.Contains(fmt.Sprint(logs.Lines()), "secret-payroll-payload") {
		t.Fatal("log leaked error detail")
	}
}
