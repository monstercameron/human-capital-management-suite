package traceability

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

// TestRequirementTraceabilityRejectsOrphans is the primary red/green test
// for GOV-003.
func TestRequirementTraceabilityRejectsOrphans(t *testing.T) {
	t.Run("extract plain backtick name", func(t *testing.T) {
		got := ExtractEvidenceTestNames("`TestFoo` in `tools/planning`")
		assertNames(t, got, []string{"TestFoo"})
	})

	t.Run("extract a wildcard matrix name", func(t *testing.T) {
		got := ExtractEvidenceTestNames("`TestTodo_FEATURE_CONF_001_*` matrix in `tools/planning/intentmanifests`")
		assertNames(t, got, []string{"TestTodo_FEATURE_CONF_001_*"})
	})

	t.Run("extract a plain TypeScript TEST label", func(t *testing.T) {
		got := ExtractEvidenceTestNames("TypeScript proof: TEST TestBrowserArtifactPolicyCacheAndStorageLifecycleRejectsStaleInjectedOrSensitiveState in src/platform/client-lifecycle; `npx vitest run` PASS")
		assertNames(t, got, []string{"TestBrowserArtifactPolicyCacheAndStorageLifecycleRejectsStaleInjectedOrSensitiveState"})
	})

	t.Run("extract brace expansion group", func(t *testing.T) {
		got := ExtractEvidenceTestNames("`TestTodo_ID_{Golden,Race}` in `internal/x`")
		assertNames(t, got, []string{"TestTodo_ID_Golden", "TestTodo_ID_Race"})
	})

	t.Run("extract shorthand suffix continuation", func(t *testing.T) {
		got := ExtractEvidenceTestNames("`TestTodo_ID`, `_Golden`, `_Race`, `FuzzTodo_ID` in `internal/x`")
		assertNames(t, got, []string{"TestTodo_ID", "TestTodo_ID_Golden", "TestTodo_ID_Race", "FuzzTodo_ID"})
	})

	t.Run("first matrix variant establishes the todo root", func(t *testing.T) {
		got := ExtractEvidenceTestNames("`TestP1BAuthority`, `TestTodo_NEXT_006_Property`, `_Golden`, `_Security`")
		assertNames(t, got, []string{"TestP1BAuthority", "TestTodo_NEXT_006_Property", "TestTodo_NEXT_006_Golden", "TestTodo_NEXT_006_Security"})
	})

	t.Run("shorthand suffix rebases on the root even after a compound plain name", func(t *testing.T) {
		// Real corpus shape: `TestTodo_DATA_005`, `TestTodo_DATA_005_Property`,
		// `_Property_MonotonicKnowledge`, `_Security` - the last two must
		// attach to the root "TestTodo_DATA_005", not to the
		// already-suffixed "TestTodo_DATA_005_Property".
		got := ExtractEvidenceTestNames("`TestTodo_DATA_005` (11 subtests), `TestTodo_DATA_005_Property`, `_Property_MonotonicKnowledge`, `_Security` (proofs), `_Mutation` in `internal/data/bitemporal`")
		assertNames(t, got, []string{
			"TestTodo_DATA_005",
			"TestTodo_DATA_005_Property",
			"TestTodo_DATA_005_Property_MonotonicKnowledge",
			"TestTodo_DATA_005_Security",
			"TestTodo_DATA_005_Mutation",
		})
	})

	t.Run("shorthand suffix rebases on the Test root even after an intervening Fuzz name and a fully-spelled Test variant", func(t *testing.T) {
		// Real corpus shape: `TestTodo_INTG_009`, `_Golden`, `_Mutation`,
		// `_Fault`, `FuzzTodo_INTG_009` ... `TestTodo_INTG_009_Integration`,
		// `_Fault_Integration` - the trailing suffix must attach to the
		// original root "TestTodo_INTG_009", not to "FuzzTodo_INTG_009"
		// nor to the fully-spelled "TestTodo_INTG_009_Integration".
		got := ExtractEvidenceTestNames("`TestTodo_INTG_009`, `_Golden`, `_Mutation`, `_Fault`, `FuzzTodo_INTG_009` in `pkg`; `TestTodo_INTG_009_Integration`, `_Fault_Integration` in `pkg2`")
		assertNames(t, got, []string{
			"TestTodo_INTG_009",
			"TestTodo_INTG_009_Golden",
			"TestTodo_INTG_009_Mutation",
			"TestTodo_INTG_009_Fault",
			"FuzzTodo_INTG_009",
			"TestTodo_INTG_009_Integration",
			"TestTodo_INTG_009_Fault_Integration",
		})
	})

	t.Run("a backticked file-path brace list is not treated as test-name expansion", func(t *testing.T) {
		got := ExtractEvidenceTestNames("seeded from `planning/research/state-employment-law/{california,new-york}.md`; also `definitions/telemetry/{metrics-catalog,dashboards,alerts}.yaml`")
		if len(got) != 0 {
			t.Fatalf("expected zero names from non-test brace-path tokens, got %v", got)
		}
	})

	t.Run("comma separated names inside one backtick pair", func(t *testing.T) {
		got := ExtractEvidenceTestNames("`TestFoo, FuzzFoo` in `tools/x`")
		assertNames(t, got, []string{"TestFoo", "FuzzFoo"})
	})

	t.Run("non-test backtick tokens are ignored", func(t *testing.T) {
		got := ExtractEvidenceTestNames("`TestFoo` PASS on `windows/arm64`; `go test -count=1 ./...`")
		assertNames(t, got, []string{"TestFoo"})
	})

	t.Run("extract literal test name from a backticked go test run command", func(t *testing.T) {
		got := ExtractEvidenceTestNames("`go test -count=1 -run '^TestTodo_WF_UI_001($|_)' ./graphcanvas/`")
		assertNames(t, got, []string{"TestTodo_WF_UI_001"})
	})

	t.Run("extract literal test name from equals form and keep only identifiers", func(t *testing.T) {
		got := ExtractEvidenceTestNames("`go test -run=\"^TestTodo_WF_UI_001$\" ./graphcanvas/`; `go test -run 'TestTodo_(FOO|BAR)' ./x`; `go test -run 'TestTodo_' ./y`")
		assertNames(t, got, []string{"TestTodo_WF_UI_001"})
	})

	t.Run("run command names still must resolve to a repository test", func(t *testing.T) {
		todos := []todoregistry.Todo{{ID: "X-001", Done: true, Evidence: "`go test -run '^TestTodo_MISSING$' ./pkg`"}}
		orphans := CheckTraceability(todos, map[string]bool{"TestTodo_PRESENT": true})
		if len(orphans) != 1 || orphans[0].ID != "X-001" {
			t.Fatalf("expected nonexistent command test to remain orphaned, got %v", orphans)
		}
	})

	t.Run("ScanTestNames finds real repo test names", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "fixture_test.go"), mustRead(t, filepath.Join("testdata", "fixture_test.go.txt")), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		names, err := ScanTestNames(dir)
		if err != nil {
			t.Fatalf("ScanTestNames: %v", err)
		}
		for _, want := range []string{"TestRealExample", "TestRealExample_Golden", "FuzzRealExample"} {
			if !names[want] {
				t.Errorf("expected ScanTestNames to find %s, got %v", want, names)
			}
		}
	})

	t.Run("done todo citing a real test has zero orphans", func(t *testing.T) {
		todos := []todoregistry.Todo{{ID: "X-001", Done: true, Evidence: "`TestReal` in `tools/x`"}}
		existing := map[string]bool{"TestReal": true}
		if orphans := CheckTraceability(todos, existing); len(orphans) != 0 {
			t.Errorf("expected zero orphans, got %v", orphans)
		}
	})

	t.Run("a wildcard evidence matrix resolves against existing tests", func(t *testing.T) {
		todos := []todoregistry.Todo{{ID: "X-001W", Done: true, Evidence: "`TestTodo_FEATURE_CONF_001_*` matrix in `tools/x`"}}
		existing := map[string]bool{"TestTodo_FEATURE_CONF_001_Golden": true, "TestTodo_FEATURE_CONF_001_Race": true}
		if orphans := CheckTraceability(todos, existing); len(orphans) != 0 {
			t.Errorf("expected wildcard evidence to resolve to its concrete matrix functions, got %v", orphans)
		}
	})

	t.Run("a retired todo with a recorded disposition needs no test evidence", func(t *testing.T) {
		todos := []todoregistry.Todo{{
			ID: "X-001R", Done: true, Retired: true,
			Disposition: "RETIRED: capability withdrawn by the approved plan",
			Evidence:    "closed under the recorded retirement disposition",
		}}
		if orphans := CheckTraceability(todos, map[string]bool{}); len(orphans) != 0 {
			t.Errorf("expected explicit retirement disposition to satisfy applicability, got %v", orphans)
		}
	})

	t.Run("one real top-level test resolves shorthand subtest labels", func(t *testing.T) {
		todos := []todoregistry.Todo{{ID: "X-001A", Done: true, Evidence: "`TestReal`, `_Golden`, `_Race` in `tools/x`"}}
		existing := map[string]bool{"TestReal": true}
		if orphans := CheckTraceability(todos, existing); len(orphans) != 0 {
			t.Errorf("expected the real top-level test to resolve the evidence, got %v", orphans)
		}
	})

	t.Run("done todo citing a nonexistent test is an orphan", func(t *testing.T) {
		todos := []todoregistry.Todo{{ID: "X-002", Done: true, Evidence: "`TestDoesNotExist` in `tools/x`"}}
		existing := map[string]bool{"TestReal": true}
		orphans := CheckTraceability(todos, existing)
		if len(orphans) != 1 || orphans[0].ID != "X-002" {
			t.Fatalf("expected one orphan for X-002, got %v", orphans)
		}
	})

	t.Run("done todo with no evidence field is an orphan", func(t *testing.T) {
		todos := []todoregistry.Todo{{ID: "X-003", Done: true}}
		orphans := CheckTraceability(todos, map[string]bool{})
		if len(orphans) != 1 || orphans[0].ID != "X-003" {
			t.Fatalf("expected one orphan for X-003, got %v", orphans)
		}
	})

	t.Run("done todo with evidence naming no test is an orphan", func(t *testing.T) {
		todos := []todoregistry.Todo{{ID: "X-004", Done: true, Evidence: "reviewed by hand, looks fine"}}
		orphans := CheckTraceability(todos, map[string]bool{})
		if len(orphans) != 1 || orphans[0].ID != "X-004" {
			t.Fatalf("expected one orphan for X-004, got %v", orphans)
		}
	})

	t.Run("not-done todo citing a future test is never an orphan", func(t *testing.T) {
		todos := []todoregistry.Todo{{ID: "X-005", Done: false, Evidence: "Remaining: `TestFutureWork` end-to-end"}}
		if orphans := CheckTraceability(todos, map[string]bool{}); len(orphans) != 0 {
			t.Errorf("expected zero orphans for a not-done todo, got %v", orphans)
		}
	})

	t.Run("real planning corpus has zero unresolved evidence orphans", func(t *testing.T) {
		content, err := os.ReadFile(filepath.Join("..", "..", "..", "planning", "todos.md"))
		if err != nil {
			t.Fatalf("read todos.md: %v", err)
		}
		todos, parseErrs := todoregistry.ParseTodos(string(content))
		if len(parseErrs) > 0 {
			t.Fatalf("ParseTodos returned errors: %v", parseErrs)
		}

		repoRoot := filepath.Join("..", "..", "..")
		existing, err := ScanTestNames(repoRoot)
		if err != nil {
			t.Fatalf("ScanTestNames: %v", err)
		}

		orphans := CheckTraceability(todos, existing)
		var unexpected []Orphan
		for _, o := range orphans {
			if _, ok := tsProvenTodos[o.ID]; !ok {
				unexpected = append(unexpected, o)
			}
		}
		if len(unexpected) != 0 {
			t.Errorf("found %d unresolved evidence orphans in the real planning corpus:", len(unexpected))
			for _, o := range unexpected {
				t.Errorf("  %s", o)
			}
		}
	})
}

