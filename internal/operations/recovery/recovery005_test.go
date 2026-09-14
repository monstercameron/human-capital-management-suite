package recovery

import (
	"testing"
	"time"
)

func TestCrossPlaneRestoreReconcilesHeadsWatermarksArtifactsAndJournalsWithoutRedrive(t *testing.T) {
	manifest := mustCrossPlaneManifest(t)
	report := mustRestoreConsistencySet(t, ConsistencySetInput{
		Manifest:    manifest,
		Destination: "recovery-cell-1",
		StartedAt:   time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC),
		Planes:      seedConsistentPlanes(),
	})
	if report.Status != SetReady {
		t.Fatalf("status=%v findings=%+v", report.Status, report.Findings)
	}
	if report.Redriven != 0 {
		t.Fatalf("redriven=%d, want 0", report.Redriven)
	}
	if report.Digest == "" {
		t.Fatal("consistency-set report must carry a digest")
	}
	// A regressed watermark fences the set: no silent success.
	regressed := seedConsistentPlanes()
	regressed[1].Watermark = regressed[1].Watermark - 1
	fenced := mustRestoreConsistencySet(t, ConsistencySetInput{
		Manifest:    manifest,
		Destination: "recovery-cell-1",
		StartedAt:   time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC),
		Planes:      regressed,
	})
	if fenced.Status != SetFenced {
		t.Fatalf("regressed watermark passed: %+v", fenced)
	}
}
