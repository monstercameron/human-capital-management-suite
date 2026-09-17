package defaultproduct

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// ALIGN-047: domain-pack activation manifests compile the packs a tenant
// may turn on. Each pack names versioned features with the capability each
// feature requires; compiling pins the whole set under one digest, and
// activating admits only the features whose capability the tenant holds. A
// feature without an admitted capability is refused by name — never
// silently skipped, never activated.

// Manifest errors.
var (
	ErrManifestInvalid   = errors.New("defaultproduct: activation manifest is invalid")
	ErrActivationRefused = errors.New("defaultproduct: pack feature is not admitted")
	ErrActivationUnknown = errors.New("defaultproduct: pack feature is not manifested")
)

// PackFeature is one activatable feature inside a domain pack.
type PackFeature struct {
	ID          string `json:"id"`
	Capability  string `json:"capability"`
	Disposition string `json:"disposition"`
}

// DomainPack is one versioned pack of default features.
type DomainPack struct {
	ID       string        `json:"id"`
	Version  string        `json:"version"`
	Features []PackFeature `json:"features"`
}

// ActivationManifest is the compiled, digest-pinned pack set.
type ActivationManifest struct {
	Packs  []DomainPack `json:"packs"`
	Digest string       `json:"digest"`
}

// ActivatedPack is the activation outcome for one pack: the admitted
// features, in manifest order.
type ActivatedPack struct {
	ID       string   `json:"id"`
	Version  string   `json:"version"`
	Features []string `json:"features"`
}

// DefaultPacks seeds the domain packs.
func DefaultPacks() []DomainPack {
	return []DomainPack{
		{ID: "promotion.pack", Version: "1", Features: []PackFeature{
			{ID: "promotion.list", Capability: "promotion.view", Disposition: "core"},
			{ID: "promotion.execute", Capability: "promotion.execute", Disposition: "default"},
		}},
		{ID: "operations.pack", Version: "1", Features: []PackFeature{
			{ID: "operations.repair", Capability: "operations.repair", Disposition: "default"},
		}},
	}
}

// Compile validates packs and pins them under one digest. Feature IDs must
// be unique across packs; every feature needs a capability.
func Compile(packs []DomainPack) (ActivationManifest, error) {
	seen := make(map[string]bool)
	for _, pack := range packs {
		if !safeID(pack.ID) || !safeID(pack.Version) {
			return ActivationManifest{}, fmt.Errorf("%w: pack id and version are required", ErrManifestInvalid)
		}
		if len(pack.Features) == 0 {
			return ActivationManifest{}, fmt.Errorf("%w: pack %q has no features", ErrManifestInvalid, pack.ID)
		}
		for _, feature := range pack.Features {
			if !safeID(feature.ID) {
				return ActivationManifest{}, fmt.Errorf("%w: feature id is required in pack %q", ErrManifestInvalid, pack.ID)
			}
			if seen[feature.ID] {
				return ActivationManifest{}, fmt.Errorf("%w: duplicate feature %q", ErrManifestInvalid, feature.ID)
			}
			seen[feature.ID] = true
			if !safeID(feature.Capability) {
				return ActivationManifest{}, fmt.Errorf("%w: feature %q needs a capability", ErrManifestInvalid, feature.ID)
			}
		}
	}
	manifest := ActivationManifest{Packs: append([]DomainPack(nil), packs...)}
	manifest.Digest = manifest.computeDigest()
	return manifest, nil
}

// Activate admits the manifested features the tenant holds capabilities
// for. Every requested feature must be manifested and admitted; the first
// failure names its feature.
func (m ActivationManifest) Activate(requested []string, admitted []string) ([]ActivatedPack, error) {
	if m.Digest == "" || m.Digest != m.computeDigest() {
		return nil, fmt.Errorf("%w: manifest digest does not match its content", ErrManifestInvalid)
	}
	grants := make(map[string]bool, len(admitted))
	for _, capability := range admitted {
		grants[capability] = true
	}
	features := make(map[string]PackFeature)
	packOf := make(map[string]DomainPack)
	for _, pack := range m.Packs {
		for _, feature := range pack.Features {
			features[feature.ID] = feature
			packOf[feature.ID] = pack
		}
	}
	activated := make(map[string][]string)
	for _, id := range requested {
		feature, ok := features[id]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrActivationUnknown, id)
		}
		if !grants[feature.Capability] {
			return nil, fmt.Errorf("%w: %q requires %q", ErrActivationRefused, id, feature.Capability)
		}
		activated[packOf[id].ID] = append(activated[packOf[id].ID], id)
	}
	ordered := make([]ActivatedPack, 0, len(activated))
	for _, pack := range m.Packs {
		if ids, ok := activated[pack.ID]; ok {
			ordered = append(ordered, ActivatedPack{ID: pack.ID, Version: pack.Version, Features: ids})
		}
	}
	return ordered, nil
}

func (m ActivationManifest) computeDigest() string {
	packs := append([]DomainPack(nil), m.Packs...)
	sort.Slice(packs, func(i, j int) bool { return packs[i].ID < packs[j].ID })
	for i := range packs {
		features := append([]PackFeature(nil), packs[i].Features...)
		sort.Slice(features, func(a, b int) bool { return features[a].ID < features[b].ID })
		packs[i].Features = features
	}
	b, err := json.Marshal(packs)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyDigest reports whether the digest matches the compiled packs.
func (m ActivationManifest) VerifyDigest() error {
	if m.Digest == "" || m.Digest != m.computeDigest() {
		return fmt.Errorf("%w: manifest digest does not match its content", ErrManifestInvalid)
	}
	return nil
}
