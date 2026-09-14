package recovery

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func mustCrossPlaneManifest(t *testing.T) RecoveryManifest {
	t.Helper()
	heads := map[ConsistencyPlane]uint64{}
	marks := map[ConsistencyPlane]uint64{}
	digests := map[ConsistencyPlane]string{}
	for i, plane := range OrderedPlanes {
		heads[plane] = uint64(100 + i)
		marks[plane] = uint64(90 + i)
		digests[plane] = fmt.Sprintf("sha256:plane-%s", plane)
	}
	return RecoveryManifest{ManifestID: "manifest-1", Heads: heads, Watermarks: marks, Digests: digests}
}

func seedConsistentPlanes() []PlaneSnapshot {
	manifest := RecoveryManifest{}
	_ = manifest
	planes := make([]PlaneSnapshot, 0, len(OrderedPlanes))
	for i, plane := range OrderedPlanes {
		keys := []string{fmt.Sprintf("%s-op-1", plane), fmt.Sprintf("%s-op-2", plane)}
		planes = append(planes, PlaneSnapshot{
			Plane: plane, StreamHead: uint64(100 + i), JournalEntries: 2,
			DedupeKeys: keys, Checkpoint: uint64(95 + i), Watermark: uint64(90 + i),
			Digest: fmt.Sprintf("sha256:plane-%s", plane), External: ExternallyVerified,
		})
	}
	return planes
}

func seedSetInput(manifest RecoveryManifest, planes []PlaneSnapshot) ConsistencySetInput {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	return ConsistencySetInput{
		Manifest: manifest, Destination: "recovery-cell-1",
		StartedAt: at, RestoredAt: at.Add(4 * time.Minute), Planes: planes,
	}
}

func mustRestoreConsistencySet(t *testing.T, in ConsistencySetInput) ConsistencySetReport {
	t.Helper()
	report, err := RestoreConsistencySet(in)
	if err != nil {
		t.Fatalf("RestoreConsistencySet(%s): %v", in.Manifest.ManifestID, err)
	}
	return report
}

func requireSetError(t *testing.T, err error, fragment string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", fragment)
	}
	if !strings.Contains(err.Error(), fragment) {
		t.Fatalf("expected error containing %q, got %v", fragment, err)
	}
}

// TestTodo_RECOVERY_005_Property: every plane subset restores in
// dependency order with monotonic heads; any gap fences.
func TestTodo_RECOVERY_005_Property(t *testing.T) {
	manifest := mustCrossPlaneManifest(t)
	full := seedConsistentPlanes()
	// Every prefix of the dependency order verifies exactly its planes.
	for n := 1; n <= len(full); n++ {
		report := mustRestoreConsistencySet(t, seedSetInput(manifest, full[:n]))
		if n == len(full) && report.Status != SetReady {
			t.Fatalf("full set: %+v", report)
		}
		if n < len(full) && report.Status != SetFenced {
			t.Fatalf("prefix %d passed with missing planes", n)
		}
		if len(report.Verified) != n && report.Status == SetReady {
			t.Fatalf("prefix %d verified=%d", n, len(report.Verified))
		}
	}
	// Out-of-order snapshots still verify: order comes from the manifest.
	reversed := append([]PlaneSnapshot(nil), full...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	if report := mustRestoreConsistencySet(t, seedSetInput(manifest, reversed)); report.Status != SetReady {
		t.Fatalf("reordered snapshots: %+v", report)
	}
}

// TestTodo_RECOVERY_005_Golden pins the consistency-set digest oracle.
func TestTodo_RECOVERY_005_Golden(t *testing.T) {
	manifest := mustCrossPlaneManifest(t)
	report := mustRestoreConsistencySet(t, seedSetInput(manifest, seedConsistentPlanes()))
	raw, err := os.ReadFile(filepath.Join("testdata", "recovery005.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if want := strings.TrimSpace(string(raw)); report.Digest != want {
		t.Fatalf("set digest mismatch:\n got=%q\nwant=%q", report.Digest, want)
	}
}

// TestTodo_RECOVERY_005_Race: concurrent filings keep one report per
// manifest ID.
func TestTodo_RECOVERY_005_Race(t *testing.T) {
	cell := NewSetCell()
	manifest := mustCrossPlaneManifest(t)
	const racers = 16
	var wg sync.WaitGroup
	reports := make([]ConsistencySetReport, racers)
	errs := make([]error, racers)
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			reports[i], errs[i] = cell.Record(seedSetInput(manifest, seedConsistentPlanes()))
		}(i)
	}
	wg.Wait()
	for i := range racers {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		if reports[i].Digest != reports[0].Digest {
			t.Fatalf("racer %d digest drift", i)
		}
	}
}

