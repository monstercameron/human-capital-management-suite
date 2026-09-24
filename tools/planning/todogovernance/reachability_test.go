package todogovernance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

func reachabilityTodo(id string, done bool, green, evidence, refs string) todoregistry.Todo {
	return todoregistry.Todo{
		ID:              id,
		Phase:           "GATE_B",
		Test:            "TestFixture",
		TestMatrix:      map[string]string{"PRIMARY": "TestFixture"},
		Red:             "fixture red",
		Green:           green,
		Refactor:        "fixture refactor",
		Refs:            refs,
		Done:            done,
		Evidence:        evidence,
		CapabilityClass: todoregistry.CapabilityRuntime,
		Owner:           "TEST_OWNER",
	}
}

func hasReachabilityFinding(findings []ReachabilityFinding, id, substr string) bool {
	for _, f := range findings {
		if f.ID == id && strings.Contains(f.Detail, substr) {
			return true
		}
	}
	return false
}

func hasAnyReachabilityFinding(findings []ReachabilityFinding, id string) bool {
	for _, f := range findings {
		if f.ID == id {
			return true
		}
	}
	return false
}

// TestTodo_REV_103_01 is the REV-103-01 primary: the plan check fails a
// ticked runtime todo whose packages no binary reaches. Fixtures prove
// each classification branch; the live-corpus subtest proves the RED gap
// (LEAVE/ELIG families still unreachable) is detected rather than
// silently accepted, while newly served packages (payroll) stay silent.
func TestTodo_REV_103_01(t *testing.T) {
	t.Run("served runtime todo is silent", func(t *testing.T) {
		todos := []todoregistry.Todo{reachabilityTodo("R-100", true,
			"returns exactly the accepted state",
			"`TestR100` in `internal/domains/payroll`; `go test -count=1 ./internal/domains/payroll/` PASS",
			"[model](data/models/x.md)")}
		reachable := map[string]bool{"internal/domains/payroll": true}
		if findings := CheckReachability(todos, reachable); len(findings) != 0 {
			t.Errorf("expected zero findings for a served runtime todo, got %v", findings)
		}
	})

	t.Run("unreachable runtime todo is flagged once with owner metadata", func(t *testing.T) {
		todos := []todoregistry.Todo{reachabilityTodo("LEAVE-900", true,
			"generated request accepts worker and governed evidence refs only",
			"`TestTodo_LEAVE_900` in `internal/domains/leave`; `go test -count=1 ./internal/domains/leave/` PASS",
			"[Leave workflow](workflows/leave/x.md)")}
		findings := CheckReachability(todos, map[string]bool{})
		if len(findings) != 1 {
			t.Fatalf("expected one finding, got %v", findings)
		}
		f := findings[0]
		if f.Kind != CodeUnreachableRuntime {
			t.Errorf("kind = %q, want %q", f.Kind, CodeUnreachableRuntime)
		}
		if !strings.Contains(f.Detail, "owner=TEST_OWNER") {
			t.Errorf("detail names no owner tag, got %q", f.Detail)
		}
		if !strings.Contains(f.Detail, "internal/domains/leave") {
			t.Errorf("detail names no package, got %q", f.Detail)
		}
		const want = "REV-103-01: LEAVE-900: UNREACHABLE_RUNTIME: ticked runtime todo names only packages no binary reaches (owner=TEST_OWNER; capability=RUNTIME; packages=internal/domains/leave)"
		if got := f.String(); got != want {
			t.Errorf("finding message changed:\n got:  %s\n want: %s", got, want)
		}
		const wantKey = "REV-103-01|LEAVE-900|UNREACHABLE_RUNTIME|owner=TEST_OWNER; capability=RUNTIME; packages=internal/domains/leave"
		if got := f.Key(); got != wantKey {
			t.Errorf("finding key changed:\n got:  %s\n want: %s", got, wantKey)
		}
	})

	t.Run("declared library stays silent when unreachable", func(t *testing.T) {
		todos := []todoregistry.Todo{reachabilityTodo("R-101", true,
			"pure helper with no binary consumer by design `LIBRARY`",
			"`TestR101` in `internal/engines/eligibility`; `go test -count=1 ./internal/engines/eligibility/` PASS",
			"[model](data/models/x.md)")}
		todos[0].CapabilityClass = todoregistry.CapabilityLibrary
		if findings := CheckReachability(todos, map[string]bool{}); len(findings) != 0 {
			t.Errorf("expected a declared library to stay silent, got %v", findings)
		}
	})

	t.Run("declared runtime is enforced", func(t *testing.T) {
		mk := func(evidence string) todoregistry.Todo {
			return reachabilityTodo("R-102", true,
				"ships in the worker binary `RUNTIME`", evidence,
				"[model](data/models/x.md)")
		}
		unreached := []todoregistry.Todo{mk("`TestR102` in `tools/planning/rolloutplan`")}
		if findings := CheckReachability(unreached, map[string]bool{}); len(findings) != 1 {
			t.Errorf("expected a declared runtime naming only tooling to be flagged, got %v", findings)
		}
		served := []todoregistry.Todo{mk("`TestR102` in `internal/domains/payroll`")}
		if findings := CheckReachability(served, map[string]bool{"internal/domains/payroll": true}); len(findings) != 0 {
			t.Errorf("expected a served declared runtime to stay silent, got %v", findings)
		}
	})

	t.Run("tooling-only todo is library by location", func(t *testing.T) {
		todos := []todoregistry.Todo{reachabilityTodo("ROLLOUT-900", true,
			"compiles the convergence record",
			"`TestRollout` in `tools/planning/rolloutplan`; `go test -count=1 ./tools/planning/rolloutplan/` PASS",
			"[blueprint](planning/x.md)")}
		todos[0].CapabilityClass = todoregistry.CapabilityLibrary
		if findings := CheckReachability(todos, map[string]bool{}); len(findings) != 0 {
			t.Errorf("expected a tools-only todo to stay silent, got %v", findings)
		}
	})

	t.Run("unclassified package evidence fails closed without ID inference", func(t *testing.T) {
		td := reachabilityTodo("LEAVE-999", true, "green", "`internal/domains/leave`", "")
		td.CapabilityClass = ""
		td.Owner = ""
		findings := CheckReachability([]todoregistry.Todo{td}, map[string]bool{})
		if len(findings) != 1 || findings[0].Kind != CodeUnclassifiedCapability {
			t.Fatalf("unclassified runtime-looking package must fail closed, got %+v", findings)
		}
		if strings.Contains(findings[0].Detail, "owner=LEAVE") {
			t.Fatalf("owner was inferred from TODO ID: %+v", findings[0])
		}
	})

	t.Run("package-less todo is silent", func(t *testing.T) {
		todos := []todoregistry.Todo{reachabilityTodo("R-103", true,
			"records the approved wording",
			"reviewed by hand; `go test -count=1 ./pkg/x/` PASS",
			"[plan](plan.md)")}
		if findings := CheckReachability(todos, map[string]bool{}); len(findings) != 0 {
			t.Errorf("expected a todo naming no packages to stay silent, got %v", findings)
		}
	})

	t.Run("unticked and retired todos are ignored", func(t *testing.T) {
		todos := []todoregistry.Todo{
			reachabilityTodo("R-104", false, "green", "`TestR104` in `internal/domains/leave`", "[x](x.md)"),
			{ID: "R-105", Done: true, Retired: true, Green: "green", Evidence: "`TestR105` in `internal/domains/leave`"},
		}
		if findings := CheckReachability(todos, map[string]bool{}); len(findings) != 0 {
			t.Errorf("expected unticked and retired todos to stay silent, got %v", findings)
		}
	})

	t.Run("chat and UI families follow package reachability", func(t *testing.T) {
		todos := []todoregistry.Todo{
			reachabilityTodo("CHAT-900", true, "green", "`TestChat` in `internal/domains/chat`", "[x](x.md)"),
			reachabilityTodo("UXLIVE-900", true, "green", "`TestUxlive` in `internal/humanwork/productui`", "[x](x.md)"),
			reachabilityTodo("WF-UI-900", true, "green", "`TestWfui` in `internal/workflow/ui`", "[x](x.md)"),
			reachabilityTodo("WEB-900", true, "green", "`TestWeb` in `internal/humanwork/productui`", "[x](x.md)"),
		}
		findings := CheckReachability(todos, map[string]bool{})
		if len(findings) != len(todos) {
			t.Fatalf("expected each unreachable runtime package to be flagged, got %v", findings)
		}
		for _, todo := range todos {
			if !hasAnyReachabilityFinding(findings, todo.ID) {
				t.Errorf("unreachable runtime todo %s was silently exempted", todo.ID)
			}
		}
	})

	t.Run("package tokens normalize", func(t *testing.T) {
		td := reachabilityTodo("R-106", true, "green",
			"`TestA` in `./internal/domains/leave/...`; `TestB` in `github.com/monstercameron/human-capital-management-suite/internal/engines/eligibility`; `TestC` in `github.com/monstercamarin/human-capital-management-suite/internal/domains/balance`",
			"command `go test ./...`, test `TestTodo_R_106`, file `pkg/x.go`, repo roots `internal` `cmd` `pkg` `tools` `test` `gen`, doc [m](x.md), prose `not a package`")
		want := []string{"internal/domains/balance", "internal/domains/leave", "internal/engines/eligibility"}
		got := TodoPackages(td)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("TodoPackages = %v, want %v", got, want)
		}
	})

	t.Run("repository roots are not Go package references", func(t *testing.T) {
		td := reachabilityTodo("R-107", true, "green",
			"the repository roots are `internal` `cmd` `pkg` `tools` `test` and `gen`",
			"")
		if got := TodoPackages(td); len(got) != 0 {
			t.Fatalf("repository roots are not packages, got %v", got)
		}
		if findings := CheckReachability([]todoregistry.Todo{td}, map[string]bool{}); len(findings) != 0 {
			t.Errorf("non-package directory references must not create runtime findings, got %v", findings)
		}
	})

	t.Run("stale and nonexistent package paths are ignored", func(t *testing.T) {
		td := reachabilityTodo("R-108", true, "green",
			"`TestR108` in `internal/domains/leave`; old ref `internal/domains/missing`",
			"")
		findings := CheckReachabilityInPackages(
			[]todoregistry.Todo{td},
			map[string]bool{},
			map[string]bool{"internal/domains/leave": true},
		)
		if len(findings) != 1 {
			t.Fatalf("expected the existing but unreachable package to remain actionable, got %v", findings)
		}
		if !strings.Contains(findings[0].Detail, "packages=internal/domains/leave") || strings.Contains(findings[0].Detail, "missing") {
			t.Errorf("finding should include only repository packages, got %q", findings[0].Detail)
		}
	})

	t.Run("live corpus still carries the RED gap", func(t *testing.T) {
		if testing.Short() {
			t.Skip("live binary-closure scan needs the full module graph")
		}
		todos := TodosFromRecords(loadRealMarkdown(t))
		reachable := loadLiveBinaryClosure(t)
		allPackages := loadLivePackages(t)
		findings := CheckReachabilityInPackages(todos, reachable, allPackages)
		if len(findings) == 0 {
			t.Fatal("expected live unreachable-tick findings, got none")
		}
		for _, binary := range []string{
			"cmd/hcmnext",
			"cmd/scheduler",
			"cmd/worker",
			"cmd/migrate",
			"cmd/hcmctl",
			"cmd/projector",
		} {
			if !reachable[binary] {
				t.Errorf("shipped binary package %s is absent from its own dependency closure", binary)
			}
		}
		if reachable["cmd/frontenddev"] {
			t.Error("developer-only frontenddev command must not count as a shipped binary")
		}
		if !hasReachabilityFinding(findings, "LEAVE-001", "internal/domains/leave") {
			t.Errorf("expected LEAVE-001 to be flagged for internal/domains/leave")
		}
		if hasAnyReachabilityFinding(findings, "ELIG-001") {
			t.Errorf("ELIG-001 was reopened and must not be checked as a completed tick")
		}
		// internal/domains/payroll links into a shipped binary, so the
		// PAYRUN family must not be re-flagged once served.
		if hasAnyReachabilityFinding(findings, "PAYRUN-001") {
			t.Errorf("PAYRUN-001 names a served package and must stay silent")
		}
		t.Logf("REV-103-01: %d live ticked todo(s) need explicit disposition for unreachable packages", len(findings))
	})
}

