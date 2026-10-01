package clockservice

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/logging"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// ClockWorkflowLink is the safe application-level join between an accepted
// clock observation and the workflow execution that committed it. Values are
// opaque references or digests; the type deliberately has no credential or
// artifact-byte fields.
type ClockWorkflowLink struct {
	Source         string
	TenantID       string
	SessionID      string
	ObservationID  string
	DeviceID       string
	DeviceSequence int64
	InstanceID     string
	WorkflowID     string
	PlanDigest     string
	CorrelationID  string
	TraceID        string
	NodeID         string
	Attempt        int
	Outcome        string
}

// ClockWorkflowTelemetry owns the one production workflow instrumentation
// adapter and the application join log emitted after the first durable punch
// result is accepted. It never creates or advances a workflow itself.
type ClockWorkflowTelemetry struct {
	instrumentation execute.Instrumentation
	recorder        observe.Recorder
	logger          *slog.Logger
	clock           func() time.Time
}

// NewClockWorkflowTelemetry composes the existing production workflow engine
// instrumentation. A provider, logger and trusted clock are all required so
// a clock workflow cannot silently fall back to an unobserved or nondetermin-
// istic composition.
func NewClockWorkflowTelemetry(provider *hcmotel.Provider, logger *slog.Logger, clock func() time.Time) (*ClockWorkflowTelemetry, error) {
	if provider == nil {
		return nil, errors.New("clock workflow telemetry: provider is required")
	}
	if logger == nil {
		return nil, errors.New("clock workflow telemetry: logger is required")
	}
	if clock == nil {
		return nil, errors.New("clock workflow telemetry: clock is required")
	}
	return &ClockWorkflowTelemetry{
		instrumentation: execution.NewOTelInstrumentation(provider, logger, clock),
		recorder:        execution.NewObserveRecorder(provider, logger, clock),
		logger:          logger,
		clock:           clock,
	}, nil
}

// Recorder returns the shared workflow operation recorder used by execute
// Options.Recorder for start, lease, retry and other engine operations.
func (t *ClockWorkflowTelemetry) Recorder() observe.Recorder {
	if t == nil {
		return nil
	}
	return t.recorder
}

// Instrumentation returns the existing engine port used by execute.Driver.
// The returned value is the sole workflow instrumentation path for a clock
// composition; callers must pass it into the engine rather than wrapping a
// second driver or span implementation.
func (t *ClockWorkflowTelemetry) Instrumentation() execute.Instrumentation {
	if t == nil {
		return nil
	}
	return t.instrumentation
}

// Validate proves that the adapter is fully composed for a production clock
// workflow. Composition roots call it after construction and before wiring the
// engine so a partially initialized telemetry path fails closed.
func (t *ClockWorkflowTelemetry) Validate() error {
	if t == nil || t.instrumentation == nil || t.recorder == nil || t.logger == nil || t.clock == nil {
		return errors.New("clock workflow telemetry: incomplete composition")
	}
	return nil
}

// RecordAccepted records the durable application join for one accepted punch.
// Replaying the same link preserves the same identity fields because no
// identifier is minted here; envelope timestamps remain logger-owned. The
// engine's node, advance and terminal spans remain the source
// of trace and attempt detail; this line joins those details to clock facts.
func (t *ClockWorkflowTelemetry) RecordAccepted(ctx context.Context, link ClockWorkflowLink) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if err := link.validate(); err != nil {
		return err
	}
	if link.CorrelationID != "" {
		ctx = logging.WithCorrelationID(ctx, link.CorrelationID)
	}
	t.logger.LogAttrs(ctx, slog.LevelInfo, "clock.workflow.accepted",
		slog.String("source", link.Source),
		slog.String("tenant_id", link.TenantID),
		slog.String("session_id", link.SessionID),
		slog.String("observation_id", link.ObservationID),
		slog.String("device_id", link.DeviceID),
		slog.Int64("device_sequence", link.DeviceSequence),
		slog.String("instance_id", link.InstanceID),
		slog.String("workflow_id", link.WorkflowID),
		slog.String("plan_digest", link.PlanDigest),
		slog.String("correlation_id", link.CorrelationID),
		slog.String("trace_id", link.TraceID),
		slog.String("node_id", link.NodeID),
		slog.Int("attempt", link.Attempt),
		slog.String("outcome", link.Outcome),
	)
	return nil
}

func (l ClockWorkflowLink) validate() error {
	if l.Source != "DEVICE" && l.Source != "WORKER_SELF" && l.Source != "IMPORT" {
		return errors.New("clock workflow telemetry: source must be DEVICE, WORKER_SELF or IMPORT")
	}
	for name, value := range map[string]string{
		"tenant_id": l.TenantID, "session_id": l.SessionID,
		"observation_id": l.ObservationID,
		"instance_id":    l.InstanceID, "workflow_id": l.WorkflowID,
		"plan_digest": l.PlanDigest, "correlation_id": l.CorrelationID,
		"trace_id": l.TraceID, "node_id": l.NodeID, "outcome": l.Outcome,
	} {
		if strings.TrimSpace(value) == "" {
			return errors.New("clock workflow telemetry: " + name + " is required")
		}
		if len([]rune(value)) > 128 {
			return errors.New("clock workflow telemetry: " + name + " exceeds 128 characters")
		}
		for _, r := range value {
			if unicode.IsControl(r) {
				return errors.New("clock workflow telemetry: " + name + " contains control characters")
			}
		}
	}
	if l.Source == "DEVICE" {
		if strings.TrimSpace(l.DeviceID) == "" {
			return errors.New("clock workflow telemetry: device_id is required for device source")
		}
		if len([]rune(l.DeviceID)) > 128 {
			return errors.New("clock workflow telemetry: device_id exceeds 128 characters")
		}
		for _, r := range l.DeviceID {
			if unicode.IsControl(r) {
				return errors.New("clock workflow telemetry: device_id contains control characters")
			}
		}
	}
	if l.Source == "DEVICE" && (strings.TrimSpace(l.DeviceID) == "" || l.DeviceSequence <= 0) {
		return errors.New("clock workflow telemetry: device source requires device id and positive sequence")
	}
	if l.Source != "DEVICE" && (l.DeviceID != "" || l.DeviceSequence != 0) {
		return errors.New("clock workflow telemetry: non-device source cannot carry device identity")
	}
	if l.Outcome != "SUCCEEDED" {
		return errors.New("clock workflow telemetry: outcome must be SUCCEEDED")
	}
	if l.NodeID != "commit_punch" && !(l.NodeID == "commit_clock_out" && l.WorkflowID == "hcmnext.workflows.time.clock_in_out") {
		return errors.New("clock workflow telemetry: node must be commit_punch, or commit_clock_out for the manual clock workflow")
	}
	if l.Attempt <= 0 {
		return errors.New("clock workflow telemetry: attempt must be positive")
	}
	return nil
}