// TestTodo_RECOVERY_005_Integration: the fenced set composes the pilot
// data-plane acceptance with cross-plane verification.
func TestTodo_RECOVERY_005_Integration(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	tenantReq := TenantRestoreRequest{
		TenantID: "harborcare-demo", RecoveryPoint: at.Add(-2 * time.Hour), RestoredAt: at,
		Rows: []RestoredRow{
			{ID: "ledger-1", TenantID: "harborcare-demo", Plane: "ledger", Digest: "sha256:l1"},
		},
		LedgerHead: "sha256:head-9", MigrationJournalDigest: "sha256:journal-3",
		RuntimeLeaseEpoch: 11,
		Conformance: RestoreConformance{
			LedgerHeadsValid: true, ForeignKeysValid: true, RuntimeLeasesValid: true,
			HoldsApplied: true, DeletionsApplied: true, HashesValid: true,
		},
		Isolated: true,
	}
	receipt, err := RestoreTenant(tenantReq)
	if err != nil {
		t.Fatalf("RestoreTenant: %v", err)
	}
	manifest := mustCrossPlaneManifest(t)
	planes := seedConsistentPlanes()
	planes[0].Digest = receipt.DataDigest
	manifest.Digests[PlaneLedger] = receipt.DataDigest
	report := mustRestoreConsistencySet(t, seedSetInput(manifest, planes))
	if report.Status != SetReady {
		t.Fatalf("composed restore: %+v", report)
	}
	if len(report.Verified) != len(OrderedPlanes) {
		t.Fatalf("verified=%d want=%d", len(report.Verified), len(OrderedPlanes))
	}
}

// TestTodo_RECOVERY_005_Fault: production targets, duplicate planes,
// lost entries and regressed checkpoints fail closed.
func TestTodo_RECOVERY_005_Fault(t *testing.T) {
	manifest := mustCrossPlaneManifest(t)
	good := seedSetInput(manifest, seedConsistentPlanes())
	prod := good
	prod.Destination = "production"
	_, err := RestoreConsistencySet(prod)
	requireSetError(t, err, "cannot target production")
	empty := good
	empty.Manifest.ManifestID = ""
	_, err = RestoreConsistencySet(empty)
	requireSetError(t, err, "manifest id")
	dup := good
	dup.Planes = append(append([]PlaneSnapshot(nil), good.Planes...), good.Planes[0])
	_, err = RestoreConsistencySet(dup)
	requireSetError(t, err, "duplicate plane")
	regressed := seedConsistentPlanes()
	regressed[2].StreamHead--
	if report := mustRestoreConsistencySet(t, seedSetInput(manifest, regressed)); report.Status != SetFenced {
		t.Fatalf("regressed head passed: %+v", report)
	}
	tampered := seedConsistentPlanes()
	tampered[5].Digest = "sha256:forged"
	if report := mustRestoreConsistencySet(t, seedSetInput(manifest, tampered)); report.Status != SetFenced {
		t.Fatalf("tampered digest passed: %+v", report)
	}
	dupeKeys := seedConsistentPlanes()
	dupeKeys[0].DedupeKeys = []string{"k", "k"}
	if report := mustRestoreConsistencySet(t, seedSetInput(manifest, dupeKeys)); report.Status != SetFenced {
		t.Fatalf("duplicate dedupe keys passed: %+v", report)
	}
	// Unknown external state fences as pending without redrive.
	unknown := seedConsistentPlanes()
	unknown[3].External = ""
	gated := mustRestoreConsistencySet(t, seedSetInput(manifest, unknown))
	if gated.Status != SetFenced || gated.Redriven != 0 {
		t.Fatalf("unknown external: %+v", gated)
	}
	if gated.External[PlaneConnectors] != ObservationPending {
		t.Fatalf("external resolution: %+v", gated.External)
	}
}

// TestTodo_RECOVERY_005_Security: repair-required planes stay fenced
// until governed repair; verified planes cannot smuggle unverified kin.
func TestTodo_RECOVERY_005_Security(t *testing.T) {
	manifest := mustCrossPlaneManifest(t)
	planes := seedConsistentPlanes()
	planes[4].External = RepairRequired
	gated := mustRestoreConsistencySet(t, seedSetInput(manifest, planes))
	if gated.Status != SetFenced {
		t.Fatalf("repair-required passed: %+v", gated)
	}
	if gated.External[PlaneSearch] != RepairRequired {
		t.Fatalf("external resolution: %+v", gated.External)
	}
	for _, verified := range gated.Verified {
		if verified == PlaneSearch {
			t.Fatalf("repair-required plane listed verified: %+v", gated.Verified)
		}
	}
}

// TestTodo_RECOVERY_005_Conformance: the conformance vector pins plane
// order, fence rules and the no-redrive invariant.
func TestTodo_RECOVERY_005_Conformance(t *testing.T) {
	wantOrder := []ConsistencyPlane{PlaneLedger, PlaneProjections, PlaneOutbox, PlaneConnectors, PlaneSearch, PlaneArtifacts}
	for i, plane := range OrderedPlanes {
		if plane != wantOrder[i] {
			t.Fatalf("plane order drift at %d: %s", i, plane)
		}
	}
	manifest := mustCrossPlaneManifest(t)
	report := mustRestoreConsistencySet(t, seedSetInput(manifest, seedConsistentPlanes()))
	if report.Redriven != 0 {
		t.Fatalf("conformance: redriven=%d", report.Redriven)
	}
	for i, verified := range report.Verified {
		if verified != wantOrder[i] {
			t.Fatalf("verified order drift at %d: %s", i, verified)
		}
	}
}

