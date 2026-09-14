package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotprovider"
)

// repoRoot is the path from this command's directory to the repository
// root: tools/planning/cmd/pilotprovider is four levels below root, exactly
// like tools/planning/cmd/pilotjurisdiction and tools/planning/cmd/scopeceiling.
const repoRoot = "../../../.."

func TestRunReportsACleanPlaceholderThatFailsTheRealSelectionGate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-topology", repoRoot + "/definitions/planning/gates/select-002-provider-topology.yaml"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("expected no error: the checked-in topology is a structurally complete, signed placeholder, got: %v", err)
	}
	if !strings.Contains(stderr.String(), "REAL SELECTION GATE:") {
		t.Errorf("expected stderr to report why the placeholder fails the real selection gate, got: %s", stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"signature verified: true",
		"selection_status: PLACEHOLDER_UNVERIFIED",
		"satisfies real provider selection gate: false",
		"faults: 9",
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

func TestRunFailsOnMissingTopology(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-topology", repoRoot + "/definitions/planning/gates/does-not-exist.yaml"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected an error for a missing topology file, got nil")
	}
	if !strings.Contains(err.Error(), "loading") {
		t.Errorf("expected a loading error, got: %v", err)
	}
}

// writeStructurallyBrokenTopology writes a topology missing several RED
// elements (no operations, no faults, no signature) to a temp file, so
// run() must report multiple violations and never reach the signature
// branch.
func writeStructurallyBrokenTopology(t *testing.T) string {
	t.Helper()
	tp := pilotprovider.ProviderTopology{
		SchemaVersion:   1,
		TodoID:          "SELECT-002",
		SignedDate:      "2026-09-13",
		SelectionStatus: pilotprovider.StatusPlaceholderUnverified,
		Provider: pilotprovider.Provider{
			VendorID: "broken-fixture-PLACEHOLDER-UNVERIFIED",
		},
	}
	b, err := yaml.Marshal(tp)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "topology.yaml")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestRunReportsMultipleStructuralViolations(t *testing.T) {
	path := writeStructurallyBrokenTopology(t)
	var stdout, stderr bytes.Buffer
	err := run([]string{"-topology", path}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected an error: the fixture topology is deliberately incomplete")
	}
	if !strings.Contains(stderr.String(), "VIOLATION: operations:") {
		t.Errorf("expected stderr to report the missing operations, got: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "VIOLATION: signature:") {
		t.Errorf("expected stderr to report the missing signature, got: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "signature verified: false") {
		t.Errorf("expected stdout to report signature verified: false, got: %s", stdout.String())
	}
}
