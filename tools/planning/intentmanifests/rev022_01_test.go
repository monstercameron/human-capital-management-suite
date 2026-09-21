package intentmanifests

import (
	"path/filepath"
	"strings"
	"testing"
)

func rev022ManifestPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "definitions", "governance", "intent-conformance-descriptors.yaml")
}

func rev022LoadValidated(t *testing.T) []IntentDescriptor {
	t.Helper()
	descriptors, err := LoadIntentManifestYAML(rev022ManifestPath(t))
	if err != nil {
		t.Fatalf("load intent manifest: %v", err)
	}
	if err := ValidateIntentManifestYAML(descriptors); err != nil {
		t.Fatalf("validate intent manifest: %v", err)
	}
	return descriptors
}

// TestTodo_REV_022_01 is the PRIMARY test for REV-022-01: the cited
// INTENT-CONF-001 evidence tests must assert against the real loader and
// validator instead of statting the file and logging.
func TestTodo_REV_022_01(t *testing.T) {
	descriptors, err := LoadIntentManifestYAML(rev022ManifestPath(t))
	if err != nil {
		t.Fatalf("load intent manifest: %v", err)
	}
	if err := ValidateIntentManifestYAML(descriptors); err != nil {
		t.Fatalf("validate intent manifest: %v", err)
	}
	if got := len(descriptors); got != 14 {
		t.Fatalf("intent descriptors count = %d, want 14", got)
	}
	dims := []string{"entities", "properties", "reads", "writes", "effects", "authority", "time", "evidence"}
	for _, d := range descriptors {
		if d.IntentTypeID == "" || d.DisplayName == "" || d.Family == "" {
			t.Errorf("%s: missing core fields", d.IntentTypeID)
		}
		if d.Version != 1 {
			t.Errorf("%s: version = %d, want 1", d.IntentTypeID, d.Version)
		}
		got := map[string]int{
			"entities": len(d.Entities), "properties": len(d.Properties),
			"reads": len(d.Reads), "writes": len(d.Writes),
			"effects": len(d.Effects), "authority": len(d.Authority),
			"time": len(d.Time), "evidence": len(d.Evidence),
		}
		for _, dim := range dims {
			if got[dim] == 0 {
				t.Errorf("%s: dimension %s is empty", d.IntentTypeID, dim)
			}
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
	found := false
	for _, d := range descriptors {
		if d.IntentTypeID == "hcmnext.people.change_manager" {
			found = true
			if !d.ConformanceOnly {
				t.Error("change_manager must be marked conformance_only")
			}
		}
	}
	if !found {
		t.Error("change_manager descriptor missing")
	}
}

// TestTodo_REV_022_01_Conformance asserts determinism, that change_manager is
// the only conformance-only row, and that no undrafted intent ids are present.
func TestTodo_REV_022_01_Conformance(t *testing.T) {
	a, err := LoadIntentManifestYAML(rev022ManifestPath(t))
	if err != nil {
		t.Fatalf("load manifest (1): %v", err)
	}
	b, err := LoadIntentManifestYAML(rev022ManifestPath(t))
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
	for _, d := range a {
		if !strings.HasPrefix(d.IntentTypeID, "hcmnext.") {
			t.Errorf("undrafted intent: %s", d.IntentTypeID)
		}
		if d.ConformanceOnly && d.IntentTypeID != "hcmnext.people.change_manager" {
			t.Errorf("non-change_manager row marked conformance_only: %s", d.IntentTypeID)
		}
	}
	if err := ValidateIntentManifestYAML(a); err != nil {
		t.Fatalf("live manifest fails validation: %v", err)
	}
}

// TestTodo_REV_022_01_Mutation proves the validator rejects a stripped
// dimension, an unmarked change_manager, and an undrafted intent id.
func TestTodo_REV_022_01_Mutation(t *testing.T) {
	base := rev022LoadValidated(t)
	clone := func() []IntentDescriptor {
		out := append([]IntentDescriptor(nil), base...)
		return out
	}

	stripped := clone()
	stripped[0].Entities = nil
	if err := ValidateIntentManifestYAML(stripped); err == nil {
		t.Error("validator accepted a descriptor with a stripped entities dimension")
	}

	unmarked := clone()
	for i := range unmarked {
		if unmarked[i].IntentTypeID == "hcmnext.people.change_manager" {
			unmarked[i].ConformanceOnly = false
		}
	}
	if err := ValidateIntentManifestYAML(unmarked); err == nil {
		t.Error("validator accepted change_manager without its conformance_only marker")
	}

	undrafted := clone()
	undrafted[0].IntentTypeID = "hcmnext.unplanned.sneaky_intent"
	if err := ValidateIntentManifestYAML(undrafted); err == nil {
		t.Error("validator accepted an undrafted intent id")
	}
}
