package releaseevidence_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/releaseevidence"
	"github.com/monstercameron/human-capital-management-suite/migrations"
	"github.com/pressly/goose/v3"
)

// scriptStore is a VersionStore scripted with the exact versions each
// ApplyNext reports. errAt fails one call; it honors context cancellation.
type scriptStore struct {
	current int64
	script  []int64
	errAt   int
	calls   int
}

func fileVersions(t *testing.T) []int64 {
	t.Helper()
	files, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	versions := make([]int64, 0, len(files))
	for _, f := range files {
		versions = append(versions, f.Version)
	}
	return versions
}

func fullScript(t *testing.T) *scriptStore {
	t.Helper()
	return &scriptStore{script: fileVersions(t), errAt: -1}
}

func (s *scriptStore) CurrentVersion(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.current, nil
}

func (s *scriptStore) ApplyNext(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if s.errAt == s.calls {
		s.calls++
		return 0, errors.New("apply refused by fixture")
	}
	version := s.script[s.calls]
	s.calls++
	s.current = version
	return version, nil
}

// TestTodo_ALIGN_060 proves the product-slice PostgreSQL chronology: every
// embedded migration applies in order from an empty schema, each step records
// its versioned artifact, and the migrated tree matches the release before
// the harness reports clean.
func TestTodo_ALIGN_060(t *testing.T) {
	release := sealed(t)
	report, err := releaseevidence.RunChronology(context.Background(), fullScript(t), release, matchingRuntime(t, release))
	if err != nil {
		t.Fatalf("chronology = %v", err)
	}
	files, _ := migrations.Files()
	digest, _ := migrations.ArtifactDigest()
	if len(report.Steps) != len(files) || report.AppliedVersion != report.TargetVersion {
		t.Fatalf("steps=%d files=%d applied=%d target=%d", len(report.Steps), len(files), report.AppliedVersion, report.TargetVersion)
	}
	for i, f := range files {
		step := report.Steps[i]
		if step.Version != f.Version || step.Name != f.Name || step.Checksum != f.Checksum {
			t.Fatalf("step %d = %+v, file = %+v", i, step, f)
		}
	}
	if report.ArtifactDigest != digest || report.ReleaseDigest != release.Digest {
		t.Fatal("report is not bound to the migrated tree and release")
	}
	if err := report.Verify(); err != nil {
		t.Fatalf("report verify = %v", err)
	}
	// A tree that skips a migration is not a chronology.
	skipped := fileVersions(t)
	gapped := append(append([]int64(nil), skipped[:10]...), skipped[11:]...)
	store := &scriptStore{script: gapped, errAt: -1}
	if _, err := releaseevidence.RunChronology(context.Background(), store, release, matchingRuntime(t, release)); err == nil {
		t.Fatal("a gapped migration history reported clean")
	}
}

// TestTodo_ALIGN_060_Property proves every failure point is named: failing
// the apply at any step reports that migration's version with the steps
// before it retained, and any non-empty start is refused.
func TestTodo_ALIGN_060_Property(t *testing.T) {
	release := sealed(t)
	versions := fileVersions(t)
	for _, at := range []int{0, 1, len(versions) / 2, len(versions) - 1} {
		store := &scriptStore{script: versions, errAt: at}
		report, err := releaseevidence.RunChronology(context.Background(), store, release, matchingRuntime(t, release))
		if err == nil || !strings.Contains(err.Error(), fmt.Sprint(versions[at])) {
			t.Errorf("failure at step %d = %+v, %v", at, report, err)
		}
		if len(report.Steps) != at {
			t.Errorf("failure at step %d retained %d steps", at, len(report.Steps))
		}
	}
	for _, start := range []int64{1, versions[0], versions[len(versions)-1]} {
		store := &scriptStore{current: start, script: versions, errAt: -1}
		if _, err := releaseevidence.RunChronology(context.Background(), store, release, matchingRuntime(t, release)); err == nil {
			t.Errorf("chronology from version %d reported clean", start)
		}
	}
}

// TestTodo_ALIGN_060_Golden pins the report encoding and its safe summary.
func TestTodo_ALIGN_060_Golden(t *testing.T) {
	release := sealed(t)
	report, err := releaseevidence.RunChronology(context.Background(), fullScript(t), release, matchingRuntime(t, release))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(report.Steps[0])
	files, _ := migrations.Files()
	want := fmt.Sprintf(`{"version":%d,"name":%q,"checksum":%q}`, files[0].Version, files[0].Name, files[0].Checksum)
	if string(body) != want {
		t.Fatalf("first step = %s", body)
	}
	wantSummary := fmt.Sprintf("chronology promotion %s steps=%d applied=%d target=%d artifact=%s release=%s",
		release.Version, len(files), report.TargetVersion, report.TargetVersion, report.ArtifactDigest, report.ReleaseDigest)
	if report.Summary() != wantSummary {
		t.Fatalf("summary = %q", report.Summary())
	}
}

