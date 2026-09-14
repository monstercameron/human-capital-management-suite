package main

import (
	"bytes"
	"strings"
	"testing"
)

// repoRoot is the path from this command's directory to the repository
// root: tools/planning/cmd/scopeceiling is four levels below root.
const repoRoot = "../../../.."

func TestRunReportsCleanManifest(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-manifest", repoRoot + "/definitions/planning/gates/phase1-scope-ceiling.yaml"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run() = %v, stderr: %s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("expected no stderr output for a clean manifest, got: %s", stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"signature verified: true", "intents: 14", "capabilities: 10", "selection slots:", "provider: filled=false"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q; got:\n%s", want, out)
		}
	}
}

func TestRunFailsOnMissingManifest(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-manifest", repoRoot + "/definitions/planning/gates/does-not-exist.yaml"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected an error for a missing manifest, got nil")
	}
	if !strings.Contains(err.Error(), "loading") {
		t.Errorf("expected a loading error, got: %v", err)
	}
}
