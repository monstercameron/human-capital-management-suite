package pilotjurisdiction

import (
	"encoding/json"
	"os"
	"testing"
)

type goldenFixture struct {
	CanonicalDigest string `json:"canonical_digest"`
	SignatureValue  string `json:"signature_value"`
	Counts          struct {
		Sources                int `json:"sources"`
		ScopeItems             int `json:"scope_items"`
		ObligationMappings     int `json:"obligation_mappings"`
		Exclusions             int `json:"exclusions"`
		StopReselectThresholds int `json:"stop_reselect_thresholds"`
	} `json:"counts"`
	ObligationMappingOrder []string `json:"obligation_mapping_order"`
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

// TestTodo_SELECT_001_Golden is SELECT-001's GOLDEN test. It pins the exact
// canonical digest, signature and category counts of the checked-in
// definitions/planning/gates/select-001-jurisdiction-profile.yaml, so any
// future hand edit that changes its meaning - not just its formatting - is
// caught here even if every other test still passes.
func TestTodo_SELECT_001_Golden(t *testing.T) {
	p := mustLoadProfile(t)
	golden := mustLoadGolden(t)

	digest, err := p.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	if digest != golden.CanonicalDigest {
		t.Errorf("canonical digest = %s, want %s (pinned in testdata/golden.json)", digest, golden.CanonicalDigest)
	}
	if p.Signature == nil || p.Signature.Value != golden.SignatureValue {
		t.Errorf("signature.value = %v, want %s", p.Signature, golden.SignatureValue)
	}

	gotCounts := map[string]int{
		"sources":                  len(p.Sources),
		"scope_items":              len(p.ScopeItems),
		"obligation_mappings":      len(p.ObligationMappings),
		"exclusions":               len(p.Exclusions),
		"stop_reselect_thresholds": len(p.StopReselectThresholds),
	}
	wantCounts := map[string]int{
		"sources":                  golden.Counts.Sources,
		"scope_items":              golden.Counts.ScopeItems,
		"obligation_mappings":      golden.Counts.ObligationMappings,
		"exclusions":               golden.Counts.Exclusions,
		"stop_reselect_thresholds": golden.Counts.StopReselectThresholds,
	}
	for k, want := range wantCounts {
		if got := gotCounts[k]; got != want {
			t.Errorf("category %s has %d items, want %d (pinned)", k, got, want)
		}
	}

	if len(p.ObligationMappings) != len(golden.ObligationMappingOrder) {
		t.Fatalf("got %d obligation mappings, golden pins %d", len(p.ObligationMappings), len(golden.ObligationMappingOrder))
	}
	for i, want := range golden.ObligationMappingOrder {
		if p.ObligationMappings[i].Kind != want {
			t.Errorf("obligation_mappings[%d] = %s, want %s (pinned order)", i, p.ObligationMappings[i].Kind, want)
		}
	}
}
