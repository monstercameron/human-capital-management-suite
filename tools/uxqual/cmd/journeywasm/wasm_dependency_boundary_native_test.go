//go:build !js || !wasm

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// serverOnlyPackages must never be linked into the browser binary. The
// server workspace package builds and hashes whole stylesheets in
// package-level vars; compiled into wasm, those builds write through GWC's
// DOM css sink into a runtime <style> element that the page's
// content-security-policy blocks, thousands of times per page load.
var serverOnlyPackages = []string{
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace",
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app",
	"github.com/monstercameron/human-capital-management-suite/internal/application",
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey",
}

func TestWasmClientDoesNotLinkServerPackages(t *testing.T) {
	if testing.Short() {
		t.Skip("lists the js/wasm build graph")
	}
	cmd := exec.Command("go", "list", "-deps", ".")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps (js/wasm): %v\n%s", err, out)
	}
	deps := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		deps[strings.TrimSpace(line)] = true
	}
	for _, pkg := range serverOnlyPackages {
		if deps[pkg] {
			t.Errorf("the wasm client links server-only package %s", pkg)
		}
	}
}
