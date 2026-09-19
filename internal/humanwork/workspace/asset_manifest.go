package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"path"
	"sort"
	"strings"
)

// AssetIntegrityManifestVersion is the wire version of the generated
// frontend asset manifest. It describes presentation bytes only; it carries
// no page, principal, tenant, workflow, or authorization state.
const AssetIntegrityManifestVersion = "hcm-next.asset-integrity.v1"

// AssetIntegrityManifestName is the non-routable source name used for the
// manifest endpoint. It is handled before the embedded asset allowlist.
const AssetIntegrityManifestName = "manifest.json"

const (
	maxAssetManifestBytes = 1 << 20
	maxManifestAssets     = 1024
	maxAssetPathBytes     = 256
	maxContentTypeBytes   = 128
	maxAssetBytes         = 512 << 20
)

// AssetIntegritySource is one deterministic input to manifest generation.
// Name is relative to the embedded assets directory and Body is copied by
// GenerateAssetIntegrityManifest before it returns.
type AssetIntegritySource struct {
	Name        string
	Body        []byte
	ContentType string
	GzipBody    []byte
}

// AssetIntegrityRepresentation describes one transfer representation. The
// identity representation is always present; gzip is present only when the
// build produced the matching precompressed bytes. SRI itself is calculated
// over the identity bytes, as required by browser SRI semantics.
type AssetIntegrityRepresentation struct {
	Encoding string `json:"encoding"`
	Bytes    int64  `json:"bytes"`
	SHA256   string `json:"sha256"`
}

// AssetIntegrity is one public, content-addressed frontend asset.
type AssetIntegrity struct {
	Path            string                         `json:"path"`
	ContentType     string                         `json:"content_type"`
	Bytes           int64                          `json:"bytes"`
	SHA256          string                         `json:"sha256"`
	Integrity       string                         `json:"integrity"`
	Representations []AssetIntegrityRepresentation `json:"representations"`
}

// AssetIntegrityManifest is the generated frontend asset contract. Its order
// is canonicalized by generation and validation, making the JSON and digest
// reproducible across machines and input ordering.
type AssetIntegrityManifest struct {
	Version string           `json:"version"`
	Assets  []AssetIntegrity `json:"assets"`
}

type assetIntegrityMetadata struct {
	ContentType string
	Integrity   string
	ETags       map[string]string
	// SHA256 is the identity representation's digest. A request that names
	// it is asking for exactly these bytes and can be answered immutably
	// (UXLIVE-013).
	SHA256 string
}

func indexAssetIntegrityManifest(m AssetIntegrityManifest) map[string]assetIntegrityMetadata {
	index := make(map[string]assetIntegrityMetadata, len(m.Assets))
	for _, asset := range m.Assets {
		metadata := assetIntegrityMetadata{ContentType: asset.ContentType, Integrity: asset.Integrity, SHA256: asset.SHA256, ETags: make(map[string]string, len(asset.Representations))}
		for _, representation := range asset.Representations {
			metadata.ETags[representation.Encoding] = `"` + representation.SHA256 + `"`
		}
		index[asset.Path] = metadata
	}
	return index
}

