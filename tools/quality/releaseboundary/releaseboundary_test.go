package releaseboundary_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/releaseboundary"
	"github.com/monstercameron/human-capital-management-suite/tools/quality/sbom"
)

func document(component string) sbom.Document {
	return sbom.Document{
		Schema:     sbom.Schema,
		Generator:  sbom.Generator{Name: "hcmnext-sbom", Version: "test", Go: "go1.26.3"},
		Graph:      sbom.GraphSource{Tool: "go version -m", Version: "go1.26.3"},
		Subject:    sbom.Subject{Name: "hcmnext-api", Digest: "sha256:" + strings.Repeat("a", 64)},
		Components: []sbom.Component{{Type: "go-module", Name: "example.invalid/app", Version: "(devel)", Hash: "sha256:" + strings.Repeat("b", 64), SourceDigest: "sha256:" + strings.Repeat("c", 64), License: "MIT", Main: true}, {Type: "go-module", Name: component, Version: "v1.0.0", Hash: "h1:" + strings.Repeat("A", 44), SourceDigest: "sha256:" + strings.Repeat("d", 64), License: "MIT"}},
	}
}

// TestReleaseContainsNoLegacyRuntime is TOOL-015's primary boundary test.
func TestReleaseContainsNoLegacyRuntime(t *testing.T) {
	for _, name := range []string{"github.com/acme/telemetry/v2", "google.golang.org/grpc", "github.com/acme/reactive-streams"} {
		if err := releaseboundary.Check(document(name)); err != nil {
			t.Errorf("allowed Go component %q rejected: %v", name, err)
		}
	}
	for _, name := range []string{"github.com/acme/node-runtime", "github.com/acme/nodeRuntime", "github.com/acme/typescript-runtime", "github.com/acme/react-renderer", "github.com/acme/reactRuntime", "github.com/acme/reactjs", "github.com/acme/vite-runtime"} {
		if err := releaseboundary.Check(document(name)); err == nil {
			t.Errorf("excluded component %q accepted", name)
		}
	}
}

func TestReleaseBoundaryChecksSubjectAndReplacement(t *testing.T) {
	d := document("github.com/acme/telemetry")
	d.Subject.Name = "node-worker"
	if err := releaseboundary.Check(d); err == nil {
		t.Fatal("excluded runtime in SBOM subject accepted")
	}

	d = document("github.com/acme/telemetry")
	d.Components[1].Replacement = &sbom.Replacement{Name: "github.com/acme/react-runtime", Version: "v1.0.0"}
	if err := releaseboundary.Check(d); err == nil {
		t.Fatal("excluded runtime in replacement module accepted")
	}
}

func TestTodo_TOOL_015_Golden(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release.sbom.json")
	d := document("google.golang.org/grpc")
	data, err := d.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := releaseboundary.CheckFile(path); err != nil {
		t.Fatalf("valid SBOM rejected: %v", err)
	}
	bad := document("npm")
	data, err = bad.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := releaseboundary.CheckFile(path); err == nil {
		t.Fatal("excluded SBOM accepted")
	}
}

// TestTodo_TOOL_015_Integration proves the public file boundary is enforced
// when a release SBOM is supplied by the build pipeline.
func TestTodo_TOOL_015_Integration(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	d, err := sbom.Generate("", binary)
	if err != nil {
		t.Fatalf("generate SBOM from actual Go build metadata: %v", err)
	}
	if err := releaseboundary.CheckArtifact(d, binary); err != nil {
		t.Fatalf("actual Go artifact rejected: %v", err)
	}
	for i := range d.Components {
		if d.Components[i].Main {
			d.Components[i].Hash = "sha256:" + strings.Repeat("0", 64)
		}
	}
	if err := releaseboundary.CheckArtifact(d, binary); err == nil {
		t.Fatal("main component hash unrelated to the subject was accepted")
	}
	d, err = sbom.Generate("", binary)
	if err != nil {
		t.Fatalf("regenerate SBOM for subject mismatch case: %v", err)
	}
	d.Subject.Digest = "sha256:" + strings.Repeat("0", 64)
	if err := releaseboundary.CheckArtifact(d, binary); err == nil {
		t.Fatal("SBOM with a subject unrelated to the binary was accepted")
	}
}

func TestCheckFileErrors(t *testing.T) {
	if err := releaseboundary.CheckFile(""); err == nil {
		t.Fatal("empty path accepted")
	}
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := releaseboundary.CheckFile(path); err == nil {
		t.Fatal("malformed SBOM accepted")
	}
}
