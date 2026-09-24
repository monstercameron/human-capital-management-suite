package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/releaseboundary"
)

func TestTodo_TOOL_015_Integration(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "release.sbom.json")
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-root", root, "-binary", binary, "-out", out}); err != nil {
		t.Fatalf("run release boundary against built Go binary: %v", err)
	}
	if err := releaseboundary.CheckFile(out); err != nil {
		t.Fatalf("written artifact SBOM was not valid: %v", err)
	}
}

func TestRunRequiresArtifactAndOutput(t *testing.T) {
	if err := run(nil); err == nil {
		t.Fatal("missing artifact and output accepted")
	}
}
