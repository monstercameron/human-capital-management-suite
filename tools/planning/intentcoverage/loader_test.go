package intentcoverage

import (
	"os"
	"path/filepath"
	"testing"
)

// repoRootForTest returns the repository root relative to this package
// directory (tools/planning/intentcoverage), matching the pattern already
// used by tools/planning/intentmanifests/realcatalog_test.go.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..")
}

// TestLoadRepositoryRealCatalog loads the live accepted-intent catalog,
// feature-intent coverage registry, workflow catalogue and planning backlog
// (the same governance files GOV-024/INTENT-009/GOV-025 already validate)
// and proves LoadRepository resolves them into a non-empty, internally
// consistent Snapshot without mutating any of them.
func TestLoadRepositoryRealCatalog(t *testing.T) {
	root := repoRootForTest(t)
	snap, testNames, err := LoadRepository(root)
	if err != nil {
		t.Fatalf("load repository: %v", err)
	}
	if len(snap.Intents) != 14 {
		t.Fatalf("accepted intent count = %d, want 14", len(snap.Intents))
	}
	for _, in := range snap.Intents {
		if in.Namespace != NamespaceBaseline {
			t.Fatalf("intent %s namespace = %q, want baseline (no extension intents exist yet)", in.ID, in.Namespace)
		}
	}
	if len(snap.Gaps) == 0 {
		t.Fatal("expected at least one capability gap bound to an accepted intent")
	}
	knownIDs := map[string]bool{}
	for _, in := range snap.Intents {
		knownIDs[in.ID] = true
	}
	for _, g := range snap.Gaps {
		if !knownIDs[g.IntentID] {
			t.Fatalf("gap %s bound to %q, which LoadRepository should have filtered out", g.FeatureID, g.IntentID)
		}
	}
	if len(snap.Todos) == 0 {
		t.Fatal("expected at least one parsed todo binding")
	}
	if len(testNames) == 0 {
		t.Fatal("expected at least one scanned executable test name")
	}

	report := Reconcile(snap, Options{TestNames: testNames})
	if report.BaselineIntentCount != 14 {
		t.Fatalf("report baseline intent count = %d, want 14", report.BaselineIntentCount)
	}
	t.Logf("live intentcoverage: baseline=%d gaps=%d workflows=%d todos=%d orphans=%d new=%d",
		report.BaselineIntentCount, report.GapCount, report.WorkflowBindingCount, report.TodoBindingCount, len(report.Orphans), len(report.NewOrphans))
}

func TestLoadAllowlistMissingIsEmpty(t *testing.T) {
	entries, err := LoadAllowlist(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("missing allowlist = %v, %v", entries, err)
	}
}

func TestLoadAllowlistRejectsIncompleteEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.json")
	if err := WriteAllowlist(path, []AllowlistEntry{{Kind: "", ID: "X", Owner: "owner"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAllowlist(path); err == nil {
		t.Fatal("expected an error for an allowlist entry missing kind/id/owner")
	}
}

func TestWriteAllowlistCreatesNestedDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "allowlist.json")
	entries := []AllowlistEntry{{Kind: KindIntentCapability, ID: "X-1", Owner: "intake-governance", Reason: "pending", ReviewDate: "2026-09-05"}}
	if err := WriteAllowlist(path, entries); err != nil {
		t.Fatal(err)
	}
	roundTripped, err := LoadAllowlist(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(roundTripped) != 1 || roundTripped[0].ID != "X-1" {
		t.Fatalf("round-tripped allowlist = %+v", roundTripped)
	}
}

func TestDirectIntentsExcludesNoneAndBlanks(t *testing.T) {
	got := directIntents("none")
	if len(got) != 0 {
		t.Fatalf("directIntents(none) = %v, want empty", got)
	}
	got = directIntents("hcmnext.people.promote_worker/v1, none, hcmnext.work.approve_proposal/v1")
	if len(got) != 2 {
		t.Fatalf("directIntents = %v, want two entries", got)
	}
}

func TestSplitTrimIgnoresBlankTokens(t *testing.T) {
	got := splitTrim("BI.PEOPLE, , BI.REWARDS")
	if len(got) != 2 || got[0] != "BI.PEOPLE" || got[1] != "BI.REWARDS" {
		t.Fatalf("splitTrim = %v", got)
	}
}

// TestScanRepoTestNamesIncludesTheTestRoot proves a suite declared only
// under test/ is an executable oracle name, and that a build-cache directory
// at the bare repository root is still not walked.
func TestScanRepoTestNamesIncludesTheTestRoot(t *testing.T) {
	root := t.TempDir()
	for rel, name := range map[string]string{
		"test/workflow/flow_test.go":        "TestOnlyUnderTestRoot",
		"internal/sample/sample_test.go":    "TestUnderInternal",
		".gocache-lane/stray/stray_test.go": "TestInABuildCache",
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		src := "package sample\n\nimport \"testing\"\n\nfunc " + name + "(t *testing.T) {}\n"
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	names, err := scanRepoTestNames(root)
	if err != nil {
		t.Fatalf("scanRepoTestNames: %v", err)
	}
	if !names["TestOnlyUnderTestRoot"] || !names["TestUnderInternal"] || names["TestInABuildCache"] || len(names) != 2 {
		t.Fatalf("scanned names = %v, want exactly the test/ and internal/ suites", names)
	}
}
