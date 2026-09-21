package depedge_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

// REV-017-01: OPS-002's telemetry-policy package
// (internal/operations/telemetry) was a fully tested duplicate of OBS-004's
// live evaluator (internal/platform/telemetry) that no served binary
// reached. The plan resolves it by deletion: the package must be either
// genuinely reachable from a served binary or absent from the module, never
// claimed-but-unreachable. These tests pin that resolution.

const rev017DuplicatePkg = "github.com/monstercameron/human-capital-management-suite/internal/operations/telemetry"

const rev017LivePkg = "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"

var rev017ServedBinaries = []string{"./cmd/hcmnext", "./cmd/worker"}

// rev017ServedDeps runs go list -deps over the served binaries from the
// module root and returns the reachable import-path set.
func rev017ServedDeps(t *testing.T) map[string]bool {
	t.Helper()
	root := repopath.RootDir()
	args := append([]string{"list", "-deps"}, rev017ServedBinaries...)
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list -deps %v failed: %v\nstderr:\n%s", rev017ServedBinaries, err, string(exit.Stderr))
		}
		t.Fatalf("go list -deps %v failed: %v", rev017ServedBinaries, err)
	}
	deps := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			deps[line] = true
		}
	}
	if len(deps) == 0 {
		t.Fatalf("go list -deps %v returned no packages", rev017ServedBinaries)
	}
	return deps
}

// rev017DuplicatePresent reports whether the duplicate package still ships
// Go sources in the checkout. An empty (or missing) directory is not a
// package: go list reports "no Go files" for it and no binary can reach it.
func rev017DuplicatePresent(t *testing.T) bool {
	t.Helper()
	dir := filepath.Join(repopath.RootDir(), "internal", "operations", "telemetry")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		t.Fatalf("reading %s: %v", dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}

// TestTodo_REV_017_01 is the REV-017-01 primary test: the OPS-002 duplicate
// must be resolved. It fails while internal/operations/telemetry exists on
// disk but is reachable from no served binary (claimed-but-unreachable),
// and passes once the package is either wired into a served binary or
// deleted from the module.
func TestTodo_REV_017_01(t *testing.T) {
	present := rev017DuplicatePresent(t)
	if !present {
		return
	}
	deps := rev017ServedDeps(t)
	if !deps[rev017DuplicatePkg] {
		t.Errorf("REV-017-01 RED: %s exists but is reachable from none of %v; it must either be wired into a served binary or deleted (the plan resolves it by deletion)",
			rev017DuplicatePkg, rev017ServedBinaries)
	}
}

// TestTodo_REV_017_01_Security pins the enforcement side of the resolution:
// the live evaluator stays reachable from the served binaries while the
// duplicate never is, so a future reintroduction cannot silently shadow the
// live path.
func TestTodo_REV_017_01_Security(t *testing.T) {
	deps := rev017ServedDeps(t)
	if !deps[rev017LivePkg] {
		t.Errorf("live evaluator %s is not reachable from %v; REV-017-01 must not be resolved by unwiring the live path",
			rev017LivePkg, rev017ServedBinaries)
	}
	if deps[rev017DuplicatePkg] && rev017DuplicatePresent(t) {
		t.Errorf("duplicate policy package %s is reachable from a served binary alongside the live evaluator %s; exactly one enforcement path may be served",
			rev017DuplicatePkg, rev017LivePkg)
	}
	if !rev017DuplicatePresent(t) && deps[rev017DuplicatePkg] {
		t.Errorf("stale dependency entry: %s is listed in served deps but has no source directory", rev017DuplicatePkg)
	}
}

// TestTodo_REV_017_01_Integration drives the real module graph: the served
// binaries must resolve exactly one owned telemetry policy root.
func TestTodo_REV_017_01_Integration(t *testing.T) {
	deps := rev017ServedDeps(t)
	var roots []string
	for dep := range deps {
		if strings.Contains(dep, "/operations/telemetry") || dep == rev017LivePkg {
			roots = append(roots, dep)
		}
	}
	sort.Strings(roots)
	if len(roots) != 1 || roots[0] != rev017LivePkg {
		t.Errorf("served telemetry policy roots = %v, want exactly [%s]", roots, rev017LivePkg)
	}
}

// TestTodo_REV_017_01_Golden pins the byte-exact resolution verdict so any
// reintroduction of the duplicate (or unwiring of the live evaluator) is a
// visible diff.
func TestTodo_REV_017_01_Golden(t *testing.T) {
	deps := rev017ServedDeps(t)
	verdict := "duplicate=absent\n"
	if rev017DuplicatePresent(t) {
		verdict = "duplicate=present\n"
	}
	live := "unreachable"
	if deps[rev017LivePkg] {
		live = "reachable"
	}
	verdict += "live=" + live + "\nserved=" + strings.Join(rev017ServedBinaries, ",") + "\n"

	goldenPath := filepath.Join(repopath.RootDir(), "tools", "policy", "depedge", "testdata", "rev017_resolution.golden.txt")
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}
	if verdict != string(want) {
		t.Errorf("resolution verdict mismatch:\n got:\n%s\nwant:\n%s", verdict, string(want))
	}
}
