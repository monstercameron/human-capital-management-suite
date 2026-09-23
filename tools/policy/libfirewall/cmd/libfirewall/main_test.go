package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

const fakeManifest = `version: 1
module: example.com/fake
modules:
  - path: example.com/tp
    role: INFRASTRUCTURE_MECHANIC
    semantic_owner: test
    license_owner: test
    security_owner: test
    allowed_import_roots: [somewhere-else]
    upgrade_sla: Best-effort.
    exposure: Test fixture.
    replacement_strategy: Test fixture.
`

func writeManifest(t *testing.T, root string) {
	t.Helper()
	writeFile(t, root, "definitions/architecture/dependency-roles.yaml", fakeManifest)
}

func TestRun_CleanTreePasses(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/fake\n\ngo 1.26\n")
	writeFile(t, root, "app/app.go", "package app\n\nimport \"fmt\"\n\nfunc Hello() string { return fmt.Sprint(1) }\n")
	writeManifest(t, root)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-root", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() = %d, want 0; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRun_ForbiddenImportFails(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/fake\n\ngo 1.26\n\nrequire example.com/tp v0.0.0\n\nreplace example.com/tp => ./thirdparty\n")
	writeFile(t, root, "thirdparty/go.mod", "module example.com/tp\n\ngo 1.26\n")
	writeFile(t, root, "thirdparty/tp.go", "package tp\n\nfunc Value() int { return 1 }\n")
	writeFile(t, root, "app/app.go", "package app\n\nimport \"example.com/tp\"\n\nfunc Value() int { return tp.Value() }\n")
	writeManifest(t, root)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-root", root}, &stdout, &stderr); code != 1 {
		t.Fatalf("run() = %d, want 1; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "app imports example.com/tp") {
		t.Errorf("stdout = %q, want it to name the forbidden edge", stdout.String())
	}
}

func TestRun_MissingManifestIsScanError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-root", t.TempDir()}, &stdout, &stderr); code != 2 {
		t.Fatalf("run() = %d, want 2; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRun_BadFlagIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-bogus"}, &stdout, &stderr); code != 2 {
		t.Fatalf("run() = %d, want 2; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}
