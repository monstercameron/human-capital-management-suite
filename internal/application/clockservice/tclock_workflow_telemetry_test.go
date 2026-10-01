package clockservice

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/buildinfo"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/logging"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

func newClockWorkflowTelemetryFixture(t *testing.T) (*ClockWorkflowTelemetry, *hcmotel.Provider, *bytes.Buffer) {
	t.Helper()
	allow, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatalf("DefaultAllowlist: %v", err)
	}
	resource := telemetry.NewResourceFromBuild(buildinfo.Info{Revision: "clock-test"}, "clock-test", "instance-1", "test", "cell-1", "us-east-1", telemetry.ProcessRoleAPI, telemetry.TenantClassStandard)
	evaluator := telemetry.NewEvaluator(allow, telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion), telemetry.DefaultSamplingPolicy())
	provider, err := hcmotel.NewProvider(context.Background(), hcmotel.Config{
		Resource: resource, Evaluator: evaluator, ShutdownTimeout: time.Second,
		Trace: hcmotel.TraceConfig{StdoutWriter: io.Discard}, Metric: hcmotel.MetricConfig{StdoutWriter: io.Discard},
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	var buf bytes.Buffer
	logger := slogForTest(&buf)
	adapter, err := NewClockWorkflowTelemetry(provider, logger, func() time.Time { return time.Unix(100, 0).UTC() })
	if err != nil {
		t.Fatalf("NewClockWorkflowTelemetry: %v", err)
	}
	t.Cleanup(func() { provider.Shutdown(context.Background()) })
	return adapter, provider, &buf
}

// testClockWorkflowTelemetry is retained for adjacent clock workflow tests
// that share this package's fixture contract.
func testClockWorkflowTelemetry(t *testing.T) (*ClockWorkflowTelemetry, *hcmotel.Provider, *bytes.Buffer) {
	return newClockWorkflowTelemetryFixture(t)
}

func slogForTest(buf *bytes.Buffer) *slog.Logger {
	return slog.New(logging.NewHandler(buf, logging.WithService("clock-test"), logging.WithClock(func() time.Time { return time.Unix(100, 0).UTC() })))
}

func validClockWorkflowLink() ClockWorkflowLink {
	return ClockWorkflowLink{
		Source:   "DEVICE",
		TenantID: "tenant-ref", SessionID: "session-ref", ObservationID: "observation-ref", DeviceID: "device-ref", DeviceSequence: 7,
		InstanceID: "instance-ref", WorkflowID: "workflow-ref", PlanDigest: "sha256:plan", CorrelationID: "corr-ref", TraceID: "trace-ref",
		NodeID: "commit_punch", Attempt: 2, Outcome: "SUCCEEDED",
	}
}

func TestTodo_TCLOCK_WorkflowTelemetryRequiresStrictComposition(t *testing.T) {
	var logger *slog.Logger
	clock := func() time.Time { return time.Unix(1, 0) }
	if _, err := NewClockWorkflowTelemetry(nil, logger, clock); err == nil {
		t.Fatal("nil provider was accepted")
	}
	adapter, provider, _ := newClockWorkflowTelemetryFixture(t)
	if adapter.Instrumentation() == nil {
		t.Fatal("valid composition returned no engine instrumentation")
	}
	if adapter.Recorder() == nil {
		t.Fatal("valid composition returned no workflow recorder")
	}
	if _, err := NewClockWorkflowTelemetry(provider, nil, clock); err == nil {
		t.Fatal("nil logger was accepted")
	}
	if _, err := NewClockWorkflowTelemetry(provider, slogForTest(&bytes.Buffer{}), nil); err == nil {
		t.Fatal("nil clock was accepted")
	}
	if _, err := NewClockWorkflowTelemetry(nil, slogForTest(&bytes.Buffer{}), clock); err == nil {
		t.Fatal("nil provider was accepted with logger")
	}
}

func TestTodo_TCLOCK_WorkflowTelemetryAcceptedLinkIsStableAndRedacted(t *testing.T) {
	adapter, _, buf := newClockWorkflowTelemetryFixture(t)
	link := validClockWorkflowLink()
	if err := adapter.RecordAccepted(context.Background(), link); err != nil {
		t.Fatalf("RecordAccepted: %v", err)
	}
	first := buf.String()
	if err := adapter.RecordAccepted(context.Background(), link); err != nil {
		t.Fatalf("RecordAccepted retry: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, `"message":"clock.workflow.accepted"`) ||
		!strings.Contains(got, `"observation_id":"observation-ref"`) ||
		!strings.Contains(got, `"device_sequence":7`) ||
		!strings.Contains(got, `"instance_id":"instance-ref"`) ||
		!strings.Contains(got, `"plan_digest":"sha256:plan"`) ||
		!strings.Contains(got, `"correlation_id":"corr-ref"`) ||
		!strings.Contains(got, `"trace_id":"trace-ref"`) ||
		!strings.Contains(got, `"node_id":"commit_punch"`) ||
		!strings.Contains(got, `"attempt":2`) ||
		!strings.Contains(got, `"outcome":"SUCCEEDED"`) {
		t.Fatalf("accepted link omitted from log: %s", got)
	}
	if strings.Contains(first, "1234") || strings.Contains(first, "photo-bytes") || strings.Contains(first, "photo_bytes") || strings.Contains(first, "pin") {
		t.Fatalf("sensitive material reached log: %s", first)
	}
	if strings.Count(buf.String(), `"message":"clock.workflow.accepted"`) != 2 {
		t.Fatalf("retry should produce two stable join records: %s", buf.String())
	}
}

func TestTodo_TCLOCK_WorkflowTelemetryRejectsIncompleteLink(t *testing.T) {
	adapter, _, _ := newClockWorkflowTelemetryFixture(t)
	for name, mutate := range map[string]func(*ClockWorkflowLink){
		"missing correlation":     func(l *ClockWorkflowLink) { l.CorrelationID = "" },
		"missing trace":           func(l *ClockWorkflowLink) { l.TraceID = "" },
		"missing device sequence": func(l *ClockWorkflowLink) { l.DeviceSequence = 0 },
		"missing attempt":         func(l *ClockWorkflowLink) { l.Attempt = 0 },
		"control character":       func(l *ClockWorkflowLink) { l.SessionID = "session\nref" },
	} {
		t.Run(name, func(t *testing.T) {
			link := validClockWorkflowLink()
			mutate(&link)
			if err := adapter.RecordAccepted(context.Background(), link); err == nil {
				t.Fatal("incomplete link was accepted")
			}
		})
	}
}

func TestTodo_TCLOCK_WorkflowTelemetryWorkerSelfHasNoDevice(t *testing.T) {
	adapter, _, buf := newClockWorkflowTelemetryFixture(t)
	link := validClockWorkflowLink()
	link.Source, link.DeviceID, link.DeviceSequence = "WORKER_SELF", "", 0
	if err := adapter.RecordAccepted(context.Background(), link); err != nil {
		t.Fatalf("worker-self link rejected: %v", err)
	}
	if !strings.Contains(buf.String(), `"source":"WORKER_SELF"`) {
		t.Fatalf("source missing from worker-self record: %s", buf.String())
	}
}

func TestTodo_TCLOCK_WorkflowTelemetryManualClockOutLink(t *testing.T) {
	adapter, _, buf := newClockWorkflowTelemetryFixture(t)
	link := validClockWorkflowLink()
	link.WorkflowID = "hcmnext.workflows.time.clock_in_out"
	link.NodeID = "commit_clock_out"
	if err := adapter.RecordAccepted(context.Background(), link); err != nil {
		t.Fatalf("manual clock-out link rejected: %v", err)
	}
	if !strings.Contains(buf.String(), `"node_id":"commit_clock_out"`) {
		t.Fatalf("clock-out node missing from telemetry: %s", buf.String())
	}
}

func TestTodo_TCLOCK_WorkflowTelemetryRecorderUsesSharedEngineOperationPath(t *testing.T) {
	adapter, _, buf := newClockWorkflowTelemetryFixture(t)
	ctx, op := adapter.Recorder().Start(context.Background(), "workflow.start", observe.Attrs{observe.KeyCorrelation: "corr-ref"})
	op.End("DENIED", errors.New("pin=1234 photo-bytes=secret"))
	if ctx == nil {
		t.Fatal("recorder returned nil context")
	}
	line := buf.String()
	if !strings.Contains(line, "workflow.start") || !strings.Contains(line, "DENIED") {
		t.Fatalf("shared recorder did not emit operation outcome: %s", line)
	}
	if strings.Contains(line, "1234") || strings.Contains(line, "photo-bytes") || strings.Contains(line, "secret") {
		t.Fatalf("raw operation error reached telemetry: %s", line)
	}
}
