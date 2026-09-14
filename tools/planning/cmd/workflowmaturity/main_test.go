package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot is the path from this command's directory to the repository
// root: tools/planning/cmd/workflowmaturity is four levels below root.
const repoRoot = "../../../.."

func TestRunReportsLiveRepository(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", repoRoot}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() exit = %d, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"hcmnext.people.change_manager/v1",
		"catalog.md cross-check:",
		"workflowmaturity: total=",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q; got:\n%s", want, out)
		}
	}
}

func TestRunWritesReportFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "report.json")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", repoRoot, "-out", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() exit = %d, stderr: %s", code, stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("expected report file at %s: %v", out, err)
	}
	if !strings.Contains(string(data), "\"total_definitions\"") {
		t.Fatalf("report file does not look like a workflowmaturity report:\n%s", data)
	}
}

func TestRunFailsOnMissingRepository(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", filepath.Join(repoRoot, "does-not-exist")}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected a non-zero exit for a missing repository root")
	}
	if stderr.Len() == 0 {
		t.Fatal("expected an error message on stderr")
	}
}
