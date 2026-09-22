package otel_test

import (
	"context"
	"testing"

	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
)

func TestExecutionSpanLateAttributesRespectExportPolicy(t *testing.T) {
	h := newTestHarness(t, testEvaluator(t))
	t.Cleanup(func() { _ = h.Provider.Shutdown(context.Background()) })
	_, span := h.Provider.StartExecutionSpan(context.Background(), "test", "workflow.test", nil)
	span.SetAttributes(map[string]string{"logical_operation_id": "instance-1", "salary": "private-value", "node_id": "approve"})
	span.End("SUCCESS", false)
	if err := h.Provider.ForceFlush(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	spans := h.SpanExporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("span count=%d", len(spans))
	}
	if got, _ := attrValue(spans[0].Attributes, "logical_operation_id"); got != "instance-1" {
		t.Fatalf("late instance=%q", got)
	}
	if got, _ := attrValue(spans[0].Attributes, "node_id"); got != "approve" {
		t.Fatalf("late node=%q", got)
	}
	if _, ok := attrValue(spans[0].Attributes, "salary"); ok {
		t.Fatal("late attribute bypassed redaction")
	}
	var zero hcmotel.ExecutionSpan
	zero.SetAttributes(map[string]string{"node_id": "ignored"})
}
