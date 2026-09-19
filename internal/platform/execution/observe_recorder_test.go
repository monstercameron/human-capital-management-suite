package execution

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/logging"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestObserveRecorderEmitsSpanAndLogPerWorkflowOperation proves the workflow
// engine's telemetry seam is real: every operation opened through
// internal/workflow/observe becomes one exported span and one structured log
// line whose level tracks the outcome, whose attributes are the bounded ids,
// and whose error contributes only its classified code -- never message text.
func TestObserveRecorderEmitsSpanAndLogPerWorkflowOperation(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { now = now.Add(5 * time.Millisecond); return now }
	_, provider, spanRec, logRec := newTestInstrumentation(t, clock)
	logger := slog.New(logging.NewHandler(logRec, logging.WithService("test-workflow-observe"), logging.WithClock(clock), logging.WithMinLevel(slog.LevelDebug)))
	ctx := observe.WithRecorder(fixedTraceContext(context.Background()), NewObserveRecorder(provider, logger, clock))

	_, op := observe.Start(ctx, "workflow.lease.acquire", observe.Attrs{observe.KeyTenant: "tenant-1", observe.KeyResource: "WORKFLOW_INSTANCE"})
	op.Set(observe.KeyFence, observe.Int(7))
	observe.Done(op, nil)
	op.End(observe.OutcomeFailure, errors.New("second end ignored"))

	_, op = observe.Start(ctx, "workflow.runtime.advance", observe.Attrs{observe.KeyInstance: "instance-1"})
	observe.Done(op, &runtime.Error{Code: runtime.CodeStaleInstance, Detail: "salary 90000 for person-9"})

	_, op = observe.Start(ctx, "workflow.lease.renew", nil)
	observe.Done(op, errors.Join(lease.ErrStorage, errors.New("password=hunter2")))

	if report := provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatalf("ForceFlush: %v", report.Err())
	}
	spans := spanRec.Spans()
	if len(spans) != 3 {
		t.Fatalf("exported %d spans, want 3 (a second End must not export twice)", len(spans))
	}
	if spans[0].Name() != "hcmnext.workflow.lease.acquire" || spans[0].Status().Code == codes.Error {
		t.Errorf("span 0 = %s %v", spans[0].Name(), spans[0].Status())
	}
	if spans[2].Status().Code != codes.Error {
		t.Errorf("failed operation span status = %v, want Error", spans[2].Status())
	}

	for _, want := range []struct{ event, level, outcome, code string }{
		{"workflow.lease.acquire", "DEBUG", observe.OutcomeSuccess, ""},
		{"workflow.runtime.advance", "WARN", observe.OutcomeRefused, runtime.CodeStaleInstance},
		{"workflow.lease.renew", "ERROR", observe.OutcomeFailure, "ERROR"},
	} {
		lines := logRec.LinesNamed(want.event)
		if len(lines) != 1 {
			t.Fatalf("%s: %d log lines, want 1", want.event, len(lines))
		}
		line := lines[0]
		if line["level"] != want.level {
			t.Errorf("%s logged at %v, want %s", want.event, line["level"], want.level)
		}
		attrs, _ := line["attrs"].(map[string]any)
		if attrs["outcome"] != want.outcome || (want.code != "" && attrs["error_code"] != want.code) {
			t.Errorf("%s attrs = %v", want.event, attrs)
		}
		if attrs["trace_id"] != fixedTraceID.String() {
			t.Errorf("%s trace_id = %v, want %s", want.event, attrs["trace_id"], fixedTraceID)
		}
		if _, ok := attrs["duration_ms"]; !ok {
			t.Errorf("%s has no duration_ms", want.event)
		}
	}
	acquire, _ := logRec.LinesNamed("workflow.lease.acquire")[0]["attrs"].(map[string]any)
	if acquire["fence_token"] != "7" || acquire["tenant_id"] != "tenant-1" {
		t.Errorf("acquire attrs = %v", acquire)
	}
	for _, raw := range logRec.Lines() {
		for _, leak := range []string{"hunter2", "90000", "person-9", "second end ignored"} {
			if strings.Contains(fmt.Sprint(raw), leak) {
				t.Errorf("log line leaks %q: %v", leak, raw)
			}
		}
	}
}

func TestObserveRecorderWithoutProviderStillLogs(t *testing.T) {
	if NewObserveRecorder(nil, nil, nil).logger == nil {
		t.Fatal("nil logger not defaulted")
	}
	clock := func() time.Time { return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC) }
	_, _, _, logRec := newTestInstrumentation(t, clock)
	logger := slog.New(logging.NewHandler(logRec, logging.WithService("test-workflow-observe"), logging.WithClock(clock)))
	rec := NewObserveRecorder(nil, logger, clock)
	_, op := rec.Start(fixedTraceContext(context.Background()), "workflow.timer.fire", observe.Attrs{observe.KeyTimer: "timer-1"})
	op.Set(observe.KeyStatus, "")
	op.End(observe.OutcomeFailure, context.DeadlineExceeded)
	lines := logRec.LinesNamed("workflow.timer.fire")
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	attrs, _ := lines[0]["attrs"].(map[string]any)
	if attrs["error_code"] != "DEADLINE_EXCEEDED" || attrs["trace_id"] != fixedTraceID.String() || attrs["timer_id"] != "timer-1" {
		t.Errorf("attrs = %v", attrs)
	}
	if _, ok := attrs["status"]; ok {
		t.Error("empty attribute value was recorded")
	}
}

// TestObserveRecorderRunCorrelationWinsOverRequest proves an engine line
// joins the run, not the request that happened to drive it: an operation
// naming the run's correlation id replaces the request's in the logging
// context, nested operations inherit it, and request_id still names the
// request.
func TestObserveRecorderRunCorrelationWinsOverRequest(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC) }
	_, _, _, logRec := newTestInstrumentation(t, clock)
	logger := slog.New(logging.NewHandler(logRec, logging.WithService("test-workflow-observe"), logging.WithClock(clock), logging.WithMinLevel(slog.LevelDebug)))
	rec := NewObserveRecorder(nil, logger, clock)
	ctx := logging.WithCorrelationID(logging.WithRequestID(context.Background(), "req:approval"), "req:approval")

	outer, op := rec.Start(ctx, "workflow.execute.complete_approval", observe.Attrs{observe.KeyCorrelation: "req:run"})
	_, inner := rec.Start(outer, "workflow.runtime.instance_transition", nil)
	inner.End(observe.OutcomeSuccess, nil)
	op.End(observe.OutcomeSuccess, nil)

	for _, name := range []string{"workflow.execute.complete_approval", "workflow.runtime.instance_transition"} {
		lines := logRec.LinesNamed(name)
		if len(lines) != 1 {
			t.Fatalf("%s: %d lines, want 1", name, len(lines))
		}
		attrs, _ := lines[0]["attrs"].(map[string]any)
		if lines[0]["correlation_id"] != "req:run" || attrs["correlation_id"] != "req:run" || lines[0]["request_id"] != "req:approval" {
			t.Errorf("%s = %v", name, lines[0])
		}
	}
}
