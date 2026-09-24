package sbom_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/sbom"
)

func TestValidateArtifactDigestBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release.exe")
	artifact := []byte("release artifact bytes")
	if err := os.WriteFile(path, artifact, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(artifact)
	digest := hex.EncodeToString(sum[:])
	doc := &sbom.Document{Metadata: sbom.Metadata{Component: sbom.Component{
		Name:   sbom.RootModulePath,
		Hashes: []sbom.Hash{{Alg: sbom.HashAlgSHA256, Content: digest}},
	}}}

	if err := sbom.ValidateArtifactDigestBinding(doc, path, digest); err != nil {
		t.Fatalf("ValidateArtifactDigestBinding(valid evidence): %v", err)
	}
	if err := sbom.ValidateArtifactDigestBinding(doc, path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("ValidateArtifactDigestBinding accepted a different expected digest")
	}
	if err := os.WriteFile(path, []byte("modified artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sbom.ValidateArtifactDigestBinding(doc, path, digest); err == nil {
		t.Fatal("ValidateArtifactDigestBinding accepted artifact bytes that differ from the expected digest")
	}
}

func TestValidateRootHashRequiresOneExactRootHash(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for name, hashes := range map[string][]sbom.Hash{
		"missing": nil,
		"wrong":   {{Alg: sbom.HashAlgSHA256, Content: strings.Repeat("b", 64)}},
		"duplicate": {
			{Alg: sbom.HashAlgSHA256, Content: digest},
			{Alg: sbom.HashAlgSHA256, Content: digest},
		},
	} {
		t.Run(name, func(t *testing.T) {
			doc := &sbom.Document{Metadata: sbom.Metadata{Component: sbom.Component{Hashes: hashes}}}
			if err := sbom.ValidateRootHash(doc, digest); err == nil {
				t.Fatalf("accepted %s root hash evidence", name)
			}
		})
	}
	if err := sbom.ValidateRootHash(&sbom.Document{}, strings.ToUpper(digest)); err == nil {
		t.Fatal("accepted non-canonical expected digest")
	}
}

func TestGenerateBindsArtifactHashToCycloneDXRoot(t *testing.T) {
	root := repopath.RootDir()
	path := filepath.Join(t.TempDir(), "release.exe")
	artifact := []byte("release artifact")
	if err := os.WriteFile(path, artifact, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(artifact)
	want := hex.EncodeToString(sum[:])
	doc, err := sbom.Generate(root, sbom.Options{ArtifactPath: path})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := sbom.ValidateRootHash(doc, want); err != nil {
		t.Fatalf("generated SBOM is not bound to artifact bytes: %v", err)
	}
}

func TestValidateArtifactDigestBindingDoesNotClaimBOMCompleteness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release.exe")
	artifact := []byte("release artifact bytes")
	if err := os.WriteFile(path, artifact, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(artifact)
	digest := hex.EncodeToString(sum[:])
	doc := &sbom.Document{Metadata: sbom.Metadata{Component: sbom.Component{
		Hashes: []sbom.Hash{{Alg: sbom.HashAlgSHA256, Content: digest}},
	}}}
	if err := sbom.ValidateArtifactDigestBinding(doc, path, digest); err != nil {
		t.Fatalf("digest-only check should accept matching hashes without asserting completeness: %v", err)
	}
}
