package scopeceiling

import (
	"encoding/json"
	"os"
	"testing"
)

type goldenFixture struct {
	CanonicalDigest string `json:"canonical_digest"`
	SignatureValue  string `json:"signature_value"`
	Counts          struct {
		Intents         int `json:"intents"`
		Capabilities    int `json:"capabilities"`
		Workflows       int `json:"workflows"`
		UserFlows       int `json:"user_flows"`
		Endpoints       int `json:"endpoints"`
		Models          int `json:"models"`
		Effects         int `json:"effects"`
		SelectionSlots  int `json:"selection_slots"`
		DeferredDomains int `json:"deferred_domains"`
	} `json:"counts"`
	IntentOrder []string `json:"intent_order"`
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

// TestTodo_PHASE_001_Golden is PHASE-001's GOLDEN test. It pins the exact
// canonical digest, signature and category counts of the checked-in
// definitions/planning/gates/phase1-scope-ceiling.yaml, so any future hand
// edit that changes its meaning - not just its formatting - is caught here
// even if every other test still passes.
func TestTodo_PHASE_001_Golden(t *testing.T) {
	m := mustLoadCeiling(t)
	golden := mustLoadGolden(t)

	digest, err := m.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	if digest != golden.CanonicalDigest {
		t.Errorf("canonical digest = %s, want %s (pinned in testdata/golden.json)", digest, golden.CanonicalDigest)
	}
	if m.Signature == nil || m.Signature.Value != golden.SignatureValue {
		t.Errorf("signature.value = %v, want %s", m.Signature, golden.SignatureValue)
	}

	gotCounts := map[string]int{
		"intents":          len(m.Intents),
		"capabilities":     len(m.Capabilities),
		"workflows":        len(m.Workflows),
		"user_flows":       len(m.UserFlows),
		"endpoints":        len(m.Endpoints),
		"models":           len(m.Models),
		"effects":          len(m.Effects),
		"selection_slots":  len(m.SelectionSlots),
		"deferred_domains": len(m.DeferredDomains),
	}
	wantCounts := map[string]int{
		"intents":          golden.Counts.Intents,
		"capabilities":     golden.Counts.Capabilities,
		"workflows":        golden.Counts.Workflows,
		"user_flows":       golden.Counts.UserFlows,
		"endpoints":        golden.Counts.Endpoints,
		"models":           golden.Counts.Models,
		"effects":          golden.Counts.Effects,
		"selection_slots":  golden.Counts.SelectionSlots,
		"deferred_domains": golden.Counts.DeferredDomains,
	}
	for k, want := range wantCounts {
		if got := gotCounts[k]; got != want {
			t.Errorf("category %s has %d items, want %d (pinned)", k, got, want)
		}
	}

	if len(m.Intents) != len(golden.IntentOrder) {
		t.Fatalf("got %d intents, golden pins %d", len(m.Intents), len(golden.IntentOrder))
	}
	for i, want := range golden.IntentOrder {
		if m.Intents[i].ID != want {
			t.Errorf("intents[%d] = %s, want %s (pinned order)", i, m.Intents[i].ID, want)
		}
	}
}
