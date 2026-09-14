package operations

import (
	"testing"
	"time"
)

func ops009Plan(now time.Time) ContinuityPlan {
	return ContinuityPlan{
		ID: "vendor-incumbent-hris", Version: "v3", Vendor: "incumbent-hris",
		Owner: "platform-ops", BackupOwner: "platform-ops-deputy",
		Fallback: "read-only continuity on cached snapshots", ExpiresAt: now.Add(90 * 24 * time.Hour),
	}
}

func ops009Drill() ContinuityDrill {
	now := ops009Now()
	return ContinuityDrill{
		ID: "drill-outage-001", PlanID: "vendor-incumbent-hris", Kind: DrillOutage,
		StartedAt: now.Add(-2 * time.Hour), NewWorkStoppedAt: now.Add(-115 * time.Minute),
		InFlightPreserved: true, InFlightReconciled: true, CredentialsRevoked: true,
		FallbackActivated: true, DrainedAt: now.Add(-60 * time.Minute),
		RolledBackAt: now.Add(-30 * time.Minute), ReopenedAt: now.Add(-10 * time.Minute),
		EvidenceRef: "drill-evidence-001",
	}
}

func ops009Now() time.Time { return time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC) }

// TestTodo_OPS_009_Fault: every skipped protection fails the drill, and
// stale operating evidence never satisfies readiness.
func TestTodo_OPS_009_Fault(t *testing.T) {
	now := ops009Now()
	plan := ops009Plan(now)
	full := ops009Drill()

	skips := map[string]func(*ContinuityDrill){
		"in_flight_preserved":  func(d *ContinuityDrill) { d.InFlightPreserved = false },
		"in_flight_reconciled": func(d *ContinuityDrill) { d.InFlightReconciled = false },
		"credentials_revoked":  func(d *ContinuityDrill) { d.CredentialsRevoked = false },
		"fallback_activated":   func(d *ContinuityDrill) { d.FallbackActivated = false },
	}
	for field, skip := range skips {
		drill := full
		skip(&drill)
		res := EvaluateContinuity([]ContinuityPlan{plan}, []ContinuityDrill{drill}, now)
		if res.Status != StatusContinuityRejected {
			t.Fatalf("%s skipped yet READY", field)
		}
		found := false
		for _, dg := range res.Diagnostics {
			if dg.Code == StatusContinuityRejected && dg.Field == field {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: diagnostics=%+v", field, res.Diagnostics)
		}
	}
	// Out-of-order drain/rollback/reopen fails.
	disordered := full
	disordered.RolledBackAt = disordered.DrainedAt.Add(-time.Minute)
	if res := EvaluateContinuity([]ContinuityPlan{plan}, []ContinuityDrill{disordered}, now); res.Status != StatusContinuityRejected {
		t.Fatal("rollback before drain satisfied readiness")
	}
	// An exception without expiry, or with a lapsed one, fails.
	noExpiry := full
	noExpiry.ExceptionID = "exception-1"
	if res := EvaluateContinuity([]ContinuityPlan{plan}, []ContinuityDrill{noExpiry}, now); res.Status != StatusContinuityRejected {
		t.Fatal("unexpiring exception satisfied readiness")
	}
	lapsed := full
	lapsed.ExceptionID = "exception-1"
	lapsed.ExceptionExpiry = now.Add(-time.Hour)
	if res := EvaluateContinuity([]ContinuityPlan{plan}, []ContinuityDrill{lapsed}, now); res.Status != StatusContinuityRejected {
		t.Fatal("lapsed exception satisfied readiness")
	}
	// Unknown drill kind and orphan drill fail.
	unknown := full
	unknown.Kind = "CHAOS"
	if res := EvaluateContinuity([]ContinuityPlan{plan}, []ContinuityDrill{unknown}, now); res.Status != StatusContinuityRejected {
		t.Fatal("unknown drill kind satisfied readiness")
	}
	orphan := full
	orphan.PlanID = "no-such-plan"
	if res := EvaluateContinuity([]ContinuityPlan{plan}, []ContinuityDrill{orphan}, now); res.Status != StatusContinuityRejected {
		t.Fatal("planless drill satisfied readiness")
	}
}

// TestTodo_OPS_009_Security: continuity evidence is tenant-free operating
// fact — drills bind to vendors, never to tenant payload — and ownership
// stays independent.
func TestTodo_OPS_009_Security(t *testing.T) {
	now := ops009Now()
	selfOwned := ops009Plan(now)
	selfOwned.BackupOwner = selfOwned.Owner
	if res := EvaluateContinuity([]ContinuityPlan{selfOwned}, []ContinuityDrill{ops009Drill()}, now); res.Status != StatusContinuityRejected {
		t.Fatal("shared owner/backups satisfied readiness")
	}
	// Every kind drills under the same rules; no kind bypasses protection.
	for _, kind := range []DrillKind{DrillOutage, DrillExit, DrillMaintenance, DrillEmergencyChange} {
		drill := ops009Drill()
		drill.ID = "drill-" + string(kind)
		drill.Kind = kind
		drill.CredentialsRevoked = false
		res := EvaluateContinuity([]ContinuityPlan{ops009Plan(now)}, []ContinuityDrill{drill}, now)
		if res.Status != StatusContinuityRejected {
			t.Fatalf("kind %s bypassed credential revocation", kind)
		}
	}
	if !DrillOutage.Valid() || DrillKind("CHAOS").Valid() {
		t.Fatal("drill kind validation is wrong")
	}
}

// TestTodo_OPS_009_Mutation: boundary mutants die — expiry instants,
// duplicate identities, empty plans and drills.
func TestTodo_OPS_009_Mutation(t *testing.T) {
	now := ops009Now()
	plan := ops009Plan(now)
	drill := ops009Drill()

	// Expiry exactly at now is already expired.
	edge := plan
	edge.ExpiresAt = now
	if res := EvaluateContinuity([]ContinuityPlan{edge}, []ContinuityDrill{drill}, now); res.Status != StatusContinuityRejected {
		t.Fatal("plan expiring exactly now satisfied readiness")
	}
	dupe := drill
	if res := EvaluateContinuity([]ContinuityPlan{plan}, []ContinuityDrill{drill, dupe}, now); res.Status != StatusContinuityRejected {
		t.Fatal("duplicate drill id satisfied readiness")
	}
	if res := EvaluateContinuity(nil, []ContinuityDrill{drill}, now); res.Status != StatusContinuityRejected {
		t.Fatal("planless evaluation satisfied readiness")
	}
	if res := EvaluateContinuity([]ContinuityPlan{plan}, nil, now); res.Status != StatusContinuityRejected {
		t.Fatal("drill-less evaluation satisfied readiness")
	}
	ready := EvaluateContinuity([]ContinuityPlan{plan}, []ContinuityDrill{drill}, now)
	if !ready.Ready() || ready.Plans != 1 || ready.Drills != 1 {
		t.Fatalf("result=%+v", ready)
	}
}
