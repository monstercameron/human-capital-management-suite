package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// TestTodo_REV_034_01_Integration drives manifest pinning end to end against
// a fixture migrations directory and proves the CLI refuses to call a
// rehearsal successful without observed database evidence.
func TestTodo_REV_034_01_Integration(t *testing.T) {
	dir := fixtureMigrations(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-dir", dir, "-json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("scan exit = %d, want 0\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"Compatibility": "UNREVIEWED"`) {
		t.Fatalf("scan invented a compatibility claim:\n%s", stdout.String())
	}
	manifestPath := writeManifest(t, stdout.Bytes())
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-dir", dir, "-manifest", manifestPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("pinned manifest exit = %d, want 0\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "manifest valid: 2 entries") {
		t.Fatalf("pinned manifest stdout does not report the valid manifest:\n%s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-dir", dir, "-manifest", manifestPath, "-rehearse"}, &stdout, &stderr); code != 1 {
		t.Fatalf("evidence-free rehearsal exit = %d, want 1\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "database-url") {
		t.Fatalf("rehearsal without a disposable database URL was not refused clearly:\n%s", stderr.String())
	}
}

func writeManifest(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

// TestTodo_REV_034_01_Fault proves altered, added, and missing migrations
// fail against pinned evidence instead of being silently rescanned.
func TestTodo_REV_034_01_Fault(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, dir string)
		want   string
	}{
		{
			name: "altered bytes",
			change: func(t *testing.T, dir string) {
				writeMigration(t, dir, "00002_second.sql", "-- +goose Up\nALTER TABLE first ADD COLUMN tampered text;\n")
			},
			want: "release digest differs from pinned manifest",
		},
		{
			name: "added migration",
			change: func(t *testing.T, dir string) {
				writeMigration(t, dir, "00003_third.sql", "-- +goose Up\nSELECT 1;\n")
			},
			want: "release digest differs from pinned manifest",
		},
		{
			name: "missing migration",
			change: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "00002_second.sql")); err != nil {
					t.Fatalf("remove migration: %v", err)
				}
			},
			want: "release digest differs from pinned manifest",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixtureMigrations(t)
			var scan bytes.Buffer
			if code := run([]string{"-dir", dir, "-json"}, &scan, &bytes.Buffer{}); code != 0 {
				t.Fatalf("scan exit = %d, want 0", code)
			}
			manifestPath := writeManifest(t, scan.Bytes())
			tc.change(t, dir)
			var stdout, stderr bytes.Buffer
			if code := run([]string{"-dir", dir, "-manifest", manifestPath}, &stdout, &stderr); code != 1 {
				t.Fatalf("pinned manifest exit = %d, want 1\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("error = %q, want it to contain %q", stderr.String(), tc.want)
			}
		})
	}
}

// TestTodo_REV_034_01_Recovery proves a migration tree that failed the pinned
// manifest check passes again once the exact original bytes are restored.
func TestTodo_REV_034_01_Recovery(t *testing.T) {
	dir := fixtureMigrations(t)
	original := "-- +goose Up\nALTER TABLE first ADD COLUMN note text;\n"
	var scan bytes.Buffer
	if code := run([]string{"-dir", dir, "-json"}, &scan, &bytes.Buffer{}); code != 0 {
		t.Fatalf("scan exit = %d, want 0", code)
	}
	manifestPath := writeManifest(t, scan.Bytes())
	writeMigration(t, dir, "00002_second.sql", "-- +goose Up\nALTER TABLE first ADD COLUMN tampered text;\n")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-dir", dir, "-manifest", manifestPath}, &stdout, &stderr); code != 1 {
		t.Fatalf("tampered manifest check exit = %d, want 1", code)
	}
	writeMigration(t, dir, "00002_second.sql", original)
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-dir", dir, "-manifest", manifestPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("restored manifest check exit = %d, want 0\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "manifest valid: 2 entries") {
		t.Fatalf("restored check did not report valid manifest: %q", stdout.String())
	}
}
