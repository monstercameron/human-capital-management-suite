package intentmanifests

import (
	"path/filepath"
	"strings"
	"testing"
)

func loaderManifestPaths() (intent, feature string) {
	base := filepath.Join("..", "..", "..", "definitions", "governance")
	return filepath.Join(base, "intent-conformance-descriptors.yaml"),
		filepath.Join(base, "feature-intent-intake.yaml")
}

// TestLoadIntentManifest verifies the intent descriptor manifest loads correctly.
func TestLoadIntentManifest(t *testing.T) {
	path, _ := loaderManifestPaths()
	descriptors, err := LoadIntentManifestYAML(path)
	if err != nil {
		t.Fatalf("load intent manifest: %v", err)
	}
	if got := len(descriptors); got != 14 {
		t.Errorf("loaded descriptors count = %d, want 14", got)
	}
	for _, d := range descriptors {
		if !strings.HasPrefix(d.IntentTypeID, "hcmnext.") {
			t.Errorf("undrafted intent id %q", d.IntentTypeID)
		}
		if d.Version != 1 {
			t.Errorf("%s: version = %d, want 1", d.IntentTypeID, d.Version)
		}
	}
}

// TestLoadFeatureManifest verifies the feature intake manifest loads correctly.
func TestLoadFeatureManifest(t *testing.T) {
	_, path := loaderManifestPaths()
	groups, err := LoadFeatureManifestYAML(path)
	if err != nil {
		t.Fatalf("load feature manifest: %v", err)
	}
	if got := len(groups); got != 49 {
		t.Errorf("loaded groups count = %d, want 49", got)
	}
}

// TestIntentDescriptorStructure verifies the manifest contains all required fields.
func TestIntentDescriptorStructure(t *testing.T) {
	path, _ := loaderManifestPaths()
	descriptors, err := LoadIntentManifestYAML(path)
	if err != nil {
		t.Fatalf("load intent manifest: %v", err)
	}
	if err := ValidateIntentManifestYAML(descriptors); err != nil {
		t.Fatalf("validate intent manifest: %v", err)
	}
	for _, d := range descriptors {
		switch d.Family {
		case "CHANGE_REQUEST", "ANALYTICAL_REQUEST", "CALCULATION_REQUEST":
		default:
			t.Errorf("%s: unexpected family %q", d.IntentTypeID, d.Family)
		}
		switch d.SideEffectProfile {
		case "INTERNAL_MUTATION", "READ_ONLY", "PURE":
		default:
			t.Errorf("%s: unexpected side-effect profile %q", d.IntentTypeID, d.SideEffectProfile)
		}
		if len(d.Entities) == 0 || len(d.Properties) == 0 ||
			len(d.Reads) == 0 || len(d.Writes) == 0 ||
			len(d.Effects) == 0 || len(d.Authority) == 0 ||
			len(d.Time) == 0 || len(d.Evidence) == 0 {
			t.Errorf("%s: missing required dimension", d.IntentTypeID)
		}
		if d.Lifecycle.Request == "" || d.Lifecycle.Execution == "" ||
			d.Lifecycle.Business == "" || d.Lifecycle.Consistency == "" ||
			d.Lifecycle.Obligation == "" {
			t.Errorf("%s: incomplete lifecycle", d.IntentTypeID)
		}
		if len(d.NegativePolicy) == 0 {
			t.Errorf("%s: missing negative policy", d.IntentTypeID)
		}
		if d.Scenario.Name == "" || d.Scenario.Given == "" ||
			d.Scenario.When == "" || d.Scenario.Then == "" {
			t.Errorf("%s: incomplete scenario", d.IntentTypeID)
		}
		if d.ConformanceOnly && d.IntentTypeID != "hcmnext.people.change_manager" {
			t.Errorf("%s: only change_manager may be conformance_only", d.IntentTypeID)
		}
	}
}

// TestFeatureGroupStructure verifies the feature manifest contains all required fields.
func TestFeatureGroupStructure(t *testing.T) {
	_, path := loaderManifestPaths()
	groups, err := LoadFeatureManifestYAML(path)
	if err != nil {
		t.Fatalf("load feature manifest: %v", err)
	}
	if err := ValidateFeatureManifestYAML(groups); err != nil {
		t.Fatalf("validate feature manifest: %v", err)
	}
	seen := make(map[int]bool)
	for _, g := range groups {
		if g.GroupID < 1 || g.GroupID > 49 {
			t.Errorf("group_id %d out of range 1-49", g.GroupID)
		}
		if seen[g.GroupID] {
			t.Errorf("duplicate group_id %d", g.GroupID)
		}
		seen[g.GroupID] = true
		if g.Name == "" {
			t.Errorf("group %d: missing name", g.GroupID)
		}
		if len(g.Features) == 0 {
			t.Errorf("group %d: no features", g.GroupID)
		}
		for _, f := range g.Features {
			if f.FeatureID == "" || f.Label == "" || f.Category == "" {
				t.Errorf("group %d: feature missing required fields", g.GroupID)
			}
			switch f.Category {
			case "CREATE", "CHANGE", "CALCULATE", "OBSERVE":
			default:
				t.Errorf("group %d: invalid category %q", g.GroupID, f.Category)
			}
		}
	}
}
