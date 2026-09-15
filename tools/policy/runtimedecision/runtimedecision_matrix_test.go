package runtimedecision_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/runtimedecision"
)

// TestTodo_WF_RUN_000_Golden proves the finding list Validate produces for a
// fixed broken input is byte-stable: the same missing fields, in the same
// order, every run, so a CI diff on this check's output is meaningful
// rather than order-flaky.
func TestTodo_WF_RUN_000_Golden(t *testing.T) {
	// Strip the signature, the selected candidate's evidence and one
	// non-negotiable's decided result at once to pin down an exact,
	// multi-finding output.
	broken := strings.NewReplacer(
		`  signed_by: "someone (owner)"
`, "",
		`    evidence:
      - description: "evidence file"
        path: "EVIDENCE_FILE.md"
        status: EXISTS
`, "",
		"result: FAIL\n", "result: UNKNOWN\n",
	).Replace(minimalCompleteYAML)

	root, path := writeRepoWithRecord(t, broken)

	want := []string{
		`candidate "Temporal": evaluation[NN3].result "UNKNOWN" is not a decision: every non-negotiable must be PASS, FAIL or PARTIAL (UNKNOWN and PENDING are refused)`,
		`selected candidate "In-house" has no evidence entries`,
		"selected_option.signed_by is required (an unsigned choice is not a decision)",
	}

	for i := 0; i < 5; i++ {
		res, err := runtimedecision.ValidateFile(path, root)
		if err != nil {
			t.Fatalf("run %d: ValidateFile: %v", i, err)
		}
		if res.OK {
			t.Fatalf("run %d: expected the broken fixture to fail validation", i)
		}
		if !reflect.DeepEqual(res.Findings, want) {
			t.Fatalf("run %d: findings mismatch:\n got:  %#v\n want: %#v", i, res.Findings, want)
		}
	}
}

// p1bRuntimeDirs are the durable-runtime scheduler, lease, timer and
// recovery directories the WF-RUN-000 gate governs: the ones that have
// landed (internal/workflow/lease, internal/workflow/timer,
// internal/workflow/recover, internal/platform/execution/scheduler) and the
// names the go-only implementation shape reserves for future ones
// (internal/workflow/scheduler, leases, timers). Any of them that exists must
// be named in a PRE_CODE or RETROACTIVE reevaluation of the decision record.
var p1bRuntimeDirs = []string{
	"internal/workflow/lease",
	"internal/workflow/timer",
	"internal/workflow/recover",
	"internal/platform/execution/scheduler",
	"internal/workflow/scheduler",
	"internal/workflow/leases",
	"internal/workflow/timers",
}

// TestTodo_WF_RUN_000_Integration exercises the checker against the real
// repository tree: the decision record must validate clean against actual
// on-disk evidence and fixture test declarations, and every P1B
// scheduler/lease/timer/recovery directory that exists must be covered by a
// re-evaluation that names it, so gated code that lands without a recorded
// re-evaluation turns this test red.
func TestTodo_WF_RUN_000_Integration(t *testing.T) {
	root := repopath.RootDir()
	path := recordPath(root)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("decision record must exist at %s: %v", path, err)
	}

	res, err := runtimedecision.ValidateFile(path, root)
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if !res.OK {
		t.Fatalf("decision record is incomplete/unevidenced against the real repository tree: %v", res.Findings)
	}

	d, err := runtimedecision.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	covered := map[string]string{}
	for _, r := range d.Reevaluations {
		if r.Kind == runtimedecision.ReevaluationInitial {
			continue
		}
		for _, p := range r.CodeLanded {
			covered[p] = r.Date + " " + r.Kind
		}
	}
	landed := 0
	for _, dir := range p1bRuntimeDirs {
		info, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(dir)))
		if statErr != nil || !info.IsDir() {
			continue
		}
		landed++
		by, ok := covered[dir]
		if !ok {
			t.Errorf("P1B runtime directory %s exists but no PRE_CODE or RETROACTIVE reevaluation in the decision record names it", dir)
			continue
		}
		t.Logf("P1B runtime directory %s is covered by the %s reevaluation", dir, by)
	}
	if landed == 0 {
		t.Fatalf("expected the landed P1B runtime directories to exist; the gate list is stale")
	}
}

