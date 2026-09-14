package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunSignedBlueprint exercises run() end to end against the checked-in
// gate files. The template's own BLOCKED readiness report is not a failure
// condition (see the package doc comment in main.go).
func TestRunSignedBlueprint(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	var out, errBuf bytes.Buffer
	err := run([]string{
		"-blueprint", filepath.Join(root, "definitions/planning/gates/customer-001-pilot-blueprint.yaml"),
		"-provider-topology", filepath.Join(root, "definitions/planning/gates/select-002-provider-topology.yaml"),
		"-jurisdiction-profile", filepath.Join(root, "definitions/planning/gates/select-001-jurisdiction-profile.yaml"),
	}, &out, &errBuf)
	if err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, errBuf.String())
	}
	for _, want := range []string{"CUSTOMER-001 pilot blueprint", "canonical_digest:", "signature verified: true", "workstreams: 10", "overall: BLOCKED"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, out.String())
		}
	}
}

func TestRunMissingBlueprintFails(t *testing.T) {
	var out, errBuf bytes.Buffer
	err := run([]string{"-blueprint", "does-not-exist.yaml"}, &out, &errBuf)
	if err == nil {
		t.Fatal("run must fail on a missing blueprint file")
	}
	if !strings.Contains(err.Error(), "does-not-exist.yaml") {
		t.Fatalf("error must name the missing path, got: %v", err)
	}
}
