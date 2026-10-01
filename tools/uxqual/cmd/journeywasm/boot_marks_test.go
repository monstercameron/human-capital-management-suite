//go:build !(js && wasm)

package main

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// TestTodo_UXBLIND_089_BootMarks keeps the client's mark names identical to
// the ones the shell's budget (and its live browser spec) reads.
func TestTodo_UXBLIND_089_BootMarks(t *testing.T) {
	if bootMarkPrefix != workspace.ColdStartMarkPrefix {
		t.Fatalf("client mark prefix %q, shell reads %q", bootMarkPrefix, workspace.ColdStartMarkPrefix)
	}
	if got, want := bootPhases(), workspace.ColdStartClientPhases(); !slices.Equal(got, want) {
		t.Fatalf("client phases %v, shell budget reads %v", got, want)
	}
}

// TestTodo_UXBLIND_089_BundleDeps proves the bundle is built without gRPC's
// server-side request tracing, which on its own pulled html/template and
// text/template into every cold load.
func TestTodo_UXBLIND_089_BundleDeps(t *testing.T) {
	if !strings.Contains(","+wasmBuildTags+",", ",grpcnotrace,") {
		t.Fatalf("wasm build tags %q do not disable gRPC tracing", wasmBuildTags)
	}
	goBin, err := goBinary()
	if err != nil {
		t.Fatalf("go toolchain: %v", err)
	}
	cmd := exec.Command(goBin, "list", "-deps", "-tags="+wasmBuildTags, wasmPackage)
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	for _, dependency := range strings.Fields(string(out)) {
		switch dependency {
		case "golang.org/x/net/trace", "html/template", "text/template":
			t.Errorf("the browser bundle still links %s", dependency)
		}
	}
}
