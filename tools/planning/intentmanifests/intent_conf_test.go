package intentmanifests

import (
	"path/filepath"
	"strings"
	"testing"
)

func intentConfManifestPath() string {
	return filepath.Join("..", "..", "..", "definitions", "governance", "intent-conformance-descriptors.yaml")
}

// TestTodo_INTENT_CONF_001 is the PRIMARY test for INTENT-CONF-001.
// It verifies the intent conformance descriptor manifest has exactly fourteen
// rows with all mandatory dimensions and a stable digest.
func TestTodo_INTENT_CONF_001(t *testing.T) {
	// Load the intent conformance manifest from the definitions tree.
	descriptors, err := LoadIntentManifestYAML(intentConfManifestPath())
	if err != nil {
		t.Fatalf("load intent manifest: %v", err)
	}

	// Validate the manifest.
	if err := ValidateIntentManifestYAML(descriptors); err != nil {
		t.Fatalf("validate intent manifest: %v", err)
	}

	// Verify exactly 14 descriptors.
	if got := len(descriptors); got != 14 {
		t.Fatalf("intent descriptors count = %d, want 14", got)
	}

	// Verify all required fields are present and non-empty.
	for _, d := range descriptors {
		if d.IntentTypeID == "" || d.DisplayName == "" || d.Family == "" {
			t.Errorf("%s: missing core fields", d.IntentTypeID)
		}
		if d.Version != 1 {
			t.Errorf("%s: version = %d, want 1", d.IntentTypeID, d.Version)
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
			t.Errorf("%s: incomplete five-dimension lifecycle", d.IntentTypeID)
		}
		if len(d.NegativePolicy) == 0 ||
			d.Scenario.Name == "" || d.Scenario.Given == "" ||
			d.Scenario.When == "" || d.Scenario.Then == "" {
			t.Errorf("%s: missing scenario or negative-policy matrix", d.IntentTypeID)
		}
	}

	// Verify change_manager is marked conformance-only.
	for _, d := range descriptors {
		if d.IntentTypeID == "hcmnext.people.change_manager" && !d.ConformanceOnly {
			t.Error("change_manager must be marked conformance_only")
		}
	}

	// Compute and verify the manifest digest.
	digest, err := ComputeIntentDigestYAML(descriptors)
	if err != nil {
		t.Fatalf("compute digest: %v", err)
	}
	if len(digest) != 64 {
		t.Errorf("digest length = %d, want 64 (SHA256)", len(digest))
	}
}

// goldenIntentDigest pins the SHA256 digest of the sorted canonical JSON of
// definitions/governance/intent-conformance-descriptors.yaml. Regenerate by
// running TestTodo_INTENT_CONF_001 and copying the logged digest after a
// reviewed manifest change.
const goldenIntentDigest = "a746ad7796da6bcf28db35f43a21fd9b4b0a593ab4d3ba9cf64e53fdc38af37b"

// TestTodo_INTENT_CONF_001_Golden pins the manifest digest and conformance markers.
func TestTodo_INTENT_CONF_001_Golden(t *testing.T) {
	descriptors, err := LoadIntentManifestYAML(intentConfManifestPath())
	if err != nil {
		t.Fatalf("load intent manifest: %v", err)
	}

	digest, err := ComputeIntentDigestYAML(descriptors)
	if err != nil {
		t.Fatalf("compute digest: %v", err)
	}

	if digest != goldenIntentDigest {
		t.Errorf("manifest digest = %q, want golden %q", digest, goldenIntentDigest)
	}

	// Verify change_manager conformance marker persists.
	for _, d := range descriptors {
		if d.IntentTypeID == "hcmnext.people.change_manager" && !d.ConformanceOnly {
			t.Error("golden: change_manager lost conformance_only marker")
		}
	}
}