// GenerateAssetIntegrityManifest builds a deterministic manifest from static
// asset bytes. It refuses unsafe names, missing media types, duplicate paths,
// and malformed source records before producing any output.
func GenerateAssetIntegrityManifest(sources []AssetIntegritySource) (AssetIntegrityManifest, error) {
	if len(sources) == 0 || len(sources) > maxManifestAssets {
		return AssetIntegrityManifest{}, fmt.Errorf("asset source count %d is outside 1..%d", len(sources), maxManifestAssets)
	}
	manifest := AssetIntegrityManifest{Version: AssetIntegrityManifestVersion, Assets: make([]AssetIntegrity, 0, len(sources))}
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		name := strings.TrimSpace(source.Name)
		if name != source.Name {
			return AssetIntegrityManifest{}, fmt.Errorf("asset name %q contains surrounding whitespace", source.Name)
		}
		if err := validateAssetName(name); err != nil {
			return AssetIntegrityManifest{}, err
		}
		if name == AssetIntegrityManifestName {
			return AssetIntegrityManifest{}, fmt.Errorf("asset name %q is reserved", name)
		}
		if err := validateAssetContentType(name, source.ContentType); err != nil {
			return AssetIntegrityManifest{}, err
		}
		if len(source.Body) == 0 || len(source.Body) > maxAssetBytes {
			return AssetIntegrityManifest{}, fmt.Errorf("asset %q byte count is outside 1..%d", name, maxAssetBytes)
		}
		if source.GzipBody != nil && (len(source.GzipBody) == 0 || len(source.GzipBody) > maxAssetBytes) {
			return AssetIntegrityManifest{}, fmt.Errorf("asset %q gzip byte count is outside 1..%d", name, maxAssetBytes)
		}
		pathName := PathAssetPrefix + name
		if _, exists := seen[pathName]; exists {
			return AssetIntegrityManifest{}, fmt.Errorf("asset %q is duplicated", name)
		}
		seen[pathName] = struct{}{}
		body := append([]byte(nil), source.Body...)
		identity := representation("identity", body)
		asset := AssetIntegrity{
			Path: pathName, ContentType: source.ContentType,
			Bytes: int64(len(body)), SHA256: identity.SHA256,
			Integrity:       integrityForDigest(identity.SHA256),
			Representations: []AssetIntegrityRepresentation{identity},
		}
		if source.GzipBody != nil {
			asset.Representations = append(asset.Representations, representation("gzip", source.GzipBody))
		}
		manifest.Assets = append(manifest.Assets, asset)
	}
	sort.Slice(manifest.Assets, func(i, j int) bool { return manifest.Assets[i].Path < manifest.Assets[j].Path })
	if err := manifest.Validate(); err != nil {
		return AssetIntegrityManifest{}, err
	}
	return manifest, nil
}

// BuildEmbeddedAssetIntegrityManifest generates the release manifest from
// the exact files this package can serve. Orphaned .gz files are ignored and
// therefore cannot accidentally become routable content.
func BuildEmbeddedAssetIntegrityManifest() (AssetIntegrityManifest, error) {
	sources, err := embeddedAssetSources()
	if err != nil {
		return AssetIntegrityManifest{}, err
	}
	return GenerateAssetIntegrityManifest(sources)
}

// LoadEmbeddedAssetIntegrityManifest loads the generated release artifact and
// compares it with the exact embedded routable catalog. Runtime code never
// invents or self-asserts a manifest: a missing, stale, extra, or altered
// artifact prevents handler construction.
func LoadEmbeddedAssetIntegrityManifest() (AssetIntegrityManifest, error) {
	body, err := fs.ReadFile(assetsFS, "assets/"+AssetIntegrityManifestName)
	if err != nil {
		return AssetIntegrityManifest{}, fmt.Errorf("read generated asset manifest: %w", err)
	}
	manifest, err := ParseAssetIntegrityManifest(body)
	if err != nil {
		return AssetIntegrityManifest{}, err
	}
	sources, err := embeddedAssetSources()
	if err != nil {
		return AssetIntegrityManifest{}, err
	}
	if err := ValidateAssetIntegrityCatalog(manifest, sources); err != nil {
		return AssetIntegrityManifest{}, err
	}
	return manifest, nil
}

// ValidateAssetIntegrityCatalog proves that a generated artifact has exactly
// the supplied identity and transfer catalog. It rejects missing, extra,
// stale, and mutated entries, including orphaned gzip representations.
func ValidateAssetIntegrityCatalog(manifest AssetIntegrityManifest, sources []AssetIntegritySource) error {
	expected, err := GenerateAssetIntegrityManifest(sources)
	if err != nil {
		return err
	}
	want, err := expected.CanonicalJSON()
	if err != nil {
		return err
	}
	got, err := manifest.CanonicalJSON()
	if err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return errors.New("generated asset manifest is stale or does not match embedded assets")
	}
	return nil
}

