package release

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/provenance"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/releaseadmission"
)

// KeySource is the release bundle signing port. It is an alias of the
// provenance port so one custody-resolved signing ceremony can sign both
// artifacts without exposing private key material to this package.
type KeySource = provenance.KeySource

func init() {
	for _, name := range releaseadmission.RequiredScannerPolicyReports {
		present := false
		for _, existing := range RequiredPolicyReports {
			if existing == name {
				present = true
				break
			}
		}
		if !present {
			RequiredPolicyReports = append(RequiredPolicyReports, name)
		}
	}
}

// NewFixtureKeySource returns the checked-in development fixture source.
// Production callers should use NewCustodyKeySource instead.
func NewFixtureKeySource(path string) (KeySource, error) {
	return provenance.LoadFixtureKeySource(path)
}

// NewCustodyKeySource returns a release signing source backed by custody.
func NewCustodyKeySource(provider custody.Provider, context custody.Context, handle custody.Handle, publicKeyHex string) (KeySource, error) {
	return provenance.NewCustodyKeySource(provider, context, handle, publicKeyHex)
}

// BuildWithKeySource assembles and signs a bundle using a resolved KeySource.
// The existing Build function remains fixture-compatible; this additive entry
// point is the production path for a custody-backed signing ceremony.
func BuildWithKeySource(root string, opts Options, source KeySource) (Manifest, error) {
	if err := validateKeySource(source); err != nil {
		return Manifest{}, err
	}
	publicKey := strings.ToLower(strings.TrimSpace(source.PublicKey()))
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
	if err := validateProvenanceWithPublicKey(inputs.sbom, inputs.provenance, publicKey); err != nil {
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
	signature, err := source.SignDigest(digest)
	if err != nil {
		return Manifest{}, fmt.Errorf("release: sign manifest through key source: %w", err)
	}
	signatureBytes, err := hex.DecodeString(signature)
	if err != nil || len(signatureBytes) != ed25519.SignatureSize {
		return Manifest{}, fmt.Errorf("release: key source signature.value: invalid Ed25519 signature")
	}
	manifest.Signature = &gateevidence.Signature{Algorithm: "ed25519", PublicKey: publicKey, Value: signature}
	if err := writeManifest(filepath.Join(stage, ManifestFileName), manifest); err != nil {
		return Manifest{}, err
	}
	if err := os.Rename(stage, out); err != nil {
		return Manifest{}, fmt.Errorf("release: publish immutable bundle: %w", err)
	}
	return manifest, nil
}

func validateKeySource(source KeySource) error {
	if source == nil {
		return fmt.Errorf("release: key source is required")
	}
	publicKey := strings.TrimSpace(source.PublicKey())
	decoded, err := hex.DecodeString(publicKey)
	if err != nil || len(decoded) != ed25519.PublicKeySize {
		return fmt.Errorf("release: key source public_key: invalid Ed25519 public key")
	}
	return nil
}

func validateProvenanceWithPublicKey(sbomPath, provenancePath, publicKey string) error {
	statement, err := provenance.LoadStatement(provenancePath)
	if err != nil {
		return err
	}
	sbomDigest, err := fileDigest(sbomPath)
	if err != nil {
		return fmt.Errorf("release: hash SBOM: %w", err)
	}
	policy := releaseadmission.Policy{
		TrustedPublicKeys: map[string]bool{publicKey: true},
		PinnedPublicKeys:  []string{publicKey},
		AllowedBuilders:   map[string]bool{provenance.BuilderID: true},
		SBOMDigest:        sbomDigest,
	}
	if err := releaseadmission.Verify(policy, *statement); err != nil {
		return fmt.Errorf("release: provenance admission: %w", err)
	}
	return nil
}

// VerifyBundleWithScannerEvidence performs the regular offline bundle
// verification and additionally requires all four scanner evidence records.
func VerifyBundleWithScannerEvidence(bundle string, opts VerifyOptions) (Verification, error) {
	receipt, err := VerifyBundle(bundle, opts)
	if err != nil {
		return Verification{}, err
	}
	statement, err := provenance.LoadStatement(filepath.Join(bundle, "provenance.json"))
	if err != nil {
		return Verification{}, err
	}
	evidence := make([]releaseadmission.ScannerEvidence, 0, len(releaseadmission.RequiredScannerPolicyReports))
	for _, name := range releaseadmission.RequiredScannerPolicyReports {
		data, err := os.ReadFile(filepath.Join(bundle, "policy", name+".json"))
		if err != nil {
			return Verification{}, fmt.Errorf("release: read scanner evidence %s: %w", name, err)
		}
		var record releaseadmission.ScannerEvidence
		if err := json.Unmarshal(data, &record); err != nil {
			return Verification{}, fmt.Errorf("release: parse scanner evidence %s: %w", name, err)
		}
		evidence = append(evidence, record)
	}
	trusted := opts.TrustedPublicKeys
	if len(trusted) == 0 {
		trusted = map[string]bool{DevFixturePublicKey: true}
	}
	policy := releaseadmission.Policy{
		TrustedPublicKeys:      trusted,
		PinnedPublicKeys:       mapKeys(trusted),
		AllowedBuilders:        map[string]bool{provenance.BuilderID: true},
		SBOMDigest:             statement.SBOM.SHA256,
		RequireScannerEvidence: true,
		ScannerEvidence:        evidence,
	}
	if err := releaseadmission.Verify(policy, *statement); err != nil {
		return Verification{}, fmt.Errorf("release: scanner evidence admission: %w", err)
	}
	return receipt, nil
}

func mapKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key, enabled := range values {
		if enabled {
			keys = append(keys, key)
		}
	}
	return keys
}
