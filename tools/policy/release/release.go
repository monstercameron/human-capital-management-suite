// Package release builds and verifies offline, immutable release bundles.
//
// The bundle is deliberately a filesystem artifact rather than a registry
// client: every input is copied into a deterministic layout, the manifest
// records the sha256 of every copied artifact, and the manifest digest is
// signed with the existing gateevidence Ed25519 fixture convention.
package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/provenance"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/releaseadmission"
)

const (
	// SchemaVersion is the release bundle manifest schema revision.
	SchemaVersion = 1
	// ManifestFileName is the signed manifest at the root of a bundle.
	ManifestFileName = "manifest.json"
	// DefaultVersionFile is the declared version marker used when -version is
	// not supplied.
	DefaultVersionFile = "VERSION"
	// DefaultKeyPath is the existing development signing fixture. It is a
	// fixture only and must be replaced by a production signing ceremony.
	DefaultKeyPath = "tools/planning/gateevidence/testdata/dev-signing-key.yaml"
	// DevFixturePublicKey is the public half of DefaultKeyPath. Keeping the
	// trust anchor here makes VerifyBundle work after a bundle is moved to an
	// offline verifier that does not have the source checkout.
	DevFixturePublicKey = "93019e7b15fc44dbfb7f36e105e465486e5d5a6ddec109abda44507e0d45332b"
)

// RequiredPolicyReports are the four CICD-001 policy outputs required in a
// release bundle.
var RequiredPolicyReports = []string{"driftgate", "apigate", "substratecoverage", "cleancheckout"}

// BinaryInput identifies one built binary to copy into the bundle.
type BinaryInput struct {
	Name string
	Path string
}

// Options configures Build. Paths other than Out are relative to root unless
// absolute. PolicyReports uses the keys in RequiredPolicyReports.
type Options struct {
	Out                 string
	Version             string
	VersionFile         string
	Binaries            []BinaryInput
	SBOMPath            string
	ProvenancePath      string
	P1AEvidencePath     string
	PolicyReports       map[string]string
	ProductGateEvidence map[string]string
	KeyPath             string
}

// Artifact is one immutable file covered by a bundle manifest.
type Artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Manifest is the signed release manifest. Signature is excluded from the
// canonical digest, so signing never creates a circular input.
type Manifest struct {
	SchemaVersion int                     `json:"schema_version"`
	Version       string                  `json:"version"`
	Artifacts     []Artifact              `json:"artifacts"`
	ProductGate   *ProductGateRecord      `json:"product_gate,omitempty"`
	Signature     *gateevidence.Signature `json:"signature,omitempty"`
}

// Verification is the successful offline verification receipt.
type Verification struct {
	ManifestDigest string
	Version        string
	Artifacts      []string
}

// VerifyOptions configures offline verification. A nil trust set uses the
// pinned development fixture public key, while a non-nil set is an explicit
// trust set for a separately retained signing ceremony.
type VerifyOptions struct {
	TrustedPublicKeys map[string]bool
}

// Version returns the release package contract revision.
func Version() int { return SchemaVersion }

// Explain returns a concise, audit-safe description of a verified bundle.
func (v Verification) Explain() string {
	return fmt.Sprintf("release bundle verified (version %s, manifest %s, %d artifacts)", v.Version, v.ManifestDigest, len(v.Artifacts))
}