// TestTodo_GOV_003_Golden pins the exact orphan message format for a fixed
// fixture so a future refactor cannot silently change diagnostic wording.
func TestTodo_GOV_003_Golden(t *testing.T) {
	todos := []todoregistry.Todo{
		{ID: "GOLDEN-001", Done: true, Evidence: "`TestGoldenMissing` in `tools/x`"},
	}
	orphans := CheckTraceability(todos, map[string]bool{})
	if len(orphans) != 1 {
		t.Fatalf("expected exactly one orphan, got %d: %v", len(orphans), orphans)
	}
	const want = "GOLDEN-001: Evidence names no repository test; first unresolved name is TestGoldenMissing"
	if got := orphans[0].String(); got != want {
		t.Errorf("orphan message changed:\n got:  %s\n want: %s", got, want)
	}
}

// TestTodo_GOV_003_Race exercises ScanTestNames and CheckTraceability
// concurrently to prove the crosswalk has no shared-state races; run with
// `go test -race`.
func TestTodo_GOV_003_Race(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fixture_test.go"), mustRead(t, filepath.Join("testdata", "fixture_test.go.txt")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	todos := []todoregistry.Todo{
		{ID: "R-001", Done: true, Evidence: "`TestRealExample` in `x`"},
		{ID: "R-002", Done: true, Evidence: "`TestMissing` in `x`"},
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			names, err := ScanTestNames(dir)
			if err != nil {
				t.Errorf("ScanTestNames: %v", err)
				return
			}
			_ = CheckTraceability(todos, names)
		}()
	}
	wg.Wait()
}

func assertNames(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}
