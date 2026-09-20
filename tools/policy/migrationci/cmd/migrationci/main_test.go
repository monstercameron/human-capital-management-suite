package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/migrationci"
)

func writeMigration(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write migration %s: %v", name, err)
	}
}

func fixtureMigrations(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeMigration(t, dir, "00001_first.sql", "-- +goose Up\nCREATE TABLE first (id bigint PRIMARY KEY);\n")
	writeMigration(t, dir, "00002_second.sql", "-- +goose Up\nALTER TABLE first ADD COLUMN note text;\n")
	return dir
}

// TestTodo_REV_034_01_Integration drives the new migration-rehearsal entry
// point end to end against a fixture migrations directory: a clean scan
// validates, and a full rehearsal with asserted preconditions passes.
func TestTodo_REV_034_01_Integration(t *testing.T) {
	dir := fixtureMigrations(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-dir", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("scan exit = %d, want 0\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "manifest valid: 2 entries") {
		t.Fatalf("scan stdout does not report the valid manifest:\n%s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	flags := []string{"-dir", dir, "-rehearse", "-backfill-complete", "-shadow-exact", "-mixed-version-compatible"}
	if code := run(flags, &stdout, &stderr); code != 0 {
		t.Fatalf("rehearse exit = %d, want 0\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "migration rehearsal PASS") {
		t.Fatalf("rehearse stdout does not report PASS:\n%s", stdout.String())
	}
}

// TestTodo_REV_034_01_Fault proves a migration edited after the manifest
// was pinned fails the rehearsal instead of passing silently.
func TestTodo_REV_034_01_Fault(t *testing.T) {
	dir := fixtureMigrations(t)
	var stdout bytes.Buffer
	if code := run([]string{"-dir", dir, "-json"}, &stdout, &bytes.Buffer{}); code != 0 {
		t.Fatalf("scan exit = %d, want 0", code)
	}
	var manifest migrationci.Manifest
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatalf("decode scanned manifest: %v", err)
	}
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	writeMigration(t, dir, "00002_second.sql", "-- +goose Up\nALTER TABLE first ADD COLUMN tampered text;\n")
	stdout.Reset()
	var stderr bytes.Buffer
	flags := []string{"-dir", dir, "-manifest", manifestPath, "-rehearse", "-backfill-complete", "-shadow-exact", "-mixed-version-compatible"}
	if code := run(flags, &stdout, &stderr); code != 1 {
		t.Fatalf("tampered rehearse exit = %d, want 1\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "CHECKSUM_MISMATCH") {
		t.Fatalf("tampered rehearse stdout does not name CHECKSUM_MISMATCH:\n%s", stdout.String())
	}
}
