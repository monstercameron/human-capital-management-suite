package operations

import (
	"testing"
	"time"
)

// OPS-009 RED: vendor continuity drill contract before ops009.go exists.
func TestTodo_OPS_009(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	plan := ContinuityPlan{
		ID: "vendor-incumbent-hris", Version: "v3", Vendor: "incumbent-hris",
		Owner: "platform-ops", BackupOwner: "platform-ops-deputy",
		Fallback: "read-only continuity on cached snapshots", ExpiresAt: now.Add(90 * 24 * time.Hour),
	}
	drill := ContinuityDrill{
		ID: "drill-outage-001", PlanID: "vendor-incumbent-hris", Kind: DrillOutage,
		StartedAt: now.Add(-2 * time.Hour), NewWorkStoppedAt: now.Add(-115 * time.Minute),
		InFlightPreserved: true, InFlightReconciled: true, CredentialsRevoked: true,
		FallbackActivated: true, DrainedAt: now.Add(-60 * time.Minute),
		RolledBackAt: now.Add(-30 * time.Minute), ReopenedAt: now.Add(-10 * time.Minute),
		EvidenceRef: "drill-evidence-001",
	}
	res := EvaluateContinuity([]ContinuityPlan{plan}, []ContinuityDrill{drill}, now)
	if res.Status != StatusContinuityReady {
		t.Fatalf("status=%q diagnostics=%+v, want READY", res.Status, res.Diagnostics)
	}

	// RED seeded defect: expired ownership continuity satisfies readiness.
	stale := plan
	stale.ExpiresAt = now.Add(-time.Hour)
	res = EvaluateContinuity([]ContinuityPlan{stale}, []ContinuityDrill{drill}, now)
	if res.Status != StatusContinuityRejected {
		t.Fatal("expired continuity plan satisfied readiness, want OPS_009_REJECTED")
	}
	found := false
	for _, d := range res.Diagnostics {
		if d.Code == StatusContinuityRejected && d.Field == "expires_at" {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics=%+v, want expiry attribution", res.Diagnostics)
	}
}