func embeddedAssetSources() ([]AssetIntegritySource, error) {
	entries, err := fs.ReadDir(assetsFS, "assets")
	if err != nil {
		return nil, fmt.Errorf("read embedded assets: %w", err)
	}
	sources := make([]AssetIntegritySource, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == ".keep" || strings.HasSuffix(name, ".gz") || name == AssetIntegrityManifestName {
			continue
		}
		body, ok := asset(name)
		if !ok {
			continue
		}
		var gzipBody []byte
		if compressed, compressedOK := compressedAsset(name); compressedOK {
			gzipBody = compressed
		}
		contentType, _ := FrontendAssetContentType(name)
		sources = append(sources, AssetIntegritySource{
			Name: name, Body: body, ContentType: contentType, GzipBody: gzipBody,
		})
	}
	return sources, nil
}

// EmbeddedAssetIntegrityManifest is the concise public alias used by build
// and serving integrations.
func EmbeddedAssetIntegrityManifest() (AssetIntegrityManifest, error) {
	return BuildEmbeddedAssetIntegrityManifest()
}

// EmbeddedAssetIntegrityManifestJSON returns validated canonical manifest
// bytes for packaging tools that do not need the decoded representation.
func EmbeddedAssetIntegrityManifestJSON() ([]byte, error) {
	manifest, err := BuildEmbeddedAssetIntegrityManifest()
	if err != nil {
		return nil, err
	}
	return manifest.CanonicalJSON()
}

// Validate rejects stale, ambiguous, or unsafe manifest data. Callers should
// validate before serving or using a manifest as build evidence.
func (m AssetIntegrityManifest) Validate() error {
	if m.Version != AssetIntegrityManifestVersion {
		return fmt.Errorf("unsupported asset manifest version %q", m.Version)
	}
	if m.Assets == nil {
		return errors.New("asset manifest has no asset list")
	}
	if len(m.Assets) == 0 || len(m.Assets) > maxManifestAssets {
		return fmt.Errorf("asset manifest contains %d assets; expected 1..%d", len(m.Assets), maxManifestAssets)
	}
	seen := make(map[string]struct{}, len(m.Assets))
	for _, asset := range m.Assets {
		if err := validateAssetPath(asset.Path); err != nil {
			return err
		}
		if len(asset.Path) > maxAssetPathBytes || len(asset.ContentType) > maxContentTypeBytes {
			return fmt.Errorf("asset %q metadata exceeds bounds", asset.Path)
		}
		if _, exists := seen[asset.Path]; exists {
			return fmt.Errorf("asset %q is duplicated", asset.Path)
		}
		seen[asset.Path] = struct{}{}
		if err := validateAssetContentType(asset.Path, asset.ContentType); err != nil {
			return err
		}
		if asset.Bytes <= 0 || asset.Bytes > maxAssetBytes {
			return fmt.Errorf("asset %q byte count is outside 1..%d", asset.Path, maxAssetBytes)
		}
		if !validSHA256(asset.SHA256) {
			return fmt.Errorf("asset %q has invalid sha256", asset.Path)
		}
		if asset.Integrity != integrityForDigest(asset.SHA256) {
			return fmt.Errorf("asset %q has invalid SRI integrity", asset.Path)
		}
		if len(asset.Representations) < 1 || len(asset.Representations) > 2 {
			return fmt.Errorf("asset %q has %d transfer representations; expected 1..2", asset.Path, len(asset.Representations))
		}
		encodings := make(map[string]struct{}, len(asset.Representations))
		identity := false
		for representationIndex, rep := range asset.Representations {
			if rep.Encoding != "identity" && rep.Encoding != "gzip" {
				return fmt.Errorf("asset %q has unsupported encoding %q", asset.Path, rep.Encoding)
			}
			if _, exists := encodings[rep.Encoding]; exists {
				return fmt.Errorf("asset %q repeats encoding %q", asset.Path, rep.Encoding)
			}
			encodings[rep.Encoding] = struct{}{}
			if rep.Bytes <= 0 || rep.Bytes > maxAssetBytes || !validSHA256(rep.SHA256) {
				return fmt.Errorf("asset %q has invalid %s representation", asset.Path, rep.Encoding)
			}
			if representationIndex == 0 && rep.Encoding != "identity" || representationIndex == 1 && rep.Encoding != "gzip" {
				return fmt.Errorf("asset %q representations are not in canonical identity, gzip order", asset.Path)
			}
			if rep.Encoding == "identity" {
				identity = true
				if rep.Bytes != asset.Bytes || rep.SHA256 != asset.SHA256 {
					return fmt.Errorf("asset %q identity representation disagrees with asset digest", asset.Path)
				}
			}
		}
		if !identity {
			return fmt.Errorf("asset %q has no identity representation", asset.Path)
		}
	}
	for i := 1; i < len(m.Assets); i++ {
		if m.Assets[i-1].Path >= m.Assets[i].Path {
			return errors.New("asset manifest is not in canonical path order")
		}
	}
	return nil
}

