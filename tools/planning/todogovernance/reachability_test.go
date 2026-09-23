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
		ID:         id,
		Phase:      "GATE_B",
		Test:       "TestFixture",
		TestMatrix: map[string]string{"PRIMARY": "TestFixture"},
		Red:        "fixture red",
		Green:      green,
		Refactor:   "fixture refactor",
		Refs:       refs,
		Done:       done,
		Evidence:   evidence,
	}
}

func hasReachabilityFinding(findings []ReachabilityFinding, id, substr string) bool {
	for _, f := range findings {
		if f.ID == id && f.Kind == CodeUnreachableRuntime && strings.Contains(f.Detail, substr) {
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
		if !strings.Contains(f.Detail, "owner=LEAVE") {
			t.Errorf("detail names no owner tag, got %q", f.Detail)
		}
		if !strings.Contains(f.Detail, "internal/domains/leave") {
			t.Errorf("detail names no package, got %q", f.Detail)
		}
		const want = "REV-103-01: LEAVE-900: UNREACHABLE_RUNTIME: ticked runtime todo names only packages no binary reaches (owner=LEAVE; packages=internal/domains/leave)"
		if got := f.String(); got != want {
			t.Errorf("finding message changed:\n got:  %s\n want: %s", got, want)
		}
		const wantKey = "REV-103-01|LEAVE-900|UNREACHABLE_RUNTIME|owner=LEAVE; packages=internal/domains/leave"
		if got := f.Key(); got != wantKey {
			t.Errorf("finding key changed:\n got:  %s\n want: %s", got, wantKey)
		}
		if got := ReachabilityOwner("NODASH"); got != "NODASH" {
			t.Errorf("ReachabilityOwner(NODASH) = %q, want the ID itself", got)
		}
	})

	t.Run("declared library stays silent when unreachable", func(t *testing.T) {
		todos := []todoregistry.Todo{reachabilityTodo("R-101", true,
			"pure helper with no binary consumer by design `LIBRARY`",
			"`TestR101` in `internal/engines/eligibility`; `go test -count=1 ./internal/engines/eligibility/` PASS",
			"[model](data/models/x.md)")}
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
		if findings := CheckReachability(todos, map[string]bool{}); len(findings) != 0 {
			t.Errorf("expected a tools-only todo to stay silent, got %v", findings)
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

	t.Run("unticked retired and exempt-surface todos are ignored", func(t *testing.T) {
		todos := []todoregistry.Todo{
			reachabilityTodo("R-104", false, "green", "`TestR104` in `internal/domains/leave`", "[x](x.md)"),
			{ID: "R-105", Done: true, Retired: true, Green: "green", Evidence: "`TestR105` in `internal/domains/leave`"},
			reachabilityTodo("CHAT-900", true, "green", "`TestChat` in `internal/domains/leave`", "[x](x.md)"),
			reachabilityTodo("UXLIVE-900", true, "green", "`TestUxlive` in `internal/domains/leave`", "[x](x.md)"),
			reachabilityTodo("WF-UI-900", true, "green", "`TestWfui` in `internal/domains/leave`", "[x](x.md)"),
			reachabilityTodo("WEB-900", true, "green", "`TestWeb` in `internal/humanwork/productui`", "[x](x.md)"),
		}
		if findings := CheckReachability(todos, map[string]bool{}); len(findings) != 0 {
			t.Errorf("expected unticked, retired and exempt todos to stay silent, got %v", findings)
		}
	})

	t.Run("package tokens normalize", func(t *testing.T) {
		td := reachabilityTodo("R-106", true, "green",
			"`TestA` in `./internal/domains/leave/...`; `TestB` in `github.com/monstercameron/human-capital-management-suite/internal/engines/eligibility`; `TestC` in `github.com/monstercamarin/human-capital-management-suite/internal/domains/balance`",
			"command `go test ./...`, test `TestTodo_R_106`, file `pkg/x.go`, doc [m](x.md), prose `not a package`")
		want := []string{"internal/domains/balance", "internal/domains/leave", "internal/engines/eligibility"}
		got := TodoPackages(td)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("TodoPackages = %v, want %v", got, want)
		}
	})

	t.Run("live corpus still carries the RED gap", func(t *testing.T) {
		if testing.Short() {
			t.Skip("live binary-closure scan needs the full module graph")
		}
		todos := TodosFromRecords(loadRealMarkdown(t))
		reachable := loadLiveBinaryClosure(t)
		findings := CheckReachability(todos, reachable)
		if len(findings) == 0 {
			t.Fatal("expected live unreachable-tick findings, got none")
		}
		if !hasReachabilityFinding(findings, "LEAVE-001", "internal/domains/leave") {
			t.Errorf("expected LEAVE-001 to be flagged for internal/domains/leave")
		}
		if !hasReachabilityFinding(findings, "ELIG-001", "internal/engines/eligibility") {
			t.Errorf("expected ELIG-001 to be flagged for internal/engines/eligibility")
		}
		// internal/domains/payroll links into a shipped binary, so the
		// PAYRUN family must not be re-flagged once served.
		if hasAnyReachabilityFinding(findings, "PAYRUN-001") {
			t.Errorf("PAYRUN-001 names a served package and must stay silent")
		}
		for _, f := range findings {
			if isReachabilityExempt(f.ID) {
				t.Errorf("exempt-surface todo %s must never be flagged", f.ID)
			}
		}
		t.Logf("REV-103-01: %d live ticked runtime todo(s) name only unreachable packages", len(findings))
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

// loadLiveBinaryClosure runs `go list -deps ./cmd/...` against the live
// checkout and returns the normalized repo-relative package set. It is a
// test-only helper: the checker itself stays pure.
func loadLiveBinaryClosure(t *testing.T) map[string]bool {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "./cmd/...")
	cmd.Dir = filepath.Join("..", "..", "..")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps ./cmd/...: %v", err)
	}
	return NormalizeReachable(strings.Split(string(out), "\n"))
}
