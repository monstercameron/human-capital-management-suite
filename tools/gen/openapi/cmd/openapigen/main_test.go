package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/openapi"
)

// repoRoot is five directories above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(wd, "..", "..", "..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root not found from %s: %v", wd, err)
	}
	return root
}

func TestRunWritesThenChecks(t *testing.T) {
	root := repoRoot(t)
	out := filepath.Join(t.TempDir(), "rpcs.openapi.yaml")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-root", root, "-out", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("write exit %d: %s", code, stderr.String())
	}
	want, err := openapi.Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("written document differs from Generate (err=%v)", err)
	}
	stdout.Reset()
	if code := run([]string{"-root", root, "-out", out, "-check"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "up to date") {
		t.Fatalf("check exit %d: %s %s", code, stdout.String(), stderr.String())
	}
	if err := os.WriteFile(out, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := run([]string{"-root", root, "-out", out, "-check"}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), openapi.RegenerateCommand) {
		t.Fatalf("stale check exit %d: %s", code, stderr.String())
	}
}

func TestRunFailures(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-no-such-flag"}, &stdout, &stderr); code != 2 {
		t.Fatalf("bad flag exit %d", code)
	}
	empty := t.TempDir()
	if code := run([]string{"-root", empty}, &stdout, &stderr); code != 1 {
		t.Fatalf("write without schema/proto exit %d", code)
	}
}
