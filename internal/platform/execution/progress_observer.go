package execution

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/progress"
)

// WF-RUN-020 telemetry names.
const (
	spanWorkflowProgressSweep = "hcmnext.workflow.progress.sweep"
	logEventProgressSweep     = "workflow.progress.sweep"
	logEventProgressStuck     = "workflow.progress.stuck"
)

// ProgressObserver is WF-RUN-020's production [progress.Observer]: one span
// and one structured log line per tenant sweep, and one warning line per
// stuck instance naming the missed expectations and the incident they opened
// or linked to. It carries only bounded identifiers and counts, never a
// payload or a principal.
type ProgressObserver struct {
	provider *hcmotel.Provider
	logger   *slog.Logger
	clock    func() time.Time
}

var _ progress.Observer = (*ProgressObserver)(nil)

// NewProgressObserver builds a ProgressObserver. A nil provider records no
// span; a nil logger records no log line; a nil clock uses time.Now in UTC.
func NewProgressObserver(provider *hcmotel.Provider, logger *slog.Logger, clock func() time.Time) *ProgressObserver {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &ProgressObserver{provider: provider, logger: logger, clock: clock}
}

// StartSweep implements progress.Observer.
func (o *ProgressObserver) StartSweep(ctx context.Context, tenant string) (context.Context, func(progress.SweepResult, error)) {
	started := o.clock()
	var span hcmotel.ExecutionSpan
	hasSpan := o.provider != nil
	if hasSpan {
		ctx, span = o.provider.StartExecutionSpan(ctx, tracerName, spanWorkflowProgressSweep, map[string]string{"tenant_id": tenant})
	}
	return ctx, func(result progress.SweepResult, err error) {
		outcome := OutcomeFor(result, err)
		if hasSpan {
			span.End(outcome, err != nil)
		}
		if o.logger == nil {
			return
		}
		level := slog.LevelInfo
		if err != nil {
			level = slog.LevelError
		} else if result.Stuck > 0 {
			level = slog.LevelWarn
		}
		attrs := []slog.Attr{
			slog.String("tenant_id", tenant),
			slog.Int("instances", result.Instances),
			slog.Int("stuck", result.Stuck),
			slog.Int("incidents_opened", result.Opened),
			slog.Int("incidents_linked", result.Linked),
			slog.Int("incidents_failed", result.Failed),
			slog.String("outcome", outcome),
			slog.Int64("duration_ms", o.clock().Sub(started).Milliseconds()),
		}
		if hasSpan {
			attrs = append(attrs, slog.String("trace_id", span.TraceID()))
		}
		if err != nil {
			attrs = append(attrs, slog.String("error_class", errorClass(err)))
		}
		o.logger.LogAttrs(ctx, level, logEventProgressSweep, attrs...)
	}
}

// Stuck implements progress.Observer.
func (o *ProgressObserver) Stuck(ctx context.Context, s progress.Snapshot, findings []progress.Finding, incidentKey string, opened bool) {
	if o.logger == nil {
		return
	}
	kinds := make([]string, 0, len(findings))
	nodes := make([]string, 0, len(findings))
	for _, f := range findings {
		kinds = append(kinds, string(f.Kind))
		if f.NodeID != "" {
			nodes = append(nodes, f.NodeID)
		}
	}
	o.logger.LogAttrs(ctx, slog.LevelWarn, logEventProgressStuck,
		slog.String("tenant_id", s.Instance.TenantID),
		slog.String("instance_id", s.Instance.InstanceID),
		slog.String("workflow_id", s.Instance.WorkflowID),
		slog.String("runtime_status", s.Instance.RuntimeStatus),
		slog.String("finding_kinds", strings.Join(kinds, ",")),
		slog.String("node_ids", strings.Join(nodes, ",")),
		slog.Int("findings", len(findings)),
		slog.String("incident_key", incidentKey),
		slog.Bool("incident_opened", opened),
		slog.String("trace_id", hcmotel.AmbientTraceID(ctx)),
	)
}

// OutcomeFor classifies a sweep for its span and log line.
func OutcomeFor(result progress.SweepResult, err error) string {
	switch {
	case err != nil:
		return "FAILURE"
	case result.Stuck > 0:
		return "STUCK_FOUND:" + strconv.Itoa(result.Stuck)
	default:
		return "SUCCESS"
	}
}

// errorClass reduces an error to a bounded class so no row identifier or
// message text reaches a log line verbatim.
func errorClass(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "storm"), strings.Contains(msg, "STORM"):
		return "incident_storm"
	case strings.Contains(msg, "conflict"), strings.Contains(msg, "CONFLICT"):
		return "incident_conflict"
	case strings.Contains(msg, "begin"), strings.Contains(msg, "tenant"):
		return "database_scope"
	default:
		return "sweep_failed"
	}
}