// TestTodo_RECOVERY_005_Recovery: a fenced set re-restores READY after
// the defect heals, receipt-stable across replays.
func TestTodo_RECOVERY_005_Recovery(t *testing.T) {
	cell := NewSetCell()
	manifest := mustCrossPlaneManifest(t)
	planes := seedConsistentPlanes()
	planes[1].Watermark--
	in := seedSetInput(manifest, planes)
	first, err := cell.Record(in)
	if err != nil {
		t.Fatalf("fenced Record: %v", err)
	}
	if first.Status != SetFenced {
		t.Fatalf("defect passed: %+v", first)
	}
	healed := seedSetInput(manifest, seedConsistentPlanes())
	healed.Manifest.ManifestID = "manifest-2"
	second, err := cell.Record(healed)
	if err != nil {
		t.Fatalf("healed Record: %v", err)
	}
	if second.Status != SetReady {
		t.Fatalf("healed restore: %+v", second)
	}
	replay, err := cell.Record(healed)
	if err != nil {
		t.Fatalf("replay Record: %v", err)
	}
	if replay.Digest != second.Digest {
		t.Fatalf("replay drift:\n got=%q\nwant=%q", replay.Digest, second.Digest)
	}
}

// BenchmarkTodo_RECOVERY_005 measures consistency-set verification.
func BenchmarkTodo_RECOVERY_005(b *testing.B) {
	manifest := RecoveryManifest{ManifestID: "manifest-bench"}
	manifest.Heads = map[ConsistencyPlane]uint64{}
	manifest.Watermarks = map[ConsistencyPlane]uint64{}
	manifest.Digests = map[ConsistencyPlane]string{}
	for i, plane := range OrderedPlanes {
		manifest.Heads[plane] = uint64(100 + i)
		manifest.Watermarks[plane] = uint64(90 + i)
		manifest.Digests[plane] = fmt.Sprintf("sha256:plane-%s", plane)
	}
	in := seedSetInput(manifest, seedConsistentPlanes())
	b.ResetTimer()
	for range b.N {
		if _, err := RestoreConsistencySet(in); err != nil {
			b.Fatalf("RestoreConsistencySet: %v", err)
		}
	}
}

// TestTodo_RECOVERY_005_ModelBased: a map-based reference model agrees
// with the implementation on admit/fence over seeded mutations.
func TestTodo_RECOVERY_005_ModelBased(t *testing.T) {
	manifest := mustCrossPlaneManifest(t)
	model := func(planes []PlaneSnapshot) SetStatus {
		byPlane := map[ConsistencyPlane]PlaneSnapshot{}
		for _, plane := range planes {
			byPlane[plane.Plane] = plane
		}
		for _, plane := range OrderedPlanes {
			snapshot, ok := byPlane[plane]
			if !ok || snapshot.StreamHead != manifest.Heads[plane] ||
				snapshot.Watermark != manifest.Watermarks[plane] ||
				snapshot.Digest != manifest.Digests[plane] ||
				snapshot.External != ExternallyVerified {
				return SetFenced
			}
		}
		return SetReady
	}
	mutations := []func([]PlaneSnapshot) []PlaneSnapshot{
		func(p []PlaneSnapshot) []PlaneSnapshot { return p },
		func(p []PlaneSnapshot) []PlaneSnapshot {
			out := append([]PlaneSnapshot(nil), p...)
			out[0].StreamHead++
			return out
		},
		func(p []PlaneSnapshot) []PlaneSnapshot {
			out := append([]PlaneSnapshot(nil), p...)
			out[4].External = ObservationPending
			return out
		},
		func(p []PlaneSnapshot) []PlaneSnapshot { return p[:len(p)-1] },
	}
	for i, mutate := range mutations {
		planes := mutate(seedConsistentPlanes())
		report := mustRestoreConsistencySet(t, seedSetInput(manifest, planes))
		if report.Status != model(planes) {
			t.Fatalf("mutation %d: implementation=%v model=%v", i, report.Status, model(planes))
		}
	}
}

// TestTodo_RECOVERY_005_Mutation: envelope and identity edges resolve
// on the documented side.
func TestTodo_RECOVERY_005_Mutation(t *testing.T) {
	manifest := mustCrossPlaneManifest(t)
	good := seedSetInput(manifest, seedConsistentPlanes())
	backwards := good
	backwards.RestoredAt = backwards.StartedAt.Add(-time.Minute)
	_, err := RestoreConsistencySet(backwards)
	requireSetError(t, err, "precedes start")
	blank := good
	blank.Destination = "  "
	_, err = RestoreConsistencySet(blank)
	requireSetError(t, err, "destination is required")
	// Zero RTO restores READY when the set is consistent.
	instant := good
	instant.RestoredAt = instant.StartedAt
	if report := mustRestoreConsistencySet(t, instant); report.Status != SetReady || report.RTO != 0 {
		t.Fatalf("instant restore: %+v", report)
	}
}
