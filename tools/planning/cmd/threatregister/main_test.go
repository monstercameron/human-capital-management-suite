package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/threatregister"
)

// repoRoot is the path from this command's directory to the repository
// root: tools/planning/cmd/threatregister is four levels below root, exactly
// like tools/planning/cmd/pilotprovider.
const repoRoot = "../../../.."

func TestRunReportsMitigatedThreatRegister(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-register", repoRoot + "/definitions/planning/gates/threat-001-register.yaml"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("expected exact-path THR-07 mitigation to permit release: %v; stderr: %s", err, stderr.String())
	}
	if strings.Contains(stderr.String(), "VIOLATION:") {
		t.Errorf("unexpected structural violation: %s", stderr.String())
	}
	if strings.Contains(stderr.String(), "THR-07") || strings.Contains(stderr.String(), "RELEASE BLOCKER:") {
		t.Errorf("mitigated THR-07 was still reported as a blocker: %s", stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"signature verified: true",
		"release blocked: false",
		"threats: 9",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q; got:\n%s", want, out)
		}
	}
}

func TestRunFailsOnUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-this-flag-does-not-exist"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected a flag-parse error, got nil")
	}
}

func TestRunFailsOnMissingRegister(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-register", repoRoot + "/definitions/planning/gates/does-not-exist.yaml"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected an error for a missing register file, got nil")
	}
	if !strings.Contains(err.Error(), "loading") {
		t.Errorf("expected a loading error, got: %v", err)
	}
}

// writeStructurallyBrokenRegister writes a register missing several RED
// elements (no threats, no signature) to a temp file, so run() must report
// multiple violations and never reach the release-decision branch as
// "clean".
func writeStructurallyBrokenRegister(t *testing.T) string {
	t.Helper()
	r := threatregister.Register{
		SchemaVersion: 1,
		TodoID:        "THREAT-001",
		SignedDate:    "2026-09-13",
		Slices: []threatregister.Slice{
			{SliceID: "broken-fixture"},
		},
	}
	b, err := yaml.Marshal(r)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "register.yaml")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestRunReportsMultipleStructuralViolations(t *testing.T) {
	path := writeStructurallyBrokenRegister(t)
	var stdout, stderr bytes.Buffer
	err := run([]string{"-register", path}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected an error: the fixture register is deliberately incomplete")
	}
	if !strings.Contains(stderr.String(), "VIOLATION: slices[0].threats:") {
		t.Errorf("expected stderr to report the missing threats, got: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "VIOLATION: signature:") {
		t.Errorf("expected stderr to report the missing signature, got: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "signature verified: false") {
		t.Errorf("expected stdout to report signature verified: false, got: %s", stdout.String())
	}
}
