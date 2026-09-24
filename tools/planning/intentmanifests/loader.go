// Package intentmanifests loads and validates the finite intent and feature manifests.
package intentmanifests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// IntentDescriptor represents one intent definition in the conformance manifest.
type IntentDescriptor struct {
	IntentTypeID      string                `yaml:"intent_type_id"`
	Version           int                   `yaml:"version"`
	DisplayName       string                `yaml:"display_name"`
	Family            string                `yaml:"family"`
	SideEffectProfile string                `yaml:"side_effect_profile"`
	Phase             string                `yaml:"phase"`
	Entities          []string              `yaml:"entities"`
	Properties        []string              `yaml:"properties"`
	Reads             []string              `yaml:"reads"`
	Writes            []string              `yaml:"writes"`
	Effects           []string              `yaml:"effects"`
	Authority         []string              `yaml:"authority"`
	Time              []string              `yaml:"time"`
	Lifecycle         LifecycleDescriptor   `yaml:"lifecycle"`
	Evidence          []string              `yaml:"evidence"`
	NegativePolicy    []NegativePolicyEntry `yaml:"negative_policy"`
	Scenario          ScenarioDescriptor    `yaml:"scenario"`
	ConformanceOnly   bool                  `yaml:"conformance_only,omitempty"`
}

type LifecycleDescriptor struct {
	Request     string `yaml:"request"`
	Execution   string `yaml:"execution"`
	Business    string `yaml:"business"`
	Consistency string `yaml:"consistency"`
	Obligation  string `yaml:"obligation"`
}

type NegativePolicyEntry struct {
	Condition   string `yaml:"condition"`
	Disposition string `yaml:"disposition"`
}

type ScenarioDescriptor struct {
	Name  string `yaml:"name"`
	Given string `yaml:"given"`
	When  string `yaml:"when"`
	Then  string `yaml:"then"`
}

// FeatureGroup represents one feature group in the intake manifest.
type FeatureGroup struct {
	GroupID          int       `yaml:"group_id"`
	Name             string    `yaml:"name"`
	Domain           string    `yaml:"domain,omitempty"`
	Features         []Feature `yaml:"features"`
	SourceProvenance string    `yaml:"source_provenance"`
	CanonicalDigest  string    `yaml:"canonical_digest"`
}

// Feature represents one named feature within a group.
type Feature struct {
	FeatureID      string `yaml:"feature_id"`
	Label          string `yaml:"label"`
	Category       string `yaml:"category"` // CREATE, CHANGE, CALCULATE, OBSERVE
	MappedIntentID string `yaml:"mapped_intent_id"`
}

// ValidateIntentManifest checks finite coverage and all mandatory dimensions.
func ValidateIntentManifest(descriptors []IntentDescriptor) error {
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

// ValidateFeatureManifest checks that exactly 49 groups are present and properly structured.
func ValidateFeatureManifest(groups []FeatureGroup) error {
	if len(groups) != 49 {
		return fmt.Errorf("feature group count=%d, want 49", len(groups))
	}

	seen := make(map[int]bool)
	groupIDsSeen := make(map[int]bool)

	for _, g := range groups {
		if g.GroupID < 1 || g.GroupID > 49 {
			return fmt.Errorf("invalid group_id %d, must be 1-49", g.GroupID)
		}
		if groupIDsSeen[g.GroupID] {
			return fmt.Errorf("duplicate group_id %d", g.GroupID)
		}
		groupIDsSeen[g.GroupID] = true
		seen[g.GroupID] = true

		if g.Name == "" {
			return fmt.Errorf("group %d: missing name", g.GroupID)
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
			seenFeatures[f.FeatureID] = true

			// Validate category.
			validCategories := map[string]bool{"CREATE": true, "CHANGE": true, "CALCULATE": true, "OBSERVE": true}
			if !validCategories[f.Category] {
				return fmt.Errorf("group %d: invalid feature category %s", g.GroupID, f.Category)
			}

			// MappedIntentID must be valid: either an intent ID, "DEFERRED", or "MISSING".
			if f.MappedIntentID != "DEFERRED" && f.MappedIntentID != "MISSING" && f.MappedIntentID != "" {
				// Check if it's a valid intent reference (could be comma-separated).
				parts := strings.Split(f.MappedIntentID, ",")
				for _, part := range parts {
					part = strings.TrimSpace(part)
					if !strings.HasPrefix(part, "hcmnext.") || !strings.Contains(part, "/v") {
						return fmt.Errorf("group %d: invalid mapped_intent_id %s", g.GroupID, part)
					}
				}
			}
		}
	}

	// Verify all group IDs 1-49 are present.
	for i := 1; i <= 49; i++ {
		if !seen[i] {
			return fmt.Errorf("missing group_id %d", i)
		}
	}

	return nil
}

// ComputeIntentDigest returns a stable SHA256 digest of the intent descriptors.
func ComputeIntentDigest(descriptors []IntentDescriptor) (string, error) {
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

// ComputeFeatureDigest returns a stable SHA256 digest of the feature groups.
func ComputeFeatureDigest(groups []FeatureGroup) (string, error) {
	cpy := append([]FeatureGroup(nil), groups...)
	sort.Slice(cpy, func(i, j int) bool { return cpy[i].GroupID < cpy[j].GroupID })

	b, err := json.Marshal(cpy)
	if err != nil {
		return "", err
	}

	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
