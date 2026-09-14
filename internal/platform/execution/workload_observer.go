package execution

import (
	"context"
	"log/slog"
	"strings"

	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/workload"
)

// WF-RUN-021 telemetry names.
const (
	spanWorkflowAdmission     = "hcmnext.workflow.admission"
	logEventWorkflowAdmission = "workflow.admission"
)

// WorkloadObserver records every WF-RUN-021 admission verdict: a span per
// verdict and a log line at INFO for an admitted start, WARN for a deferred
// one and ERROR for an overloaded one, carrying the violated dimensions and the
// control snapshot the limits came from.
type WorkloadObserver struct {
	provider *hcmotel.Provider
	logger   *slog.Logger
}

// NewWorkloadObserver builds a WorkloadObserver. A nil provider records no
// span; a nil logger falls back to slog.Default so a refusal is never silent.
func NewWorkloadObserver(provider *hcmotel.Provider, logger *slog.Logger) *WorkloadObserver {
	if logger == nil {
		logger = slog.Default()
	}
	return &WorkloadObserver{provider: provider, logger: logger}
}

// Observe matches runtime.WorkloadGate.Observe.
func (o *WorkloadObserver) Observe(ctx context.Context, tenant, workflowID string, verdict workload.Verdict) {
	if o.provider != nil {
		_, span := o.provider.StartExecutionSpan(ctx, tracerName, spanWorkflowAdmission, map[string]string{
			"workflow_id": workflowID, "admission_outcome": string(verdict.Outcome),
		})
		span.End(string(verdict.Outcome), verdict.Outcome == workload.OutcomeOverloaded)
	}
	level := slog.LevelInfo
	switch verdict.Outcome {
	case workload.OutcomeAdmissionDeferred:
		level = slog.LevelWarn
	case workload.OutcomeOverloaded:
		level = slog.LevelError
	}
	dims := make([]string, 0, len(verdict.Violations))
	for _, v := range verdict.Violations {
		dims = append(dims, string(v.Dimension))
	}
	o.logger.LogAttrs(ctx, level, logEventWorkflowAdmission,
		slog.String("tenant_id", tenant),
		slog.String("workflow_id", workflowID),
		slog.String("outcome", string(verdict.Outcome)),
		slog.String("violated_dimensions", strings.Join(dims, ",")),
		slog.String("reason", verdict.Reason()),
		slog.String("limits_snapshot", verdict.SnapshotVersion),
		slog.String("limits_source", verdict.Source),
		slog.String("trace_id", hcmotel.AmbientTraceID(ctx)),
	)
}
