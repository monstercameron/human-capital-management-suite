package closurewitness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingFileRegistriesAreUnsourcedNotEmpty(t *testing.T) {
	root := t.TempDir()
	var snap Snapshot
	for _, load := range []func(string, *Snapshot) error{loadCeiling, loadSlices, loadCoverage, loadWaivers} {
		if err := load(root, &snap); err != nil {
			t.Fatalf("loading an absent registry failed instead of marking it unsourced: %v", err)
		}
	}
	want := []EdgeClass{ClassPhaseGate, ClassSlice, ClassTodo, ClassEvidence}
	if len(snap.Unsourced) != len(want) {
		t.Fatalf("unsourced = %v, want %v", snap.Unsourced, want)
	}
	for i := range want {
		if snap.Unsourced[i] != want[i] {
			t.Errorf("unsourced[%d] = %s, want %s", i, snap.Unsourced[i], want[i])
		}
	}
}

func TestLoadSnapshotFailsWithoutTheSourceRegistry(t *testing.T) {
	if _, err := LoadSnapshot(t.TempDir(), fixtureAsOf); err == nil || !strings.Contains(err.Error(), "source descriptors") {
		t.Fatalf("LoadSnapshot over an empty root = %v, want a source-descriptor error", err)
	}
}

func TestLoadTestsSkipsAbsentRootsAndFindsPresentOnes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "test", "sample")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package sample\n\nimport \"testing\"\n\nfunc TestSampleExists(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(dir, "sample_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var snap Snapshot
	if err := loadTests(root, &snap); err != nil {
		t.Fatalf("loadTests: %v", err)
	}
	if !snap.TestExists["TestSampleExists"] || len(snap.TestExists) != 1 {
		t.Fatalf("TestExists = %v, want exactly TestSampleExists from test/", snap.TestExists)
	}
}

func TestParseCoverageKeepsIntentRowsAndDirectClaimsOnly(t *testing.T) {
	raw := []byte(`version: 1
items:
  - id: hcmnext.fixture.alpha
    kind: capability
    state: DEFINED
    claims: [FIX-001:DIRECT]
    tests: [TestCap]
  - id: hcmnext.fixture.alpha
    kind: intent
    state: PARTIAL
    claims: [FIX-001:DIRECT, FIX-002:DOMAIN, malformed]
    tests: [TestAlpha]
`)
	var snap Snapshot
	if err := parseCoverage(raw, &snap); err != nil {
		t.Fatalf("parseCoverage: %v", err)
	}
	if len(snap.Coverage) != 1 {
		t.Fatalf("coverage rows = %+v, want the one intent row", snap.Coverage)
	}
	row := snap.Coverage[0]
	if row.Intent != "hcmnext.fixture.alpha" || row.State != "PARTIAL" || len(row.DirectTodos) != 1 || row.DirectTodos[0] != "FIX-001" || len(row.Tests) != 1 {
		t.Fatalf("coverage row = %+v", row)
	}
	if err := parseCoverage([]byte("items: [unterminated"), &snap); err == nil {
		t.Fatal("malformed coverage YAML parsed")
	}
}

func TestPresentDistinguishesMissingFromExisting(t *testing.T) {
	root := t.TempDir()
	if ok, err := present(filepath.Join(root, "nope")); ok || err != nil {
		t.Fatalf("present(missing) = %v, %v", ok, err)
	}
	if ok, err := present(root); !ok || err != nil {
		t.Fatalf("present(existing) = %v, %v", ok, err)
	}
}
