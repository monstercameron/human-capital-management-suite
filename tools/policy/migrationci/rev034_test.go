package migrationci

import (
	"errors"
	"testing"
)

// TestTodo_REV_034_01_Recovery proves the rehearsal recovers: an aborted
// contract fences the upgrade, and clearing the abort on the same manifest
// and watermark passes without rebuilding anything.
func TestTodo_REV_034_01_Recovery(t *testing.T) {
	aborted := passingInput()
	aborted.AbortRequested = true
	result, err := Rehearse(aborted)
	if !errors.Is(err, ErrRehearsalFailed) {
		t.Fatalf("aborted rehearse err = %v, want ErrRehearsalFailed", err)
	}
	fenced := false
	for _, finding := range result.Findings {
		if finding.Code == "ABORTED_BEFORE_CONTRACT" {
			fenced = true
		}
	}
	if !fenced {
		t.Fatalf("aborted result has no ABORTED_BEFORE_CONTRACT finding: %+v", result)
	}
	cleared := passingInput()
	cleared.AbortRequested = false
	recovered, err := Rehearse(cleared)
	if err != nil {
		t.Fatalf("rehearse after clearing abort: %v", err)
	}
	if recovered.Status != "PASS" {
		t.Fatalf("recovered status = %s, want PASS", recovered.Status)
	}
	if recovered.ManifestDigest != result.ManifestDigest {
		t.Fatalf("recovered digest %s differs from fenced digest %s: same manifest must digest identically",
			recovered.ManifestDigest, result.ManifestDigest)
	}
}
