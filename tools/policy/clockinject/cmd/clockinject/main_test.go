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

func TestRun_CleanTreePasses(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "internal/engines/a/a.go", `package a

func Pure() int { return 1 }
`)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-root", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() = %d, want 0; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRun_DirectReadFails(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "internal/domains/b/b.go", `package b

import "time"

func Stamp() time.Time { return time.Now().UTC() }
`)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-root", root}, &stdout, &stderr); code != 1 {
		t.Fatalf("run() = %d, want 1; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "internal/domains/b/b.go") {
		t.Errorf("stdout = %q, want it to name the violating file", stdout.String())
	}
}

func TestRun_MissingRootScansEmpty(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-root", filepath.Join(t.TempDir(), "missing")}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() = %d, want 0 for a tree with no scan roots; stderr=%s", code, stderr.String())
	}
}

func TestRun_NoArgsIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-bad-flag"}, &stdout, &stderr); code != 2 {
		t.Fatalf("run() = %d, want 2; stderr=%s", code, stderr.String())
	}
}
