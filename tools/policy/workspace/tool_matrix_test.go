package workspace_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/workspace"
)

// TestTodo_TOOL_001_Conformance asks the Go tool itself to resolve the root
// module and all root packages. This catches a policy-only pass where the
// declared workspace cannot actually be loaded by Go.
func TestTodo_TOOL_001_Conformance(t *testing.T) {
	root := repopath.RootDir()
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Path}} {{.GoVersion}}")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -m failed: %v\n%s", err, out)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 || fields[0] != "github.com/monstercameron/human-capital-management-suite" || !workspace.IsPinnedGoVersion(fields[1]) {
		t.Fatalf("root module identity/version = %q, want the authoritative module and pinned Go version", strings.TrimSpace(string(out)))
	}
	cmd = exec.Command("go", "list", "./...")
	cmd.Dir = root
	if out, err = cmd.CombinedOutput(); err != nil || len(strings.TrimSpace(string(out))) == 0 {
		t.Fatalf("go list ./... did not resolve root packages: %v\n%s", err, out)
	}
}

// TestTodo_TOOL_001_Golden pins the root go.mod bytes. The module boundary
// and toolchain directive are repository architecture decisions.
func TestTodo_TOOL_001_Golden(t *testing.T) {
	root := repopath.RootDir()
	got, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	const wantPrefix = "module github.com/monstercameron/human-capital-management-suite\n\ngo 1.26.3\n"
	if !strings.HasPrefix(string(got), wantPrefix) {
		t.Fatalf("go.mod no longer begins with the pinned authoritative module contract; got prefix %q", string(got[:min(len(got), 100)]))
	}
}
