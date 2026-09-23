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

func TestRun_CleanFilePasses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clean.go")
	writeFile(t, dir, "clean.go", `package clean

func Checked() (int, error) { return 1, nil }

func Call() error {
	v, err := Checked()
	if err != nil {
		return err
	}
	_ = v
	return nil
}
`)

	var stdout, stderr bytes.Buffer
	if code := run([]string{path}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() = %d, want 0; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRun_BlankedCallFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dirty.go")
	writeFile(t, dir, "dirty.go", `package dirty

func Dropped() error {
	_ = compute()
	return nil
}

func compute() error { return nil }
`)

	var stdout, stderr bytes.Buffer
	if code := run([]string{path}, &stdout, &stderr); code != 1 {
		t.Fatalf("run() = %d, want 1; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "dirty.go") {
		t.Errorf("stdout = %q, want it to name the violating file", stdout.String())
	}
}

func TestRun_NoFilesIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{}, &stdout, &stderr); code != 2 {
		t.Fatalf("run() = %d, want 2; stderr=%s", code, stderr.String())
	}
}

func TestRun_MissingFileIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{filepath.Join(t.TempDir(), "missing.go")}, &stdout, &stderr); code != 2 {
		t.Fatalf("run() = %d, want 2; stderr=%s", code, stderr.String())
	}
}
