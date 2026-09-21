package release

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTodo_CICD_003(t *testing.T) {
	root := releaseRepoRoot(t)
	fixture := releaseFixture(t, root)
	out := filepath.Join(t.TempDir(), "bundle")
	manifest, err := Build(root, fixture.options(out))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	digest, err := manifest.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	receipt, err := VerifyBundle(out, VerifyOptions{})
	if err != nil {
		t.Fatalf("VerifyBundle: %v", err)
	}
	if receipt.ManifestDigest != digest {
		t.Fatalf("verified digest = %s, built digest = %s", receipt.ManifestDigest, digest)
	}

	changed := filepath.Join(out, "policy", "driftgate.json")
	if err := os.WriteFile(changed, []byte("altered\n"), 0o644); err != nil {
		t.Fatalf("alter policy report: %v", err)
	}
	_, err = VerifyBundle(out, VerifyOptions{})
	if err == nil || !strings.Contains(err.Error(), `policy/driftgate.json`) {
		t.Fatalf("altered artifact error = %v, want named policy/driftgate.json", err)
	}
}

func TestTodo_CICD_003_Golden(t *testing.T) {
	root := releaseRepoRoot(t)
	fixture := releaseFixture(t, root)
	firstOut := filepath.Join(t.TempDir(), "first")
	secondOut := filepath.Join(t.TempDir(), "second")
	first, err := Build(root, fixture.options(firstOut))
	if err != nil {
		t.Fatalf("first Build: %v", err)
	}
	second, err := Build(root, fixture.options(secondOut))
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}
	firstDigest, err := first.CanonicalDigest()
	if err != nil {
		t.Fatalf("first CanonicalDigest: %v", err)
	}
	secondDigest, err := second.CanonicalDigest()
	if err != nil {
		t.Fatalf("second CanonicalDigest: %v", err)
	}
	if firstDigest != secondDigest {
		t.Fatalf("manifest digest changed for identical inputs: %s != %s", firstDigest, secondDigest)
	}
	firstBytes, err := os.ReadFile(filepath.Join(firstOut, ManifestFileName))
	if err != nil {
		t.Fatalf("read first manifest: %v", err)
	}
	secondBytes, err := os.ReadFile(filepath.Join(secondOut, ManifestFileName))
	if err != nil {
		t.Fatalf("read second manifest: %v", err)
	}
	if string(firstBytes) != string(secondBytes) {
		t.Fatal("identical inputs produced different manifest bytes")
	}
	if len(first.Artifacts) < 10 {
		t.Fatalf("artifact count = %d, want binaries, digest list, evidence, four policies and supply-chain artifacts", len(first.Artifacts))
	}
}

func TestTodo_CICD_003_Race(t *testing.T) {
	root := releaseRepoRoot(t)
	fixture := releaseFixture(t, root)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			out := filepath.Join(t.TempDir(), "bundle")
			_, err := Build(root, fixture.options(out))
			if err == nil {
				_, err = VerifyBundle(out, VerifyOptions{})
			}
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent build/verify: %v", err)
	}
}

type releaseFixtureInputs struct {
	root string
	bin  string
	ver  string
	pol  map[string]string
	gate map[string]string
}

func (f releaseFixtureInputs) options(out string) Options {
	return Options{
		Out:                 out,
		VersionFile:         f.ver,
		Binaries:            []BinaryInput{{Name: "hcmnext.exe", Path: f.bin}},
		SBOMPath:            filepath.Join(f.root, "definitions", "supply-chain", "sbom.cdx.json"),
		ProvenancePath:      filepath.Join(f.root, "definitions", "supply-chain", "provenance.json"),
		P1AEvidencePath:     filepath.Join(f.root, "definitions", "planning", "gates", "p1a-evidence-report.json"),
		PolicyReports:       f.pol,
		ProductGateEvidence: f.gate,
		KeyPath:             filepath.Join(f.root, DefaultKeyPath),
	}
}

func releaseFixture(t *testing.T, root string) releaseFixtureInputs {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "hcmnext.exe")
	if err := os.WriteFile(bin, []byte("deterministic binary fixture\n"), 0o755); err != nil {
		t.Fatalf("write binary fixture: %v", err)
	}
	version := filepath.Join(dir, "VERSION")
	if err := os.WriteFile(version, []byte("2026.09.05\n"), 0o644); err != nil {
		t.Fatalf("write version fixture: %v", err)
	}
	policies := make(map[string]string, len(RequiredPolicyReports))
	for _, name := range RequiredPolicyReports {
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, []byte("{\"policy\":\""+name+"\",\"passed\":true}\n"), 0o644); err != nil {
			t.Fatalf("write %s fixture: %v", name, err)
		}
		policies[name] = path
	}
	gate := make(map[string]string, len(ProductGateGates()))
	for _, name := range ProductGateGates() {
		gate[name] = "fixture-digest-" + name
	}
	return releaseFixtureInputs{root: root, bin: bin, ver: version, pol: policies, gate: gate}
}

func releaseRepoRoot(t *testing.T) string {
	t.Helper()
	working, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	root, err := filepath.Abs(filepath.Join(working, "..", "..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	return root
}
