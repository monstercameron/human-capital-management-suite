package pilotblueprint

import (
	"encoding/json"
	"os"
	"testing"
)

type goldenFixture struct {
	CanonicalDigest string `json:"canonical_digest"`
	SignatureValue  string `json:"signature_value"`
	Counts          struct {
		Workstreams int `json:"workstreams"`
	} `json:"counts"`
	WorkstreamOrder []string `json:"workstream_order"`
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

// TestTodo_CUSTOMER_001_Golden is CUSTOMER-001's GOLDEN test. It pins the
// exact canonical digest, signature and workstream order of the checked-in
// definitions/planning/gates/customer-001-pilot-blueprint.yaml, so any
// future hand edit that changes its meaning - not just its formatting - is
// caught here even if every other test still passes.
func TestTodo_CUSTOMER_001_Golden(t *testing.T) {
	bp := mustLoadBlueprint(t)
	golden := mustLoadGolden(t)

	digest, err := bp.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	if digest != golden.CanonicalDigest {
		t.Errorf("canonical digest = %s, want %s (pinned in testdata/golden.json)", digest, golden.CanonicalDigest)
	}
	if bp.Signature == nil || bp.Signature.Value != golden.SignatureValue {
		t.Errorf("signature.value = %v, want %s", bp.Signature, golden.SignatureValue)
	}

	if len(bp.Workstreams) != golden.Counts.Workstreams {
		t.Errorf("workstreams count = %d, want %d (pinned)", len(bp.Workstreams), golden.Counts.Workstreams)
	}
	if len(bp.Workstreams) != len(golden.WorkstreamOrder) {
		t.Fatalf("got %d workstreams, golden pins %d", len(bp.Workstreams), len(golden.WorkstreamOrder))
	}
	for i, want := range golden.WorkstreamOrder {
		if string(bp.Workstreams[i].Kind) != want {
			t.Errorf("workstreams[%d].kind = %s, want %s (pinned order)", i, bp.Workstreams[i].Kind, want)
		}
	}
}
