package telemetry

import (
	"testing"
	"time"
)

func TestTodo_OBS_008(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	manifest := seedDeliveryManifest()
	healthy, err := Check(DefaultRequiredSignals(), seedHealthySink(), PipelineFaults{}, at)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	evidence := mustPublishEvidence(t, manifest, healthy, at)
	if evidence.Digest == "" {
		t.Fatal("delivery evidence must carry a digest")
	}
	// Seeded defect: a missing telemetry signal still reporting
	// complete is rejected with the offending field/state/version and
	// persists nothing.
	broken, err := Check(DefaultRequiredSignals(), seedGappedSink(), PipelineFaults{}, at)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if broken.Health == HealthHealthy {
		t.Fatal("gapped signals report healthy")
	}
	_, err = publishEvidence(manifest, broken, at)
	rejected, ok := AsDeliveryRejected(err)
	if !ok {
		t.Fatalf("expected OBS_008_REJECTED, got %v", err)
	}
	if rejected.Field == "" || rejected.State == "" || rejected.Version == 0 {
		t.Fatalf("rejection lacks field/state/version: %+v", rejected)
	}
}
