package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRegeneratesManifests(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.invalid/storagemanifest-test\n")
	mustWrite(t, filepath.Join(root, "migrations", "00001_fixture.sql"), "-- +goose Up\nCREATE TABLE intent_instance (id text);\n-- +goose Down\nDROP TABLE intent_instance;\n")

	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	var out, errOut bytes.Buffer
	if code := run(&out, &errOut); code != 0 {
		t.Fatalf("run code = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "regenerated") {
		t.Fatalf("stdout = %q, want regeneration summary", out.String())
	}
	for _, name := range []string{"storage-disposition.yaml", "property-sql-mappings.yaml", "relationship-lifecycle-constraints.yaml"} {
		if _, err := os.Stat(filepath.Join(root, "definitions", "model", name)); err != nil {
			t.Fatalf("generated %s: %v", name, err)
		}
	}
}

func TestRunReportsMissingMigrations(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.invalid/storagemanifest-test\n")
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	var out, errOut bytes.Buffer
	if code := run(&out, &errOut); code != 1 {
		t.Fatalf("run code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "scan migrations") {
		t.Fatalf("stderr = %q, want migration error", errOut.String())
	}
}

func TestRunReportsMissingRepository(t *testing.T) {
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.VolumeName(previous) + string(os.PathSeparator)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	var out, errOut bytes.Buffer
	if code := run(&out, &errOut); code != 1 {
		t.Fatalf("run code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "no go.mod found") {
		t.Fatalf("stderr = %q, want repository error", errOut.String())
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
