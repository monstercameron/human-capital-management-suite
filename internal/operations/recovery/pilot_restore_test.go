package recovery

import (
	"testing"
	"time"
)

// RECOVERY-003 RED: isolated pilot-restore drill before pilot_restore.go.
func TestTodo_RECOVERY_003(t *testing.T) {
	at := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	tenantReq := TenantRestoreRequest{
		TenantID: "harborcare-demo", RecoveryPoint: at.Add(-2 * time.Hour), RestoredAt: at,
		Rows: []RestoredRow{
			{ID: "intent-1", TenantID: "harborcare-demo", Plane: "intent", Digest: "sha256:i1"},
			{ID: "ledger-1", TenantID: "harborcare-demo", Plane: "ledger", Digest: "sha256:l1"},
		},
		DeletedIDs: []string{"worker-gone"}, HeldIDs: []string{"intent-1"},
		LedgerHead: "sha256:head-9", MigrationJournalDigest: "sha256:journal-3",
		RuntimeLeaseEpoch: 11,
		Conformance: RestoreConformance{
			LedgerHeadsValid: true, ForeignKeysValid: true, RuntimeLeasesValid: true,
			HoldsApplied: true, DeletionsApplied: true, HashesValid: true,
		},
		Isolated: true,
	}
	req := PilotRestoreRequest{
		DrillID: "pilot-restore-1", Destination: "recovery-cell-1",
		StartedAt: at.Add(-30 * time.Minute), Tenant: tenantReq,
		Runtime: RestoredRuntimeState{
			Timers: 3, Signals: 2, FrontierNodes: 4, OutboxEntries: 5, IdempotencyKeys: 6,
			StateDigest: "sha256:runtime-1",
		},
	}

	rep, err := DrillPilotRestore(req)
	if err != nil {
		t.Fatalf("DrillPilotRestore: %v", err)
	}
	if rep.Status != RestoreReady {
		t.Fatalf("status=%q findings=%+v, want READY", rep.Status, rep.Findings)
	}
	if rep.RTO <= 0 || rep.RestoredCounts.Timers != 3 || rep.ProductionEffects {
		t.Fatalf("report=%+v, want measured RTO, exact counts and zero production effects", rep)
	}

	// RED: a restore that loses timer state stays fenced.
	lost := req
	lost.Runtime.Timers = 0
	rep, err = DrillPilotRestore(lost)
	if err != nil {
		t.Fatalf("DrillPilotRestore(lost): %v", err)
	}
	if rep.Status != RestoreFenced {
		t.Fatal("timer-losing restore left the fence, want FENCED")
	}

	// RED: production destinations are refused outright.
	live := req
	live.Destination = "production-cell-1"
	if _, err := DrillPilotRestore(live); err == nil {
		t.Fatal("production-destination restore accepted")
	}
}
