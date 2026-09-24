package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/controlcrosswalk"
)

func TestTodo_GOV_030_Conformance(t *testing.T) {
	root := repoRoot(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root, "-format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var revision controlcrosswalk.Revision
	if err := json.Unmarshal(stdout.Bytes(), &revision); err != nil {
		t.Fatalf("JSON output is invalid: %v; output=%s", err, stdout.String())
	}
	if len(revision.Controls) != 51 || revision.Digest == "" {
		t.Fatalf("JSON revision has %d controls and digest %q; want 51 controls and a digest", len(revision.Controls), revision.Digest)
	}
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"-root", root}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("summary run() = %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "controlcrosswalk: PASS") || !strings.Contains(stdout.String(), "digest=") {
		t.Fatalf("summary = %q, want PASS and digest", stdout.String())
	}
}

func TestTodo_GOV_030_OwnerPackagesRejectUnresolvedAndEscapingPaths(t *testing.T) {
	root := t.TempDir()
	packagePath := filepath.Join(root, "internal", "security", "owner")
	if err := os.MkdirAll(packagePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packagePath, "owner.go"), []byte("package owner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	revision := controlcrosswalk.Revision{Controls: []controlcrosswalk.Control{{ID: "E-01", OwnerPackage: "internal/security/owner"}}}
	if err := validateOwnerPackages(root, revision); err != nil {
		t.Fatalf("valid owner package rejected: %v", err)
	}
	revision.Controls[0].OwnerPackage = "internal/security/missing"
	if err := validateOwnerPackages(root, revision); err == nil || !strings.Contains(err.Error(), "PACKAGE_NOT_FOUND") {
		t.Fatalf("missing owner package error = %v, want PACKAGE_NOT_FOUND", err)
	}
	revision.Controls[0].OwnerPackage = "../../outside"
	if err := validateOwnerPackages(root, revision); err == nil || !strings.Contains(err.Error(), "PACKAGE_OUTSIDE_ROOT") {
		t.Fatalf("escaping owner package error = %v, want PACKAGE_OUTSIDE_ROOT", err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve package path")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))))
}
