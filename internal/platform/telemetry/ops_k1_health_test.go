package telemetry

import (
	"testing"
	"time"

	telemetryhealth "github.com/monstercameron/human-capital-management-suite/internal/operations/telemetryhealth"
)

func structObservation(now time.Time) telemetryhealth.Observation {
	return telemetryhealth.Observation{
		CollectorOK: true, ExporterOK: true, PolicyOK: true,
		Watermark: now.Add(-time.Minute), ObservedAt: now.Add(-time.Minute), FailureSince: now,
	}
}

func TestTodo_OPS_003_LiveBridge(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	got, err := EvaluatePipelineHealth(DefaultPipelineHealthPolicy(), structObservation(now), now)
	if err != nil || got.Status != "HEALTHY" || got.EvidenceQuality != "COMPLETE" {
		t.Fatalf("live telemetry health=%+v err=%v", got, err)
	}

	missing := structObservation(now)
	missing.ExporterOK = false
	missing.FailureSince = now.Add(-11 * time.Minute)
	got, err = EvaluatePipelineHealth(DefaultPipelineHealthPolicy(), missing, now)
	if err != nil || got.Status != "UNKNOWN" || got.EvidenceQuality != "INCOMPLETE" || got.Incident == nil || !got.Incident.Bounded {
		t.Fatalf("live telemetry failure=%+v err=%v", got, err)
	}
}
