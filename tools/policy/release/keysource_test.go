package release_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/provenance"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/release"
)

type releaseCustodyFake struct {
	keys map[string]ed25519.PrivateKey
}

func (f *releaseCustodyFake) Encrypt(custody.Context, custody.Handle, []byte) (custody.Ciphertext, custody.Receipt, error) {
	return custody.Ciphertext{}, custody.Receipt{}, errors.New("unused")
}

func (f *releaseCustodyFake) Decrypt(custody.Context, custody.Handle, custody.Ciphertext) ([]byte, custody.Receipt, error) {
	return nil, custody.Receipt{}, errors.New("unused")
}

func (f *releaseCustodyFake) Sign(ctx custody.Context, handle custody.Handle, message []byte) (custody.Signature, custody.Receipt, error) {
	private, ok := f.keys[handle.ID+":"+handle.Version]
	if !ok {
		return custody.Signature{}, custody.Receipt{}, fmt.Errorf("missing custody key %s/%s", handle.ID, handle.Version)
	}
	return custody.Signature{Handle: handle, Algorithm: "ed25519", Data: ed25519.Sign(private, message)}, custody.Receipt{ID: "sign", Handle: handle, Operation: custody.Sign, At: time.Now().UTC()}, nil
}

func (f *releaseCustodyFake) Verify(custody.Context, custody.Handle, []byte, custody.Signature) (bool, custody.Receipt, error) {
	return false, custody.Receipt{}, errors.New("unused")
}

func (f *releaseCustodyFake) IssueLease(custody.Context, custody.Handle, custody.Operation, time.Duration) (custody.Lease, error) {
	return custody.Lease{}, errors.New("unused")
}

func (f *releaseCustodyFake) RenewLease(custody.Context, custody.Lease, time.Duration) (custody.Lease, error) {
	return custody.Lease{}, errors.New("unused")
}

func (f *releaseCustodyFake) Rotate(custody.Context, custody.Handle) (custody.Handle, custody.Receipt, error) {
	return custody.Handle{}, custody.Receipt{}, errors.New("unused")
}

func (f *releaseCustodyFake) Revoke(custody.Context, custody.Handle, string) (custody.Receipt, error) {
	return custody.Receipt{}, errors.New("unused")
}

var _ custody.Provider = (*releaseCustodyFake)(nil)

func releaseSigningContext() custody.Context {
	return custody.Context{RequestContext: custody.RequestContext{Workload: "release-builder", Tenant: "release", Region: "us-east-1", Purpose: "release-signing", Destination: "release-admission"}}
}

func releaseSigningHandle(version string) custody.Handle {
	return custody.Handle{ID: "release-key", Kind: custody.Key, Version: version, Tenant: "release", Region: "us-east-1"}
}

func releaseStatement() provenance.Statement {
	return provenance.Statement{
		SchemaVersion: provenance.SchemaVersion,
		PredicateType: provenance.PredicateType,
		GeneratedAt:   "2026-09-05T00:00:00Z",
		Subjects:      []provenance.Subject{{Name: "hcmnext", SHA256: strings.Repeat("a", 64)}},
		Builder:       provenance.Builder{ID: provenance.BuilderID},
		Source:        provenance.SourceRef{Repository: provenance.RootModulePath, Ref: "main", Commit: "commit"},
		BuildConfig:   provenance.BuildConfig{GoVersion: "go1.26.3", GOOS: "windows", GOARCH: "arm64", ConfigDigest: "config"},
		SBOM:          provenance.SBOMReference{Path: "sbom.json", SHA256: strings.Repeat("b", 64)},
	}
}

type releaseInputs struct {
	root    string
	version string
	bin     string
	sbom    string
	prov    string
	p1a     string
	pol     map[string]string
	gate    map[string]string
}

func newReleaseInputs(t *testing.T) releaseInputs {
	t.Helper()
	root := t.TempDir()
	inputs := releaseInputs{root: root, version: filepath.Join(root, "VERSION"), bin: filepath.Join(root, "hcmnext.exe"), sbom: filepath.Join(root, "sbom.json"), prov: filepath.Join(root, "provenance.json"), p1a: filepath.Join(root, "p1a.json"), pol: make(map[string]string)}
	for path, data := range map[string]string{inputs.version: "2026.09.05\n", inputs.bin: "binary\n", inputs.sbom: "{\"sbom\":true}\n", inputs.p1a: "{\"p1a\":true}\n"} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	for _, name := range release.RequiredPolicyReports {
		path := filepath.Join(root, name+".json")
		if err := os.WriteFile(path, []byte("{\"policy\":\""+name+"\"}\n"), 0o644); err != nil {
			t.Fatalf("write policy %s: %v", name, err)
		}
		inputs.pol[name] = path
	}
	inputs.gate = make(map[string]string, len(release.ProductGateGates()))
	for _, name := range release.ProductGateGates() {
		inputs.gate[name] = "fixture-digest-" + name
	}
	return inputs
}