// TestTodo_ALIGN_060_Security proves a tampered release, a tree that changed
// under the release, or a release aimed at another schema can never report a
// clean chronology, and the harness touches no database for them.
func TestTodo_ALIGN_060_Security(t *testing.T) {
	release := sealed(t)
	forged := release
	forged.BinaryRevision = "rev-attacker"
	counting := fullScript(t)
	if _, err := releaseevidence.RunChronology(context.Background(), counting, forged, matchingRuntime(t, forged)); err == nil {
		t.Fatal("a forged release reported a clean chronology")
	}
	if counting.calls != 0 {
		t.Fatalf("forged release drove %d database calls", counting.calls)
	}
	drifted := release
	drifted.SchemaDigest = "sha256:another-tree"
	if _, err := releaseevidence.RunChronology(context.Background(), fullScript(t), drifted, matchingRuntime(t, release)); err == nil {
		t.Fatal("a release bound to another tree reported clean")
	}
	retargeted := release
	retargeted.SchemaVersion--
	if _, err := releaseevidence.RunChronology(context.Background(), fullScript(t), retargeted, matchingRuntime(t, release)); err == nil {
		t.Fatal("a release aimed at another schema reported clean")
	}
}

// gooseStore adapts a real Goose provider to the owned chronology boundary.
type gooseStore struct {
	t *testing.T
	p *goose.Provider
}

func (s gooseStore) CurrentVersion(ctx context.Context) (int64, error) {
	return s.p.GetDBVersion(ctx)
}

func (s gooseStore) ApplyNext(ctx context.Context) (int64, error) {
	if _, err := s.p.UpByOne(ctx); err != nil {
		return 0, err
	}
	return s.p.GetDBVersion(ctx)
}

// TestTodo_ALIGN_060_Integration replays the whole embedded migration
// chronology against a real empty PostgreSQL schema and proves the release
// matches the migrated tree step by step.
func TestTodo_ALIGN_060_Integration(t *testing.T) {
	release := sealed(t)
	db := pgtest.NewEmpty(t)
	report, err := releaseevidence.RunChronology(context.Background(), gooseStore{t, db.Provider(t)}, release, matchingRuntime(t, release))
	if err != nil {
		t.Fatalf("postgresql chronology = %v", err)
	}
	files, _ := migrations.Files()
	if len(report.Steps) != len(files) || report.AppliedVersion != files[len(files)-1].Version {
		t.Fatalf("steps=%d files=%d applied=%d", len(report.Steps), len(files), report.AppliedVersion)
	}
	applied, err := db.Provider(t).GetDBVersion(context.Background())
	if err != nil || applied != report.AppliedVersion {
		t.Fatalf("database version %d != reported %d (%v)", applied, report.AppliedVersion, err)
	}
}

// TestTodo_ALIGN_060_Fault proves mid-run failures, misreported versions,
// cancellation and a drifted runtime fail closed with the blocking step
// named.
func TestTodo_ALIGN_060_Fault(t *testing.T) {
	release := sealed(t)
	versions := fileVersions(t)
	misreported := append([]int64(nil), versions...)
	misreported[7] = misreported[6]
	if _, err := releaseevidence.RunChronology(context.Background(), &scriptStore{script: misreported, errAt: -1}, release, matchingRuntime(t, release)); err == nil {
		t.Fatal("a misreported migration version reported clean")
	} else if !strings.Contains(err.Error(), fmt.Sprint(versions[7])) {
		t.Fatalf("gap error does not name the expected version: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := releaseevidence.RunChronology(ctx, fullScript(t), release, matchingRuntime(t, release)); err == nil {
		t.Fatal("a canceled chronology reported clean")
	}
	if _, err := releaseevidence.RunChronology(context.Background(), nil, release, matchingRuntime(t, release)); err == nil {
		t.Fatal("a nil version store reported clean")
	}
	drifted := matchingRuntime(t, release)
	drifted.BinaryRevision = "rev-unreleased"
	if _, err := releaseevidence.RunChronology(context.Background(), fullScript(t), release, drifted); err == nil {
		t.Fatal("a runtime that drifted from its release reported clean")
	}
}

// TestTodo_ALIGN_060_Conformance proves reports are deterministic and
// self-verifying: repeated runs are byte-identical and any tampering with
// the steps or digests fails verification.
func TestTodo_ALIGN_060_Conformance(t *testing.T) {
	release := sealed(t)
	first, err := releaseevidence.RunChronology(context.Background(), fullScript(t), release, matchingRuntime(t, release))
	if err != nil {
		t.Fatal(err)
	}
	second, err := releaseevidence.RunChronology(context.Background(), fullScript(t), release, matchingRuntime(t, release))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatal("repeated chronologies differ")
	}
	mutants := []func(*releaseevidence.ChronologyReport){
		func(r *releaseevidence.ChronologyReport) { r.Steps = r.Steps[:len(r.Steps)-1] },
		func(r *releaseevidence.ChronologyReport) { r.Steps[0].Checksum = "tampered" },
		func(r *releaseevidence.ChronologyReport) { r.ArtifactDigest = "sha256:other" },
		func(r *releaseevidence.ChronologyReport) { r.AppliedVersion-- },
	}
	for i, mutate := range mutants {
		clone := first
		mutate(&clone)
		if err := clone.Verify(); err == nil {
			t.Errorf("mutant %d verified", i)
		}
	}
}
