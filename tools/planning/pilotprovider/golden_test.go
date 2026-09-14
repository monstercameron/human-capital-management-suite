package pilotprovider

import (
	"encoding/json"
	"os"
	"testing"
)

type goldenFixture struct {
	CanonicalDigest string `json:"canonical_digest"`
	SignatureValue  string `json:"signature_value"`
	Counts          struct {
		APIEntitlements        int `json:"api_entitlements"`
		FieldAuthorities       int `json:"field_authorities"`
		Operations             int `json:"operations"`
		Faults                 int `json:"faults"`
		StopReselectThresholds int `json:"stop_reselect_thresholds"`
	} `json:"counts"`
	OperationVerbOrder []string `json:"operation_verb_order"`
	FaultClassOrder    []string `json:"fault_class_order"`
}

func mustLoadGolden(t *testing.T) goldenFixture {
	t.Helper()
	b, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatalf("read testdata/golden.json: %v", err)
	}
	var g goldenFixture
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatalf("parse testdata/golden.json: %v", err)
	}
	return g
}

// TestTodo_SELECT_002_Golden is SELECT-002's GOLDEN test. It pins the exact
// canonical digest, signature and category counts/order of the checked-in
// definitions/planning/gates/select-002-provider-topology.yaml, so any
// future hand edit that changes its meaning - not just its formatting - is
// caught here even if every other test still passes.
func TestTodo_SELECT_002_Golden(t *testing.T) {
	topology := mustLoadTopology(t)
	golden := mustLoadGolden(t)

	digest, err := topology.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	if digest != golden.CanonicalDigest {
		t.Errorf("canonical digest = %s, want %s (pinned in testdata/golden.json)", digest, golden.CanonicalDigest)
	}
	if topology.Signature == nil || topology.Signature.Value != golden.SignatureValue {
		t.Errorf("signature.value = %v, want %s", topology.Signature, golden.SignatureValue)
	}

	gotCounts := map[string]int{
		"api_entitlements":         len(topology.APIEntitlements),
		"field_authorities":        len(topology.FieldAuthorities),
		"operations":               len(topology.Operations),
		"faults":                   len(topology.Faults),
		"stop_reselect_thresholds": len(topology.StopReselectThresholds),
	}
	wantCounts := map[string]int{
		"api_entitlements":         golden.Counts.APIEntitlements,
		"field_authorities":        golden.Counts.FieldAuthorities,
		"operations":               golden.Counts.Operations,
		"faults":                   golden.Counts.Faults,
		"stop_reselect_thresholds": golden.Counts.StopReselectThresholds,
	}
	for k, want := range wantCounts {
		if got := gotCounts[k]; got != want {
			t.Errorf("category %s has %d items, want %d (pinned)", k, got, want)
		}
	}

	if len(topology.Operations) != len(golden.OperationVerbOrder) {
		t.Fatalf("got %d operations, golden pins %d", len(topology.Operations), len(golden.OperationVerbOrder))
	}
	for i, want := range golden.OperationVerbOrder {
		if topology.Operations[i].Verb != want {
			t.Errorf("operations[%d].verb = %s, want %s (pinned order)", i, topology.Operations[i].Verb, want)
		}
	}

	if len(topology.Faults) != len(golden.FaultClassOrder) {
		t.Fatalf("got %d faults, golden pins %d", len(topology.Faults), len(golden.FaultClassOrder))
	}
	for i, want := range golden.FaultClassOrder {
		if topology.Faults[i].Class != want {
			t.Errorf("faults[%d].class = %s, want %s (pinned order)", i, topology.Faults[i].Class, want)
		}
	}
}
