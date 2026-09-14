package recovery

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func recovery003Request(at time.Time) PilotRestoreRequest {
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
	return PilotRestoreRequest{
		DrillID: "pilot-restore-1", Destination: "recovery-cell-1",
		StartedAt: at.Add(-30 * time.Minute), Tenant: tenantReq,
		Runtime: RestoredRuntimeState{
			Timers: 3, Signals: 2, FrontierNodes: 4, OutboxEntries: 5, IdempotencyKeys: 6,
			StateDigest: "sha256:runtime-1",
		},
	}
}

func recovery003At() time.Time { return time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC) }

// TestTodo_RECOVERY_003_Golden pins the pilot restore drill oracle.
func TestTodo_RECOVERY_003_Golden(t *testing.T) {
	rep, err := DrillPilotRestore(recovery003Request(recovery003At()))
	if err != nil {
		t.Fatalf("DrillPilotRestore: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "pilot_restore.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	oracle := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			t.Fatalf("malformed golden line: %q", line)
		}
		oracle[key] = value
	}
	if rep.Digest != oracle["digest"] {
		t.Fatalf("digest mismatch:\n got=%q\nwant=%q", rep.Digest, oracle["digest"])
	}
	if string(rep.Status) != oracle["status"] || rep.Explain() != oracle["explain"] {
		t.Fatalf("report mismatch:\n got=%q %q\nwant=%q %q", rep.Status, rep.Explain(), oracle["status"], oracle["explain"])
	}
}

// TestTodo_RECOVERY_003_Race: the drill is pure, so concurrent drills over
// shared inputs stay race-free and agree.
func TestTodo_RECOVERY_003_Race(t *testing.T) {
	req := recovery003Request(recovery003At())
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				rep, err := DrillPilotRestore(req)
				if err != nil {
					t.Errorf("DrillPilotRestore: %v", err)
					return
				}
				if rep.Status != RestoreReady || rep.ProductionEffects {
					t.Errorf("report=%+v", rep)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestTodo_RECOVERY_003_Fault: lost runtime state and unconforming data
// fence; malformed envelopes and live destinations error.
func TestTodo_RECOVERY_003_Fault(t *testing.T) {
	req := recovery003Request(recovery003At())

	for _, field := range []string{"signals", "frontier", "outbox", "idempotency"} {
		lost := req
		switch field {
		case "signals":
			lost.Runtime.Signals = 0
		case "frontier":
			lost.Runtime.FrontierNodes = 0
		case "outbox":
			lost.Runtime.OutboxEntries = 0
		case "idempotency":
			lost.Runtime.IdempotencyKeys = 0
		}
		rep, err := DrillPilotRestore(lost)
		if err != nil {
			t.Fatalf("DrillPilotRestore(%s): %v", field, err)
		}
		if rep.Status != RestoreFenced || !hasPilotFinding(rep.Findings, "RUNTIME_STATE_LOST") {
			t.Fatalf("%s: %+v, want fenced with runtime findings", field, rep)
		}
	}
	hashless := req
	hashless.Runtime.StateDigest = ""
	rep, err := DrillPilotRestore(hashless)
	if err != nil {
		t.Fatalf("DrillPilotRestore: %v", err)
	}
	if rep.Status != RestoreFenced || !hasPilotFinding(rep.Findings, "RUNTIME_HASHES_INVALID") {
		t.Fatalf("%+v, want hash fencing", rep)
	}
	unconforming := req
	unconforming.Tenant.Conformance.HashesValid = false
	rep, err = DrillPilotRestore(unconforming)
	if err != nil {
		t.Fatalf("DrillPilotRestore: %v", err)
	}
	if rep.Status != RestoreFenced || !hasPilotFinding(rep.Findings, "DATA_PLANE_UNCONFORMING") {
		t.Fatalf("%+v, want data-plane fencing", rep)
	}
	nodest := req
	nodest.Destination = ""
	if _, err := DrillPilotRestore(nodest); err == nil {
		t.Fatal("destinationless drill accepted")
	}
	backwards := req
	backwards.StartedAt = backwards.Tenant.RestoredAt.Add(time.Hour)
	if _, err := DrillPilotRestore(backwards); err == nil {
		t.Fatal("drill starting after its restore accepted")
	}
	for _, dest := range []string{"production-cell-1", "PRODUCTION", "cell-1", " recovery-cell-1 "} {
		live := req
		live.Destination = dest
		if _, err := DrillPilotRestore(live); err == nil {
			t.Fatalf("destination %q accepted", dest)
		}
	}
}

// TestTodo_RECOVERY_003_Recovery: measured RPO/RTO, exact counts and
// applied manifests make the drill a recovery rehearsal, not a reopening.
func TestTodo_RECOVERY_003_Recovery(t *testing.T) {
	req := recovery003Request(recovery003At())
	rep, err := DrillPilotRestore(req)
	if err != nil {
		t.Fatalf("DrillPilotRestore: %v", err)
	}
	if rep.RPO != 2*time.Hour || rep.RTO != 30*time.Minute {
		t.Fatalf("rpo=%s rto=%s, want measured 2h/30m", rep.RPO, rep.RTO)
	}
	if rep.TombstonesApplied != 1 || rep.HoldsApplied != 1 {
		t.Fatalf("report=%+v, want applied tombstone and hold", rep)
	}
	if rep.RestoredCounts != req.Runtime {
		t.Fatalf("counts=%+v, want the exact runtime inventory", rep.RestoredCounts)
	}
	again, err := DrillPilotRestore(req)
	if err != nil || again.Digest != rep.Digest {
		t.Fatal("repeat drill disagrees")
	}
}

// TestTodo_RECOVERY_003_Mutation: destination and epoch edges resolve on
// the documented side.
func TestTodo_RECOVERY_003_Mutation(t *testing.T) {
	req := recovery003Request(recovery003At())
	upper := req
	upper.Destination = "RECOVERY-CELL-2"
	if rep, err := DrillPilotRestore(upper); err != nil || rep.Status != RestoreReady {
		t.Fatalf("uppercase recovery destination: %+v %v", rep, err)
	}
	trimmed := req
	trimmed.Destination = " recovery-cell-1"
	if _, err := DrillPilotRestore(trimmed); err == nil {
		t.Fatal("padded destination accepted")
	}
	zeroRTO := req
	zeroRTO.StartedAt = zeroRTO.Tenant.RestoredAt
	if rep, err := DrillPilotRestore(zeroRTO); err != nil || rep.RTO != 0 || rep.Status != RestoreReady {
		t.Fatalf("zero RTO drill: %+v %v", rep, err)
	}
}

func hasPilotFinding(findings []PilotRestoreFinding, code string) bool {
	for _, f := range findings {
		if f.Code == code {
			return true
		}
	}
	return false
}