func (in releaseInputs) options(out string) release.Options {
	return release.Options{Out: out, VersionFile: in.version, Binaries: []release.BinaryInput{{Name: "hcmnext.exe", Path: in.bin}}, SBOMPath: in.sbom, ProvenancePath: in.prov, P1AEvidencePath: in.p1a, PolicyReports: in.pol, ProductGateEvidence: in.gate}
}

func custodySource(t *testing.T, provider custody.Provider, version string, private ed25519.PrivateKey) release.KeySource {
	t.Helper()
	source, err := release.NewCustodyKeySource(provider, releaseSigningContext(), releaseSigningHandle(version), fmt.Sprintf("%x", private.Public()))
	if err != nil {
		t.Fatalf("NewCustodyKeySource: %v", err)
	}
	return source
}

func writeSignedReleaseStatement(t *testing.T, inputs releaseInputs, source provenance.KeySource) {
	t.Helper()
	sbom, err := os.ReadFile(inputs.sbom)
	if err != nil {
		t.Fatalf("read SBOM: %v", err)
	}
	base := releaseStatement()
	sum := sha256.Sum256(sbom)
	base.SBOM.SHA256 = hex.EncodeToString(sum[:])
	signed, err := provenance.SignStatementWithKeySource(source, base, "custody")
	if err != nil {
		t.Fatalf("SignStatementWithKeySource: %v", err)
	}
	if err := provenance.WriteStatement(inputs.prov, signed); err != nil {
		t.Fatalf("WriteStatement: %v", err)
	}
}

func TestTodo_SECARCH_005_Integration(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 31
	private := ed25519.NewKeyFromSeed(seed)
	provider := &releaseCustodyFake{keys: map[string]ed25519.PrivateKey{"release-key:v1": private}}
	source := custodySource(t, provider, "v1", private)
	inputs := newReleaseInputs(t)
	writeSignedReleaseStatement(t, inputs, source)
	out := filepath.Join(inputs.root, "bundle")
	if _, err := release.BuildWithKeySource(inputs.root, inputs.options(out), source); err != nil {
		t.Fatalf("BuildWithKeySource: %v", err)
	}
	if _, err := release.VerifyBundle(out, release.VerifyOptions{TrustedPublicKeys: map[string]bool{source.PublicKey(): true}}); err != nil {
		t.Fatalf("VerifyBundle: %v", err)
	}
}

func TestTodo_SECARCH_005_Rotation(t *testing.T) {
	firstSeed := make([]byte, ed25519.SeedSize)
	firstSeed[0] = 32
	secondSeed := make([]byte, ed25519.SeedSize)
	secondSeed[0] = 33
	firstPrivate := ed25519.NewKeyFromSeed(firstSeed)
	secondPrivate := ed25519.NewKeyFromSeed(secondSeed)
	provider := &releaseCustodyFake{keys: map[string]ed25519.PrivateKey{"release-key:v1": firstPrivate, "release-key:v2": secondPrivate}}
	inputs := newReleaseInputs(t)
	first := custodySource(t, provider, "v1", firstPrivate)
	writeSignedReleaseStatement(t, inputs, first)
	firstOut := filepath.Join(inputs.root, "bundle-v1")
	if _, err := release.BuildWithKeySource(inputs.root, inputs.options(firstOut), first); err != nil {
		t.Fatalf("BuildWithKeySource(v1): %v", err)
	}
	if _, err := release.VerifyBundle(firstOut, release.VerifyOptions{TrustedPublicKeys: map[string]bool{first.PublicKey(): true}}); err != nil {
		t.Fatalf("VerifyBundle(v1): %v", err)
	}
	second := custodySource(t, provider, "v2", secondPrivate)
	writeSignedReleaseStatement(t, inputs, second)
	secondOut := filepath.Join(inputs.root, "bundle-v2")
	if _, err := release.BuildWithKeySource(inputs.root, inputs.options(secondOut), second); err != nil {
		t.Fatalf("BuildWithKeySource(v2): %v", err)
	}
	if _, err := release.VerifyBundle(secondOut, release.VerifyOptions{TrustedPublicKeys: map[string]bool{second.PublicKey(): true}}); err != nil {
		t.Fatalf("VerifyBundle(v2): %v", err)
	}
}

func TestTodo_SECARCH_005_Mutation(t *testing.T) {
	if _, err := release.BuildWithKeySource(t.TempDir(), release.Options{}, nil); err == nil || !strings.Contains(err.Error(), "key source") {
		t.Fatalf("nil source error = %v, want typed key source refusal", err)
	}
}
