// Package intentmanifests provides YAML-based manifests for intent definitions and features.
// This file provides real implementations using gopkg.in/yaml.v3.
package intentmanifests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// IntentManifest is the parsed structure of the intent conformance descriptors.
type IntentManifest struct {
	Descriptors []IntentDescriptor `yaml:"descriptors"`
}

// FeatureManifest is the parsed structure of the feature-to-intent intake.
type FeatureManifest struct {
	Groups               []FeatureGroup `yaml:"groups"`
	SourceManifestDigest string         `yaml:"source_manifest_digest"`
	TotalGroups          int            `yaml:"total_groups"`
	Version              string         `yaml:"version"`
	MaterialFeatureRule  string         `yaml:"material_feature_rule"`
}

// LoadIntentManifestYAML loads and parses the intent conformance descriptors from YAML.
func LoadIntentManifestYAML(path string) ([]IntentDescriptor, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read intent manifest: %w", err)
	}

	var manifest IntentManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse intent manifest: %w", err)
	}

	return manifest.Descriptors, nil
}

// LoadFeatureManifestYAML loads and parses the feature-to-intent intake from YAML.
func LoadFeatureManifestYAML(path string) ([]FeatureGroup, error) {
	manifest, err := LoadFeatureIntakeYAML(path)
	if err != nil {
		return nil, err
	}
	return manifest.Groups, nil
}

// LoadFeatureIntakeYAML loads the complete immutable intake envelope and its groups.
func LoadFeatureIntakeYAML(path string) (FeatureManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FeatureManifest{}, fmt.Errorf("read feature manifest: %w", err)
	}

	var manifest FeatureManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return FeatureManifest{}, fmt.Errorf("parse feature manifest: %w", err)
	}

	return manifest, nil
}

