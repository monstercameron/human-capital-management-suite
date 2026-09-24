package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowmaturity"
)

// repoRoot is the path from this command's directory to the repository
// root: tools/planning/cmd/workflowmaturity is four levels below root.
const repoRoot = "../../../.."

func TestRunReportsLiveRepository(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", repoRoot}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() exit = %d, want 0 for the exact pinned open baseline; stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"hcmnext.people.change_manager/v1",
		"catalog.md cross-check:",
		"catalog baseline: status=OPEN_UNASSIGNED known=30 open_unassigned=30",
		"workflowmaturity: total=",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q; got:\n%s", want, out)
		}
	}
	if strings.Contains(stderr.String(), "CATALOG BASELINE FAILURE:") {
		t.Fatalf("known open baseline unexpectedly failed: %s", stderr.String())
	}
}

func TestRunWritesReportFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "report.json")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", repoRoot, "-out", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() exit = %d, want 0 while the exact open baseline is pinned; stderr: %s", code, stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("expected report file at %s: %v", out, err)
	}
	if !strings.Contains(string(data), "\"total_definitions\"") {
		t.Fatalf("report file does not look like a workflowmaturity report:\n%s", data)
	}
}

func TestRunFailsOnNewCatalogMismatch(t *testing.T) {
	baseline, err := workflowmaturity.LoadCatalogBaseline(repoRoot, workflowmaturity.DefaultCatalogBaseline)
	if err != nil {
		t.Fatalf("LoadCatalogBaseline: %v", err)
	}
	baseline.Entries = baseline.Entries[1:]
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		t.Fatalf("marshal reduced baseline: %v", err)
	}
	baselinePath := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(baselinePath, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write reduced baseline: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", repoRoot, "-catalog-baseline", baselinePath}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run() exit = %d, want 1 for unreviewed live mismatch; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unreviewed catalog disagreement: WF-BEN-003 hcmnext.operations.create_repair_plan/v1") {
		t.Fatalf("stderr did not name the mismatch missing from the baseline: %s", stderr.String())
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