// TestTodo_INTENT_CONF_001_Conformance proves manifest is deterministic with no undrafted intents.
func TestTodo_INTENT_CONF_001_Conformance(t *testing.T) {
	a, err := LoadIntentManifestYAML(intentConfManifestPath())
	if err != nil {
		t.Fatalf("load manifest (1): %v", err)
	}

	b, err := LoadIntentManifestYAML(intentConfManifestPath())
	if err != nil {
		t.Fatalf("load manifest (2): %v", err)
	}

	da, err := ComputeIntentDigestYAML(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := ComputeIntentDigestYAML(b)
	if err != nil {
		t.Fatal(err)
	}

	if da != db {
		t.Errorf("manifest digest not deterministic: %q vs %q", da, db)
	}

	// Verify no undrafted intents are present.
	for _, d := range a {
		if !strings.HasPrefix(d.IntentTypeID, "hcmnext.") {
			t.Errorf("undrafted intent: %s", d.IntentTypeID)
		}
	}
}

// TestTodo_INTENT_CONF_001_Mutation ensures each mandatory contract class is enforced.
func TestTodo_INTENT_CONF_001_Mutation(t *testing.T) {
	descriptors, err := LoadIntentManifestYAML(intentConfManifestPath())
	if err != nil {
		t.Fatalf("load intent manifest: %v", err)
	}
	if err := ValidateIntentManifestYAML(descriptors); err != nil {
		t.Fatalf("live manifest must validate before mutation: %v", err)
	}
	clone := func() []IntentDescriptor {
		return append([]IntentDescriptor(nil), descriptors...)
	}
	victim := -1
	for i := range descriptors {
		if descriptors[i].IntentTypeID == "hcmnext.rewards.change_base_pay" {
			victim = i
		}
	}
	if victim < 0 {
		t.Fatal("change_base_pay descriptor missing from live manifest")
	}

	// Verify validation rejects an undrafted intent name.
	undrafted := clone()
	undrafted[victim].IntentTypeID = "acme.unplanned.shadow_intent"
	if err := ValidateIntentManifestYAML(undrafted); err == nil {
		t.Error("validator accepted an undrafted intent name")
	}

	// Verify validation rejects missing entities, authority, and lifecycle dimensions.
	noEntities := clone()
	noEntities[victim].Entities = nil
	if err := ValidateIntentManifestYAML(noEntities); err == nil {
		t.Error("validator accepted a descriptor with missing entities")
	}
	noAuthority := clone()
	noAuthority[victim].Authority = nil
	if err := ValidateIntentManifestYAML(noAuthority); err == nil {
		t.Error("validator accepted a descriptor with missing authority")
	}
	noLifecycle := clone()
	noLifecycle[victim].Lifecycle.Execution = ""
	if err := ValidateIntentManifestYAML(noLifecycle); err == nil {
		t.Error("validator accepted a descriptor with a missing lifecycle dimension")
	}

	// Verify validation rejects a missing negative policy or scenario.
	noPolicy := clone()
	noPolicy[victim].NegativePolicy = nil
	if err := ValidateIntentManifestYAML(noPolicy); err == nil {
		t.Error("validator accepted a descriptor with missing negative policy")
	}
	noScenario := clone()
	noScenario[victim].Scenario.Then = ""
	if err := ValidateIntentManifestYAML(noScenario); err == nil {
		t.Error("validator accepted a descriptor with an incomplete scenario")
	}

	// Verify validation rejects a wrong family/side-effect combination for
	// analytical or calculation requests.
	badFamily := clone()
	for i := range badFamily {
		if badFamily[i].Family == "ANALYTICAL_REQUEST" {
			badFamily[i].SideEffectProfile = "INTERNAL_MUTATION"
			break
		}
	}
	if err := ValidateIntentManifestYAML(badFamily); err == nil {
		t.Error("validator accepted an analytical request with mutating side effects")
	}

	// Verify validation rejects non-conformance-only descriptors for change_manager.
	unmarked := clone()
	for i := range unmarked {
		if unmarked[i].IntentTypeID == "hcmnext.people.change_manager" {
			unmarked[i].ConformanceOnly = false
		}
	}
	if err := ValidateIntentManifestYAML(unmarked); err == nil {
		t.Error("validator accepted change_manager without conformance_only")
	}
}