// CanonicalJSON returns the deterministic manifest bytes. Validation is
// performed first so no invalid manifest can become a release artifact.
func (m AssetIntegrityManifest) CanonicalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// Digest returns the SHA-256 digest of CanonicalJSON.
func (m AssetIntegrityManifest) Digest() (string, error) {
	body, err := m.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// ParseAssetIntegrityManifest strictly parses and validates generated JSON.
func ParseAssetIntegrityManifest(data []byte) (AssetIntegrityManifest, error) {
	if len(data) > maxAssetManifestBytes {
		return AssetIntegrityManifest{}, errors.New("parse asset manifest: input exceeds size bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest AssetIntegrityManifest
	if err := decoder.Decode(&manifest); err != nil {
		return AssetIntegrityManifest{}, fmt.Errorf("parse asset manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return AssetIntegrityManifest{}, errors.New("parse asset manifest: trailing JSON")
	} else if !errors.Is(err, io.EOF) {
		return AssetIntegrityManifest{}, fmt.Errorf("parse asset manifest: trailing JSON: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return AssetIntegrityManifest{}, err
	}
	return manifest, nil
}

// VerifyAsset checks served bytes against the generated manifest. It is used
// at the serving boundary so a packaging mismatch fails closed rather than
// returning an unverified bundle.
func (m AssetIntegrityManifest) VerifyAsset(assetPath, encoding string, body []byte) error {
	if err := m.Validate(); err != nil {
		return err
	}
	for _, asset := range m.Assets {
		if asset.Path != assetPath {
			continue
		}
		for _, rep := range asset.Representations {
			if rep.Encoding != encoding {
				continue
			}
			sum := sha256.Sum256(body)
			if int64(len(body)) != rep.Bytes || hex.EncodeToString(sum[:]) != rep.SHA256 {
				return fmt.Errorf("asset %q %s bytes failed integrity validation", assetPath, encoding)
			}
			return nil
		}
		return fmt.Errorf("asset %q has no %s representation", assetPath, encoding)
	}
	return fmt.Errorf("asset %q is absent from integrity manifest", assetPath)
}

func representation(encoding string, body []byte) AssetIntegrityRepresentation {
	sum := sha256.Sum256(body)
	return AssetIntegrityRepresentation{Encoding: encoding, Bytes: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
}

func integrityForDigest(digest string) string {
	decoded, err := hex.DecodeString(digest)
	if err != nil {
		return ""
	}
	return "sha256-" + base64.StdEncoding.EncodeToString(decoded)
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateAssetContentType(name, value string) error {
	if value == "" || len(value) > maxContentTypeBytes {
		return fmt.Errorf("asset %q has an invalid content type", name)
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil || mediaType == "" || mediaType != strings.ToLower(mediaType) || !strings.Contains(mediaType, "/") {
		return fmt.Errorf("asset %q has invalid content type %q", name, value)
	}
	return nil
}

func validateAssetName(name string) error {
	if name == "" || name == "." || name == ".." || name != path.Base(name) || strings.ContainsAny(name, `/\\?#`) {
		return fmt.Errorf("asset name %q is not a safe relative filename", name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("asset name %q contains control characters", name)
		}
	}
	return nil
}

func validateAssetPath(value string) error {
	if !strings.HasPrefix(value, PathAssetPrefix) {
		return fmt.Errorf("asset path %q is outside the workspace asset prefix", value)
	}
	return validateAssetName(strings.TrimPrefix(value, PathAssetPrefix))
}
