package telemetry

import (
	"time"

	telemetryhealth "github.com/monstercameron/human-capital-management-suite/internal/operations/telemetryhealth"
)

// DefaultPipelineHealthPolicy is the live platform default for OPS-003. The
// returned value is copied, so callers cannot mutate shared process state.
func DefaultPipelineHealthPolicy() telemetryhealth.Policy {
	return telemetryhealth.Policy{
		ID:              "telemetry-pipeline",
		Version:         "v1",
		StalenessBound:  5 * time.Minute,
		IncidentAfter:   10 * time.Minute,
		MaxQueueDepth:   100,
		MaxDropCount:    0,
		MaxExportErrors: 0,
	}
}

// EvaluatePipelineHealth keeps telemetry pipeline evidence on the live
// platform boundary while delegating the deterministic classification and
// bounded incident policy to the operations contract. Missing evidence is
// therefore UNKNOWN, never an accidental product HEALTHY result.
func EvaluatePipelineHealth(policy telemetryhealth.Policy, observation telemetryhealth.Observation, now time.Time) (telemetryhealth.Result, error) {
	return telemetryhealth.Evaluate(policy, observation, now)
}