// Build assembles and signs an immutable bundle at opts.Out. An existing
// output directory containing manifest.json is never overwritten.
func Build(root string, opts Options) (Manifest, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Manifest{}, fmt.Errorf("release: resolve root: %w", err)
	}
	if strings.TrimSpace(opts.Out) == "" {
		return Manifest{}, fmt.Errorf("release: output directory is required")
	}
	if len(opts.Binaries) == 0 {
		return Manifest{}, fmt.Errorf("release: at least one binary is required")
	}
	version, err := declaredVersion(root, opts)
	if err != nil {
		return Manifest{}, err
	}
	inputs, err := normalizedInputs(root, opts)
	if err != nil {
		return Manifest{}, err
	}
	keyPath := rooted(root, opts.KeyPath)
	if opts.KeyPath == "" {
		keyPath = rooted(root, DefaultKeyPath)
	}
	privateKey, err := provenance.LoadSigningKeyFixture(keyPath)
	if err != nil {
		return Manifest{}, err
	}
	if err := validateProvenance(inputs.sbom, inputs.provenance, privateKey); err != nil {
		return Manifest{}, err
	}

	out := rooted(root, opts.Out)
	parent := filepath.Dir(out)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Manifest{}, fmt.Errorf("release: create output parent: %w", err)
	}
	if _, err := os.Stat(filepath.Join(out, ManifestFileName)); err == nil {
		return Manifest{}, fmt.Errorf("release: output %s is immutable and already contains %s", out, ManifestFileName)
	} else if !os.IsNotExist(err) {
		return Manifest{}, fmt.Errorf("release: inspect output: %w", err)
	}
	stage, err := os.MkdirTemp(parent, ".release-staging-")
	if err != nil {
		return Manifest{}, fmt.Errorf("release: create staging directory: %w", err)
	}
	defer os.RemoveAll(stage)

	if err := writeBundleFiles(stage, version, inputs); err != nil {
		return Manifest{}, err
	}
	manifest, err := manifestFor(stage, version, inputs.gate)
	if err != nil {
		return Manifest{}, err
	}
	digest, err := manifest.CanonicalDigest()
	if err != nil {
		return Manifest{}, err
	}
	sig, err := gateevidence.SignDigest(privateKey, digest)
	if err != nil {
		return Manifest{}, fmt.Errorf("release: sign manifest: %w", err)
	}
	actualPublicKey := DevFixturePublicKey
	if got := hex.EncodeToString(privateKey[32:]); got != actualPublicKey {
		return Manifest{}, fmt.Errorf("release: signing key public key %s is not the gateevidence dev fixture", got)
	}
	fixtureName := opts.KeyPath
	if fixtureName == "" {
		fixtureName = DefaultKeyPath
	}
	manifest.Signature = &gateevidence.Signature{
		Algorithm:  "ed25519",
		PublicKey:  actualPublicKey,
		Value:      sig,
		KeyFixture: fixtureName,
	}
	if err := writeManifest(filepath.Join(stage, ManifestFileName), manifest); err != nil {
		return Manifest{}, err
	}
	if err := os.Rename(stage, out); err != nil {
		return Manifest{}, fmt.Errorf("release: publish immutable bundle: %w", err)
	}
	return manifest, nil
}

