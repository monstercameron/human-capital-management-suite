package pilotcommercial

import (
	"encoding/json"
	"os"
	"testing"
)

type goldenFixture struct {
	CanonicalDigest string `json:"canonical_digest"`
	SignatureValue  string `json:"signature_value"`
	Counts          struct {
		Entitlements     int `json:"entitlements"`
		Exclusions       int `json:"exclusions"`
		EvidenceCaptured int `json:"evidence_captured"`
		ObservedDomains  int `json:"observed_domains"`
		ForbiddenEffects int `json:"forbidden_effects"`
		ExportFormats    int `json:"export_formats"`
		ExportIncludes   int `json:"export_includes"`
	} `json:"counts"`
	EntitlementIDOrder []string `json:"entitlement_id_order"`
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

// TestTodo_COMMERCIAL_001_Golden is COMMERCIAL-001's GOLDEN test. It pins
// the exact canonical digest, signature and category counts/order of the
// checked-in definitions/planning/gates/commercial-001-pilot-package.yaml,
// so any future hand edit that changes its meaning - not just its
// formatting - is caught here even if every other test still passes.
func TestTodo_COMMERCIAL_001_Golden(t *testing.T) {
	freeze := mustLoadFreeze(t)
	golden := mustLoadGolden(t)

	digest, err := freeze.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	if digest != golden.CanonicalDigest {
		t.Errorf("canonical digest = %s, want %s (pinned in testdata/golden.json)", digest, golden.CanonicalDigest)
	}
	if freeze.Signature == nil || freeze.Signature.Value != golden.SignatureValue {
		t.Errorf("signature.value = %v, want %s", freeze.Signature, golden.SignatureValue)
	}

	gotCounts := map[string]int{
		"entitlements":      len(freeze.Intent.Entitlements),
		"exclusions":        len(freeze.Intent.Exclusions),
		"evidence_captured": len(freeze.Evidence.EvidenceCaptured),
		"observed_domains":  len(freeze.Authority.ObservedDomains),
		"forbidden_effects": len(freeze.Authority.ForbiddenEffects),
		"export_formats":    len(freeze.Exit.ExportFormats),
		"export_includes":   len(freeze.Exit.ExportIncludes),
	}
	wantCounts := map[string]int{
		"entitlements":      golden.Counts.Entitlements,
		"exclusions":        golden.Counts.Exclusions,
		"evidence_captured": golden.Counts.EvidenceCaptured,
		"observed_domains":  golden.Counts.ObservedDomains,
		"forbidden_effects": golden.Counts.ForbiddenEffects,
		"export_formats":    golden.Counts.ExportFormats,
		"export_includes":   golden.Counts.ExportIncludes,
	}
	for k, want := range wantCounts {
		if got := gotCounts[k]; got != want {
			t.Errorf("category %s has %d items, want %d (pinned)", k, got, want)
		}
	}

	if len(freeze.Intent.Entitlements) != len(golden.EntitlementIDOrder) {
		t.Fatalf("got %d entitlements, golden pins %d", len(freeze.Intent.Entitlements), len(golden.EntitlementIDOrder))
	}
	for i, want := range golden.EntitlementIDOrder {
		if freeze.Intent.Entitlements[i].IntentID != want {
			t.Errorf("entitlements[%d].intent_id = %s, want %s (pinned order)", i, freeze.Intent.Entitlements[i].IntentID, want)
		}
	}
}
