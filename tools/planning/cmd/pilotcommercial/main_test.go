package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotcommercial"
)

// repoRoot is the path from this command's directory to the repository
// root: tools/planning/cmd/pilotcommercial is four levels below root,
// exactly like tools/planning/cmd/pilotprovider.
const repoRoot = "../../../.."

func flags(t *testing.T) []string {
	t.Helper()
	return []string{
		"-freeze", repoRoot + "/definitions/planning/gates/commercial-001-pilot-package.yaml",
		"-ceiling", repoRoot + "/definitions/planning/gates/phase1-scope-ceiling.yaml",
		"-jurisdiction", repoRoot + "/definitions/planning/gates/select-001-jurisdiction-profile.yaml",
		"-provider", repoRoot + "/definitions/planning/gates/select-002-provider-topology.yaml",
	}
}

func TestRunReportsACleanConformingFreeze(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(flags(t), &stdout, &stderr)
	if err != nil {
		t.Fatalf("expected no error: the checked-in freeze is structurally complete, signed and conformant, got: %v (stderr: %s)", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"signature verified: true",
		"conforms to live registries: true",
		"provider: status=NONE",
		"slo: status=NONE",
		"entitlements: 8",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q; got:\n%s", want, out)
		}
	}
	if stderr.String() != "" {
		t.Errorf("expected empty stderr for a clean run, got: %s", stderr.String())
	}
}

func TestRunFailsOnUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-this-flag-does-not-exist"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected a flag-parse error, got nil")
	}
}

func TestRunFailsOnMissingFreeze(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-freeze", repoRoot + "/definitions/planning/gates/does-not-exist.yaml"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected an error for a missing freeze file, got nil")
	}
	if !strings.Contains(err.Error(), "loading") {
		t.Errorf("expected a loading error, got: %v", err)
	}
}

// writeStructurallyBrokenFreeze writes a freeze missing several RED
// elements (no entitlements, no signature) to a temp file, so run() must
// report multiple violations and never reach the registry-conformance
// branch cleanly.
func writeStructurallyBrokenFreeze(t *testing.T) string {
	t.Helper()
	f := pilotcommercial.PilotCommercialFreeze{
		SchemaVersion: 1,
		TodoID:        "COMMERCIAL-001",
		SignedDate:    "2026-09-13",
	}
	b, err := yaml.Marshal(f)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "freeze.yaml")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestRunReportsMultipleStructuralViolations(t *testing.T) {
	path := writeStructurallyBrokenFreeze(t)
	args := append([]string{"-freeze", path}, flags(t)[2:]...)
	var stdout, stderr bytes.Buffer
	err := run(args, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected an error: the fixture freeze is deliberately incomplete")
	}
	if !strings.Contains(stderr.String(), "VIOLATION: intent.entitlements:") {
		t.Errorf("expected stderr to report the missing entitlements, got: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "VIOLATION: signature:") {
		t.Errorf("expected stderr to report the missing signature, got: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "signature verified: false") {
		t.Errorf("expected stdout to report signature verified: false, got: %s", stdout.String())
	}
}