// ComputeIntentDigestYAML returns a stable SHA256 digest of the intent descriptors.
func ComputeIntentDigestYAML(descriptors []IntentDescriptor) (string, error) {
	cpy := append([]IntentDescriptor(nil), descriptors...)
	sort.Slice(cpy, func(i, j int) bool {
		return fmt.Sprintf("%s/v%d", cpy[i].IntentTypeID, cpy[i].Version) <
			fmt.Sprintf("%s/v%d", cpy[j].IntentTypeID, cpy[j].Version)
	})

	b, err := json.Marshal(cpy)
	if err != nil {
		return "", err
	}

	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// ComputeFeatureDigestYAML returns a stable SHA256 digest of the feature groups.
func ComputeFeatureDigestYAML(groups []FeatureGroup) (string, error) {
	cpy := append([]FeatureGroup(nil), groups...)
	sort.Slice(cpy, func(i, j int) bool { return cpy[i].GroupID < cpy[j].GroupID })

	b, err := json.Marshal(cpy)
	if err != nil {
		return "", err
	}

	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// ValidateFeatureIntakeYAML verifies the intake envelope and its complete group set.
func ValidateFeatureIntakeYAML(manifest FeatureManifest) error {
	if manifest.Version != "1.0" {
		return fmt.Errorf("feature manifest version=%q, want 1.0", manifest.Version)
	}
	if manifest.TotalGroups != 49 || len(manifest.Groups) != manifest.TotalGroups {
		return fmt.Errorf("feature manifest total_groups=%d and parsed groups=%d, want 49", manifest.TotalGroups, len(manifest.Groups))
	}
	if strings.TrimSpace(manifest.MaterialFeatureRule) != "A material feature binds to exactly one defined intent; DEFERRED marks an unbound candidate awaiting review and does not add it to the catalog." {
		return fmt.Errorf("feature manifest material_feature_rule is missing or differs from the governing rule")
	}
	if manifest.SourceManifestDigest == "" {
		return fmt.Errorf("feature manifest missing source_manifest_digest")
	}
	if len(manifest.SourceManifestDigest) != 64 {
		return fmt.Errorf("feature manifest source_manifest_digest length=%d, want 64 hex characters", len(manifest.SourceManifestDigest))
	}
	if _, err := hex.DecodeString(manifest.SourceManifestDigest); err != nil {
		return fmt.Errorf("feature manifest source_manifest_digest is not hexadecimal: %w", err)
	}
	if err := ValidateFeatureManifestYAML(manifest.Groups); err != nil {
		return err
	}
	digest, err := ComputeFeatureDigestYAML(manifest.Groups)
	if err != nil {
		return fmt.Errorf("compute feature manifest digest: %w", err)
	}
	if manifest.SourceManifestDigest != digest {
		return fmt.Errorf("feature manifest digest=%q, computed %q", manifest.SourceManifestDigest, digest)
	}
	return nil
}

// ValidateIntentManifestYAML checks finite coverage and all mandatory dimensions.
func ValidateIntentManifestYAML(descriptors []IntentDescriptor) error {
	if len(descriptors) != 14 {
		return fmt.Errorf("intent descriptor count=%d, want 14", len(descriptors))
	}

	expectedIDs := []string{
		"hcmnext.people.change_manager", "hcmnext.people.explain_worker_state", "hcmnext.people.promote_worker",
		"hcmnext.rewards.change_base_pay", "hcmnext.rewards.simulate_compensation", "hcmnext.rewards.evaluate_pay_band_position",
		"hcmnext.rewards.reserve_compensation_budget", "hcmnext.rewards.release_compensation_budget",
		"hcmnext.work.approve_proposal", "hcmnext.work.reject_proposal",
		"hcmnext.intelligence.explain_transaction", "hcmnext.operations.detect_drift",
		"hcmnext.operations.create_repair_plan", "hcmnext.operations.simulate_repair",
	}

	expected := make(map[string]bool)
	for _, id := range expectedIDs {
		expected[id] = true
	}

	seen := make(map[string]bool)
	for _, d := range descriptors {
		if !expected[d.IntentTypeID] {
			return fmt.Errorf("undrafted intent %q", d.IntentTypeID)
		}
		if d.Version != 1 || !strings.HasPrefix(d.IntentTypeID, "hcmnext.") {
			return fmt.Errorf("invalid identity %s/v%d", d.IntentTypeID, d.Version)
		}
		key := fmt.Sprintf("%s/v%d", d.IntentTypeID, d.Version)
		if seen[key] {
			return fmt.Errorf("duplicate descriptor %s", key)
		}
		seen[key] = true

		// Check mandatory dimensions.
		if len(d.Entities) == 0 || len(d.Properties) == 0 || len(d.Reads) == 0 ||
			len(d.Writes) == 0 || len(d.Effects) == 0 || len(d.Authority) == 0 ||
			len(d.Time) == 0 || len(d.Evidence) == 0 {
			return fmt.Errorf("%s: missing required dimension", key)
		}

		if d.Family == "" || d.SideEffectProfile == "" ||
			d.Lifecycle.Request == "" || d.Lifecycle.Execution == "" ||
			d.Lifecycle.Business == "" || d.Lifecycle.Consistency == "" ||
			d.Lifecycle.Obligation == "" {
			return fmt.Errorf("%s: incomplete family/side-effect/lifecycle", key)
		}

		// Validate family/side-effect combinations.
		if (d.Family == "ANALYTICAL_REQUEST" && d.SideEffectProfile != "READ_ONLY") ||
			(d.Family == "CALCULATION_REQUEST" && d.SideEffectProfile != "PURE") {
			return fmt.Errorf("%s: family %s cannot use side-effect %s", key, d.Family, d.SideEffectProfile)
		}

		// Read/calculation intents must declare writes as ["none"].
		if (d.Family == "ANALYTICAL_REQUEST" || d.Family == "CALCULATION_REQUEST") &&
			(len(d.Writes) != 1 || d.Writes[0] != "none") {
			return fmt.Errorf("%s: read/calculation descriptor declares non-trivial writes", key)
		}

		// Check negative policy and scenario.
		if len(d.NegativePolicy) == 0 ||
			d.Scenario.Name == "" || d.Scenario.Given == "" ||
			d.Scenario.When == "" || d.Scenario.Then == "" {
			return fmt.Errorf("%s: missing negative policy or scenario", key)
		}

		// Only change_manager can be conformance-only.
		if d.ConformanceOnly && d.IntentTypeID != "hcmnext.people.change_manager" {
			return fmt.Errorf("%s: only change_manager may be conformance-only", key)
		}

		// change_manager must keep its conformance-only marker: it is the
		// single row exempt from draft delivery, so silently unmarking it
		// would promote a conformance fixture into the draft set.
		if d.IntentTypeID == "hcmnext.people.change_manager" && !d.ConformanceOnly {
			return fmt.Errorf("%s: change_manager must stay conformance-only", key)
		}
	}

	// Verify all expected IDs are present.
	for _, id := range expectedIDs {
		if !seen[id+"/v1"] {
			return fmt.Errorf("missing descriptor %s/v1", id)
		}
	}

	return nil
}

// ValidateFeatureManifestYAML checks that exactly 49 groups are present and properly structured.
func ValidateFeatureManifestYAML(groups []FeatureGroup) error {
	if len(groups) != 49 {
		return fmt.Errorf("feature group count=%d, want 49", len(groups))
	}

	groupIDsSeen := make(map[int]bool)
	featureIDsSeen := make(map[string]int)

	for index, g := range groups {
		if g.GroupID != index+1 {
			return fmt.Errorf("group at position %d has group_id %d, want %d", index+1, g.GroupID, index+1)
		}
		if g.GroupID < 1 || g.GroupID > 49 {
			return fmt.Errorf("invalid group_id %d, must be 1-49", g.GroupID)
		}
		if groupIDsSeen[g.GroupID] {
			return fmt.Errorf("duplicate group_id %d", g.GroupID)
		}
		groupIDsSeen[g.GroupID] = true

		if g.Name == "" {
			return fmt.Errorf("group %d: missing name", g.GroupID)
		}
		if strings.TrimSpace(g.SourceProvenance) == "" {
			return fmt.Errorf("group %d: missing source_provenance", g.GroupID)
		}
		if strings.TrimSpace(g.CanonicalDigest) == "" {
			return fmt.Errorf("group %d: missing canonical_digest", g.GroupID)
		}
		if len(g.Features) == 0 {
			return fmt.Errorf("group %d: no features defined", g.GroupID)
		}

		seenFeatures := make(map[string]bool)
		for _, f := range g.Features {
			if f.FeatureID == "" || f.Label == "" || f.Category == "" {
				return fmt.Errorf("group %d: feature missing required fields", g.GroupID)
			}
			if seenFeatures[f.FeatureID] {
				return fmt.Errorf("group %d: duplicate feature_id %s", g.GroupID, f.FeatureID)
			}
			if priorGroup, exists := featureIDsSeen[f.FeatureID]; exists {
				return fmt.Errorf("duplicate feature_id %s in groups %d and %d", f.FeatureID, priorGroup, g.GroupID)
			}
			seenFeatures[f.FeatureID] = true
			featureIDsSeen[f.FeatureID] = g.GroupID

			// Validate category.
			validCategories := map[string]bool{"CREATE": true, "CHANGE": true, "CALCULATE": true, "OBSERVE": true}
			if !validCategories[f.Category] {
				return fmt.Errorf("group %d: invalid feature category %s", g.GroupID, f.Category)
			}

			// MappedIntentID must be valid: either an intent ID, "DEFERRED", or "MISSING", or comma-separated.
			if f.MappedIntentID == "" {
				return fmt.Errorf("group %d: feature %s missing mapped_intent_id", g.GroupID, f.FeatureID)
			}
			if f.MappedIntentID != "DEFERRED" && f.MappedIntentID != "MISSING" {
				parts := strings.Split(f.MappedIntentID, ",")
				for _, part := range parts {
					part = strings.TrimSpace(part)
					if !strings.HasPrefix(part, "hcmnext.") || !strings.Contains(part, "/v") || strings.HasSuffix(part, "/v") || part != strings.TrimSpace(part) {
						return fmt.Errorf("group %d: invalid mapped_intent_id %s", g.GroupID, part)
					}
				}
			}
		}
	}

	// Verify all group IDs 1-49 are present.
	for i := 1; i <= 49; i++ {
		if !groupIDsSeen[i] {
			return fmt.Errorf("missing group_id %d", i)
		}
	}

	return nil
}
