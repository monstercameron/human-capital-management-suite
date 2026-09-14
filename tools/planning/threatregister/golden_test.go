package threatregister

import (
	"encoding/json"
	"os"
	"testing"
)

type goldenFixture struct {
	CanonicalDigest string `json:"canonical_digest"`
	SignatureValue  string `json:"signature_value"`
	Counts          struct {
		Actors          int `json:"actors"`
		Assets          int `json:"assets"`
		TrustBoundaries int `json:"trust_boundaries"`
		EntryPoints     int `json:"entry_points"`
		Edges           int `json:"edges"`
		Threats         int `json:"threats"`
		Mitigations     int `json:"mitigations"`
		ResidualRisks   int `json:"residual_risks"`
	} `json:"counts"`
	AttackClassOrder []string `json:"attack_class_order"`
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

// TestTodo_THREAT_001_Golden pins the exact canonical digest, signature and
// category counts/order of the checked-in
// definitions/planning/gates/threat-001-register.yaml, so any future hand
// edit that changes its meaning - not just its formatting - is caught here
// even if every other test still passes.
func TestTodo_THREAT_001_Golden(t *testing.T) {
	r := mustLoadRegister(t)
	golden := mustLoadGolden(t)

	digest, err := r.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	if digest != golden.CanonicalDigest {
		t.Errorf("canonical digest = %s, want %s (pinned in testdata/golden.json)", digest, golden.CanonicalDigest)
	}
	if r.Signature == nil || r.Signature.Value != golden.SignatureValue {
		t.Errorf("signature.value = %v, want %s", r.Signature, golden.SignatureValue)
	}

	if len(r.Slices) != 1 {
		t.Fatalf("golden pins exactly one slice, got %d", len(r.Slices))
	}
	s := r.Slices[0]

	gotCounts := map[string]int{
		"actors":           len(s.Actors),
		"assets":           len(s.Assets),
		"trust_boundaries": len(s.TrustBoundaries),
		"entry_points":     len(s.EntryPoints),
		"edges":            len(s.Edges),
		"threats":          len(s.Threats),
		"mitigations":      len(s.Mitigations),
		"residual_risks":   len(s.ResidualRisks),
	}
	wantCounts := map[string]int{
		"actors":           golden.Counts.Actors,
		"assets":           golden.Counts.Assets,
		"trust_boundaries": golden.Counts.TrustBoundaries,
		"entry_points":     golden.Counts.EntryPoints,
		"edges":            golden.Counts.Edges,
		"threats":          golden.Counts.Threats,
		"mitigations":      golden.Counts.Mitigations,
		"residual_risks":   golden.Counts.ResidualRisks,
	}
	for k, want := range wantCounts {
		if got := gotCounts[k]; got != want {
			t.Errorf("category %s has %d items, want %d (pinned)", k, got, want)
		}
	}

	if len(s.Threats) != len(golden.AttackClassOrder) {
		t.Fatalf("got %d threats, golden pins %d", len(s.Threats), len(golden.AttackClassOrder))
	}
	for i, want := range golden.AttackClassOrder {
		if s.Threats[i].AttackClass != want {
			t.Errorf("threats[%d].attack_class = %s, want %s (pinned order)", i, s.Threats[i].AttackClass, want)
		}
	}
}
