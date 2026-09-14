package designclosure

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/traceability"
)

func writeTestSource(t *testing.T, root, rel, name string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package sample\n\nimport \"testing\"\n\nfunc " + name + "(t *testing.T) {}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestScanTestNamesIncludesTheTestRoot proves a suite declared only under
// test/ counts as an existing test, and that the bare repository root is
// still not walked.
func TestScanTestNamesIncludesTheTestRoot(t *testing.T) {
	root := t.TempDir()
	writeTestSource(t, root, "test/bootstrap/cell_test.go", "TestOnlyUnderTestRoot")
	writeTestSource(t, root, "tools/planning/x/x_test.go", "TestUnderTools")
	writeTestSource(t, root, ".gocache-lane/stray/stray_test.go", "TestInABuildCache")

	names, err := scanTestNames(root)
	if err != nil {
		t.Fatalf("scanTestNames: %v", err)
	}
	want := map[string]bool{"TestOnlyUnderTestRoot": true, "TestUnderTools": true}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("scanned names = %v, want %v", names, want)
	}
}

func TestScanTestNamesToleratesAbsentRoots(t *testing.T) {
	names, err := scanTestNames(t.TempDir())
	if err != nil || len(names) != 0 {
		t.Fatalf("empty root = %v, %v; want no names and no error", names, err)
	}
}

// TestLoadSnapshotSeesEveryLiveTestRootSuite proves the live loader no
// longer reports test/ suites as dangling: every name declared under test/
// exists in the snapshot, including names declared nowhere else.
func TestLoadSnapshotSeesEveryLiveTestRootSuite(t *testing.T) {
	root := repoRoot(t)
	snap, _, err := LoadSnapshot(root)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	underTest, err := traceability.ScanTestNames(filepath.Join(root, "test"))
	if err != nil {
		t.Fatal(err)
	}
	onlyUnderTest := 0
	elsewhere := map[string]bool{}
	for _, sub := range []string{"cmd", "gen", "internal", "tools"} {
		dir := filepath.Join(root, sub)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		names, err := traceability.ScanTestNames(dir)
		if err != nil {
			t.Fatal(err)
		}
		for name := range names {
			elsewhere[name] = true
		}
	}
	for name := range underTest {
		if !snap.TestExists[name] {
			t.Errorf("test/ suite %s is missing from TestExists", name)
		}
		if !elsewhere[name] {
			onlyUnderTest++
		}
	}
	if onlyUnderTest == 0 {
		t.Fatal("fixture precondition: no live test name is declared only under test/, so this test would be vacuous")
	}
}

func TestDirectIntentsDropsNoneAndBlanksSorted(t *testing.T) {
	got := directIntents(" b/v1, none, ,a/v1")
	if want := []string{"a/v1", "b/v1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("directIntents = %v, want %v", got, want)
	}
}