// VerifyBundle verifies a bundle without network access or source checkout.
// Every missing, extra, or modified artifact is named in the returned error.
func VerifyBundle(bundle string, opts VerifyOptions) (Verification, error) {
	manifestPath := filepath.Join(bundle, ManifestFileName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return Verification{}, fmt.Errorf("release: read %s: %w", ManifestFileName, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Verification{}, fmt.Errorf("release: parse %s: %w", ManifestFileName, err)
	}
	if manifest.SchemaVersion != SchemaVersion {
		return Verification{}, fmt.Errorf("release: unsupported manifest schema_version %d", manifest.SchemaVersion)
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return Verification{}, fmt.Errorf("release: manifest version is empty")
	}
	trusted := opts.TrustedPublicKeys
	if len(trusted) == 0 {
		trusted = map[string]bool{DevFixturePublicKey: true}
	}
	if manifest.Signature == nil || !trusted[manifest.Signature.PublicKey] {
		return Verification{}, fmt.Errorf("release: manifest signer %q is not trusted", signatureKey(manifest))
	}
	if manifest.Signature.Algorithm != "ed25519" {
		return Verification{}, fmt.Errorf("release: unsupported manifest signature algorithm %q", manifest.Signature.Algorithm)
	}
	digest, err := manifest.CanonicalDigest()
	if err != nil {
		return Verification{}, err
	}
	ok, err := gateevidence.VerifyDigestSignature(manifest.Signature.PublicKey, digest, manifest.Signature.Value)
	if err != nil {
		return Verification{}, fmt.Errorf("release: manifest signature: %w", err)
	}
	if !ok {
		return Verification{}, fmt.Errorf("release: manifest signature does not verify")
	}
	if err := validateArtifactEntries(manifest.Artifacts); err != nil {
		return Verification{}, err
	}
	if err := requireBundleArtifacts(manifest.Artifacts); err != nil {
		return Verification{}, err
	}
	if err := verifyFiles(bundle, manifest.Artifacts); err != nil {
		return Verification{}, err
	}
	if err := verifyBinaryDigestList(bundle, manifest.Artifacts); err != nil {
		return Verification{}, err
	}
	if err := verifyProvenanceInBundle(bundle, manifest, trusted); err != nil {
		return Verification{}, err
	}
	if err := VerifyProductGateRecord(manifest.ProductGate); err != nil {
		return Verification{}, err
	}
	digest, err = manifest.CanonicalDigest()
	if err != nil {
		return Verification{}, err
	}
	paths := make([]string, 0, len(manifest.Artifacts))
	for _, artifact := range manifest.Artifacts {
		paths = append(paths, artifact.Path)
	}
	return Verification{ManifestDigest: digest, Version: manifest.Version, Artifacts: paths}, nil
}

// Verify is the short-form verifier using the pinned development trust set.
func Verify(bundle string) error {
	_, err := VerifyBundle(bundle, VerifyOptions{})
	return err
}

func (m Manifest) CanonicalDigest() (string, error) {
	payload := struct {
		SchemaVersion int                `json:"schema_version"`
		Version       string             `json:"version"`
		Artifacts     []Artifact         `json:"artifacts"`
		ProductGate   *ProductGateRecord `json:"product_gate,omitempty"`
	}{m.SchemaVersion, m.Version, m.Artifacts, m.ProductGate}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("release: marshal canonical manifest: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

type normalizedInputSet struct {
	sbom       string
	provenance string
	p1a        string
	policies   map[string]string
	binaries   []BinaryInput
	gate       ProductGateRecord
	gateJSON   []byte
}

func normalizedInputs(root string, opts Options) (normalizedInputSet, error) {
	// The product-slice release gate decides the bundle before any artifact
	// is copied: a bundle missing one of the eight gate proofs is refused.
	decision, evidence, err := AdmitProductGate(opts.ProductGateEvidence)
	if err != nil {
		return normalizedInputSet{}, err
	}
	record := SealProductGate(decision, evidence)
	gateJSON, err := MarshalProductGate(record)
	if err != nil {
		return normalizedInputSet{}, err
	}
	set := normalizedInputSet{
		gate:       record,
		gateJSON:   gateJSON,
		sbom:       rooted(root, defaultString(opts.SBOMPath, "definitions/supply-chain/sbom.cdx.json")),
		provenance: rooted(root, defaultString(opts.ProvenancePath, "definitions/supply-chain/provenance.json")),
		p1a:        rooted(root, defaultString(opts.P1AEvidencePath, "definitions/planning/gates/p1a-evidence-report.json")),
		policies:   make(map[string]string, len(RequiredPolicyReports)),
	}
	if err := requireReadable(set.sbom, "SBOM"); err != nil {
		return normalizedInputSet{}, err
	}
	if err := requireReadable(set.provenance, "provenance"); err != nil {
		return normalizedInputSet{}, err
	}
	if err := requireReadable(set.p1a, "P1A evidence report"); err != nil {
		return normalizedInputSet{}, err
	}
	for _, name := range RequiredPolicyReports {
		path := opts.PolicyReports[name]
		if path == "" {
			return normalizedInputSet{}, fmt.Errorf("release: policy report %q is required", name)
		}
		path = rooted(root, path)
		if err := requireReadable(path, name+" policy report"); err != nil {
			return normalizedInputSet{}, err
		}
		set.policies[name] = path
	}
	seen := make(map[string]bool, len(opts.Binaries))
	for _, binary := range opts.Binaries {
		name := filepath.ToSlash(strings.TrimSpace(binary.Name))
		if err := validateLeaf(name, "binary name"); err != nil {
			return normalizedInputSet{}, err
		}
		if seen[name] {
			return normalizedInputSet{}, fmt.Errorf("release: duplicate binary name %q", name)
		}
		seen[name] = true
		path := rooted(root, binary.Path)
		if err := requireReadable(path, "binary "+name); err != nil {
			return normalizedInputSet{}, err
		}
		set.binaries = append(set.binaries, BinaryInput{Name: name, Path: path})
	}
	sort.Slice(set.binaries, func(i, j int) bool { return set.binaries[i].Name < set.binaries[j].Name })
	return set, nil
}

func writeBundleFiles(stage, version string, inputs normalizedInputSet) error {
	if err := writeText(filepath.Join(stage, "version.txt"), version+"\n"); err != nil {
		return err
	}
	if err := copyFile(inputs.sbom, filepath.Join(stage, "sbom.cdx.json")); err != nil {
		return fmt.Errorf("release: copy SBOM: %w", err)
	}
	if err := copyFile(inputs.provenance, filepath.Join(stage, "provenance.json")); err != nil {
		return fmt.Errorf("release: copy provenance: %w", err)
	}
	if err := copyFile(inputs.p1a, filepath.Join(stage, "p1a-evidence-report.json")); err != nil {
		return fmt.Errorf("release: copy P1A evidence report: %w", err)
	}
	var digestLines strings.Builder
	for _, binary := range inputs.binaries {
		digest, err := fileDigest(binary.Path)
		if err != nil {
			return fmt.Errorf("release: hash binary %s: %w", binary.Name, err)
		}
		rel := filepath.ToSlash(filepath.Join("binaries", binary.Name))
		if err := copyFile(binary.Path, filepath.Join(stage, filepath.FromSlash(rel))); err != nil {
			return fmt.Errorf("release: copy binary %s: %w", binary.Name, err)
		}
		fmt.Fprintf(&digestLines, "%s  %s\n", digest, rel)
	}
	if err := writeText(filepath.Join(stage, "binaries.sha256"), digestLines.String()); err != nil {
		return err
	}
	for _, name := range RequiredPolicyReports {
		if err := copyFile(inputs.policies[name], filepath.Join(stage, "policy", name+".json")); err != nil {
			return fmt.Errorf("release: copy %s policy report: %w", name, err)
		}
	}
	if err := writeText(filepath.Join(stage, filepath.FromSlash(ProductGateFileName)), string(inputs.gateJSON)); err != nil {
		return fmt.Errorf("release: write product-gate report: %w", err)
	}
	return nil
}

func manifestFor(stage, version string, gate ProductGateRecord) (Manifest, error) {
	var artifacts []Artifact
	err := filepath.WalkDir(stage, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(stage, path)
		if err != nil {
			return err
		}
		digest, err := fileDigest(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		artifacts = append(artifacts, Artifact{Path: filepath.ToSlash(rel), SHA256: digest, Size: info.Size()})
		return nil
	})
	if err != nil {
		return Manifest{}, fmt.Errorf("release: hash bundle artifacts: %w", err)
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return Manifest{SchemaVersion: SchemaVersion, Version: version, Artifacts: artifacts, ProductGate: &gate}, nil
}

func verifyFiles(bundle string, artifacts []Artifact) error {
	expected := make(map[string]bool, len(artifacts))
	for _, artifact := range artifacts {
		expected[artifact.Path] = true
		path := filepath.Join(bundle, filepath.FromSlash(artifact.Path))
		digest, err := fileDigest(path)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("release: artifact %q is missing", artifact.Path)
			}
			return fmt.Errorf("release: artifact %q: %w", artifact.Path, err)
		}
		if digest != artifact.SHA256 {
			return fmt.Errorf("release: artifact %q sha256 mismatch: manifest %s, actual %s", artifact.Path, artifact.SHA256, digest)
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("release: artifact %q: %w", artifact.Path, err)
		}
		if info.Size() != artifact.Size {
			return fmt.Errorf("release: artifact %q size mismatch: manifest %d, actual %d", artifact.Path, artifact.Size, info.Size())
		}
	}
	var extras []string
	err := filepath.WalkDir(bundle, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(bundle, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel != ManifestFileName && !expected[rel] {
			extras = append(extras, rel)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("release: scan bundle: %w", err)
	}
	if len(extras) != 0 {
		sort.Strings(extras)
		return fmt.Errorf("release: unmanifested artifact %q", extras[0])
	}
	return nil
}

func verifyBinaryDigestList(bundle string, artifacts []Artifact) error {
	data, err := os.ReadFile(filepath.Join(bundle, "binaries.sha256"))
	if err != nil {
		return fmt.Errorf("release: read binaries.sha256: %w", err)
	}
	manifestByPath := make(map[string]string)
	for _, artifact := range artifacts {
		if strings.HasPrefix(artifact.Path, "binaries/") {
			manifestByPath[artifact.Path] = artifact.SHA256
		}
	}
	seen := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || len(fields[0]) != sha256.Size*2 {
			return fmt.Errorf("release: malformed binaries.sha256 line %q", line)
		}
		path := filepath.ToSlash(fields[1])
		if !strings.HasPrefix(path, "binaries/") || seen[path] {
			return fmt.Errorf("release: invalid binary digest entry %q", path)
		}
		seen[path] = true
		if manifestByPath[path] != strings.ToLower(fields[0]) {
			return fmt.Errorf("release: binary digest list mismatch for %q", path)
		}
	}
	if len(seen) != len(manifestByPath) || len(seen) == 0 {
		return fmt.Errorf("release: binaries.sha256 does not cover every binary")
	}
	return nil
}

func verifyProvenanceInBundle(bundle string, manifest Manifest, trusted map[string]bool) error {
	stmt, err := provenance.LoadStatement(filepath.Join(bundle, "provenance.json"))
	if err != nil {
		return err
	}
	sbomDigest := ""
	for _, artifact := range manifest.Artifacts {
		if artifact.Path == "sbom.cdx.json" {
			sbomDigest = artifact.SHA256
		}
	}
	policy := releaseadmission.Policy{
		TrustedPublicKeys: trusted,
		PinnedPublicKeys:  mapKeys(trusted),
		AllowedBuilders:   map[string]bool{provenance.BuilderID: true},
		SBOMDigest:        sbomDigest,
	}
	if err := releaseadmission.Verify(policy, *stmt); err != nil {
		return fmt.Errorf("release: provenance admission: %w", err)
	}
	return nil
}

func validateProvenance(sbomPath, provenancePath string, privateKey []byte) error {
	stmt, err := provenance.LoadStatement(provenancePath)
	if err != nil {
		return err
	}
	sbomDigest, err := fileDigest(sbomPath)
	if err != nil {
		return fmt.Errorf("release: hash SBOM: %w", err)
	}
	pub := hex.EncodeToString(privateKey[32:])
	policy := releaseadmission.Policy{
		TrustedPublicKeys: map[string]bool{pub: true},
		PinnedPublicKeys:  []string{pub},
		AllowedBuilders:   map[string]bool{provenance.BuilderID: true},
		SBOMDigest:        sbomDigest,
	}
	if err := releaseadmission.Verify(policy, *stmt); err != nil {
		return fmt.Errorf("release: provenance admission: %w", err)
	}
	return nil
}

func requireBundleArtifacts(artifacts []Artifact) error {
	present := make(map[string]bool, len(artifacts))
	for _, artifact := range artifacts {
		present[artifact.Path] = true
	}
	required := []string{"version.txt", "binaries.sha256", "sbom.cdx.json", "provenance.json", "p1a-evidence-report.json", ProductGateFileName}
	for _, name := range RequiredPolicyReports {
		required = append(required, "policy/"+name+".json")
	}
	for _, name := range required {
		if !present[name] {
			return fmt.Errorf("release: required artifact %q is missing from manifest", name)
		}
	}
	return nil
}

func validateArtifactEntries(artifacts []Artifact) error {
	previous := ""
	seen := make(map[string]bool, len(artifacts))
	for _, artifact := range artifacts {
		if err := validateRelativePath(artifact.Path); err != nil {
			return fmt.Errorf("release: artifact %q: %w", artifact.Path, err)
		}
		if artifact.Path == ManifestFileName {
			return fmt.Errorf("release: manifest cannot list itself as an artifact")
		}
		if seen[artifact.Path] {
			return fmt.Errorf("release: duplicate artifact %q", artifact.Path)
		}
		if previous != "" && artifact.Path <= previous {
			return fmt.Errorf("release: artifacts are not in canonical path order at %q", artifact.Path)
		}
		if len(artifact.SHA256) != sha256.Size*2 {
			return fmt.Errorf("release: artifact %q has invalid sha256", artifact.Path)
		}
		if _, err := hex.DecodeString(artifact.SHA256); err != nil {
			return fmt.Errorf("release: artifact %q has invalid sha256: %w", artifact.Path, err)
		}
		if artifact.Size < 0 {
			return fmt.Errorf("release: artifact %q has negative size", artifact.Path)
		}
		seen[artifact.Path] = true
		previous = artifact.Path
	}
	return nil
}

func declaredVersion(root string, opts Options) (string, error) {
	if strings.TrimSpace(opts.Version) != "" {
		return strings.TrimSpace(opts.Version), nil
	}
	path := opts.VersionFile
	if path == "" {
		path = DefaultVersionFile
	}
	data, err := os.ReadFile(rooted(root, path))
	if err != nil {
		return "", fmt.Errorf("release: read version file %s: %w", path, err)
	}
	version := strings.TrimSpace(string(data))
	if version == "" {
		return "", fmt.Errorf("release: version file %s is empty", path)
	}
	return version, nil
}

func writeManifest(path string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("release: encode manifest: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("release: write manifest: %w", err)
	}
	return nil
}

func copyFile(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o644)
}

func writeText(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("release: create parent for %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return fmt.Errorf("release: write %s: %w", path, err)
	}
	return nil
}

func fileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func requireReadable(path, label string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("release: read %s %s: %w", label, path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("release: %s %s is a directory", label, path)
	}
	return nil
}

func rooted(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func validateLeaf(name, label string) error {
	if name == "" || name == "." || name == ".." || strings.Contains(name, "../") || strings.Contains(name, "..\\") || filepath.IsAbs(name) || strings.ContainsAny(name, `<>:"|?*`) {
		return fmt.Errorf("release: unsafe %s %q", label, name)
	}
	if filepath.Base(name) != name {
		return fmt.Errorf("release: %s %q must be a file name", label, name)
	}
	return nil
}

func validateRelativePath(path string) error {
	if path == "" || filepath.IsAbs(path) || strings.HasPrefix(path, "../") || strings.Contains(path, `\`) || strings.ContainsAny(path, `<>:"|?*`) || filepath.Clean(filepath.FromSlash(path)) != filepath.FromSlash(path) {
		return fmt.Errorf("unsafe relative path")
	}
	return nil
}

func signatureKey(manifest Manifest) string {
	if manifest.Signature == nil {
		return "<none>"
	}
	return manifest.Signature.PublicKey
}