// TestTodo_REV_103_01_Golden pins the exact rendered bytes for a fixed
// two-todo fixture so a future refactor cannot silently change diagnostic
// wording or ordering.
func TestTodo_REV_103_01_Golden(t *testing.T) {
	todos := []todoregistry.Todo{
		reachabilityTodo("R-900", true,
			"generated request accepts worker and governed evidence refs only",
			"`TestR900` in `internal/domains/leave`; `go test -count=1 ./internal/domains/leave/` PASS",
			"[Leave workflow](workflows/leave/x.md)"),
		reachabilityTodo("R-901", true,
			"result binds program, fact and rule snapshots",
			"`TestR901` in `./internal/engines/eligibility/...`; `go test -count=1 ./internal/engines/eligibility/` PASS",
			"[Rules models](data/models/rules-and-decisions.md)"),
	}
	got := RenderReachabilityFindings(CheckReachability(todos, map[string]bool{}))
	want, err := os.ReadFile(filepath.Join("testdata", "rev10301.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch:\n got:  %q\n want: %q", got, string(want))
	}
}

// loadLiveBinaryClosure runs go list for the six binaries scripts/build.sh
// actually ships. A wildcard under cmd/ also includes developer-only
// commands such as frontenddev, which must not make a runtime todo appear
// served. It is a test-only helper: the checker itself stays pure.
func loadLiveBinaryClosure(t *testing.T) map[string]bool {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps",
		"./cmd/hcmnext",
		"./cmd/scheduler",
		"./cmd/worker",
		"./cmd/migrate",
		"./cmd/hcmctl",
		"./cmd/projector",
	)
	cmd.Dir = filepath.Join("..", "..", "..")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps shipped command set: %v", err)
	}
	return NormalizeReachable(strings.Split(string(out), "\n"))
}

func loadLivePackages(t *testing.T) map[string]bool {
	t.Helper()
	cmd := exec.Command("go", "list", "./...")
	cmd.Dir = filepath.Join("..", "..", "..")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list repository packages: %v", err)
	}
	return NormalizeReachable(strings.Split(string(out), "\n"))
}