// TestTodo_WF_RUN_000_Conformance proves the decision record's four
// non-negotiables are the exact four criteria named in
// planning/specs/workflow-runtime.md's "Build or adopt" section, and runs the
// structural half of the in-house NN1 fixture: no runtime-state package and no
// runtime-state migration reaches the business ledger, so the ledger stays
// outside the engine's own history store by construction (the only ledger
// writer on the workflow path is the effects terminal adapter behind the
// execute.TerminalWriter port).
func TestTodo_WF_RUN_000_Conformance(t *testing.T) {
	root := repopath.RootDir()

	specPath := filepath.Join(root, "planning", "specs", "workflow-runtime.md")
	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("reading %s: %v", specPath, err)
	}
	wantNN, err := extractNonNegotiables(string(specBytes))
	if err != nil {
		t.Fatalf("extracting non-negotiables from spec: %v", err)
	}
	if len(wantNN) != 4 {
		t.Fatalf("expected exactly 4 non-negotiables in the spec, found %d: %v", len(wantNN), wantNN)
	}

	d, err := runtimedecision.Load(recordPath(root))
	if err != nil {
		t.Fatalf("loading decision record: %v", err)
	}
	if len(d.NonNegotiables) != 4 {
		t.Fatalf("decision record has %d non_negotiables, want 4", len(d.NonNegotiables))
	}
	for i, nn := range d.NonNegotiables {
		got := normalizeWhitespace(nn.Description)
		want := normalizeWhitespace(wantNN[i])
		if got != want {
			t.Fatalf("non_negotiables[%d] (%s) = %q, want the spec's exact wording %q", i, nn.ID, got, want)
		}
	}

	t.Run("NN1_runtime_state_packages_do_not_import_the_ledger", func(t *testing.T) {
		for _, pkg := range runtimeStatePackages {
			if bad := ledgerImports(t, filepath.Join(root, filepath.FromSlash(pkg))); len(bad) != 0 {
				t.Errorf("runtime-state package %s imports the business ledger: %v", pkg, bad)
			}
		}
	})

	t.Run("NN1_runtime_state_migrations_do_not_reference_the_ledger", func(t *testing.T) {
		for _, m := range runtimeStateMigrations {
			src, err := os.ReadFile(filepath.Join(root, "migrations", m))
			if err != nil {
				t.Fatalf("reading migration %s: %v", m, err)
			}
			if strings.Contains(strings.ToLower(string(src)), "ledger_event") {
				t.Errorf("runtime-state migration %s references ledger_event", m)
			}
		}
	})

	t.Run("NN1_scanner_detects_a_planted_ledger_import", func(t *testing.T) {
		dir := t.TempDir()
		src := "package p\n\nimport _ \"" + modulePath + "/internal/data/ledger\"\n"
		if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "p_test.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		if bad := ledgerImports(t, dir); len(bad) != 1 {
			t.Fatalf("scanner must flag exactly the planted non-test ledger import, got %v", bad)
		}
	})
}

const modulePath = "github.com/monstercameron/human-capital-management-suite"

// runtimeStatePackages hold the engine's own execution state: instances,
// nodes, receipts, leases, timers, recovery, scheduler roles and inspection.
var runtimeStatePackages = []string{
	"internal/workflow/runtime",
	"internal/workflow/lease",
	"internal/workflow/timer",
	"internal/workflow/recover",
	"internal/workflow/inspect",
	"internal/workflow/execute",
	"internal/data/runtimestate",
	"internal/platform/execution/scheduler",
}

// runtimeStateMigrations create the workflow runtime and scheduling tables.
var runtimeStateMigrations = []string{
	"00016_workflow_runtime.sql",
	"00026_workflow_scheduling_state.sql",
}

// ledgerImports returns the non-test Go files directly in dir that import a
// business ledger package.
func ledgerImports(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	forbidden := map[string]struct{}{
		modulePath + "/internal/ledger":      {},
		modulePath + "/internal/data/ledger": {},
	}
	var bad []string
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			if _, hit := forbidden[strings.Trim(imp.Path.Value, `"`)]; hit {
				bad = append(bad, name+" -> "+imp.Path.Value)
			}
		}
	}
	return bad
}

var numberedItemRE = regexp.MustCompile(`(?m)^\d+\.\s+`)

// extractNonNegotiables pulls the four numbered criteria out of
// workflow-runtime.md's "### Build or adopt" section.
func extractNonNegotiables(spec string) ([]string, error) {
	const startMarker = "### Build or adopt"
	const endMarker = "Migration, shadow mode, replay"

	start := strings.Index(spec, startMarker)
	if start < 0 {
		return nil, errNoMarker(startMarker)
	}
	section := spec[start:]
	end := strings.Index(section, endMarker)
	if end < 0 {
		return nil, errNoMarker(endMarker)
	}
	section = section[:end]

	// The numbered list is the paragraph containing "1. ".
	paras := strings.Split(section, "\n\n")
	var listPara string
	for _, p := range paras {
		if strings.Contains(p, "1. ") {
			listPara = p
			break
		}
	}
	if listPara == "" {
		return nil, errNoMarker("numbered non-negotiable list")
	}

	locs := numberedItemRE.FindAllStringIndex(listPara, -1)
	if len(locs) == 0 {
		return nil, errNoMarker("numbered list items")
	}
	items := make([]string, 0, len(locs))
	for i, loc := range locs {
		itemStart := loc[1]
		itemEnd := len(listPara)
		if i+1 < len(locs) {
			itemEnd = locs[i+1][0]
		}
		items = append(items, strings.TrimSpace(listPara[itemStart:itemEnd]))
	}
	return items, nil
}

func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

type markerError string

func (e markerError) Error() string { return "marker not found: " + string(e) }

func errNoMarker(marker string) error { return markerError(marker) }
