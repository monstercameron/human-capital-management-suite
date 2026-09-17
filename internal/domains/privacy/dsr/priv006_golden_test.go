package dsr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_PRIV_006_Golden is the GOLDEN matrix test for PRIV-006: a fixed
// intake/verify/resolve fixture produces a fixed, recorded certificate
// digest. The oracle lives in testdata/priv006_resolution.golden.txt; any
// deliberate change to Resolution.canonicalBytes needs that file updated in
// the same change, which is exactly the tamper-evidence property this test
// exists to enforce.
func TestTodo_PRIV_006_Golden(t *testing.T) {
	t.Run("a fixed erasure fixture's certificate digest is a stable, recorded value", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-erase-golden", KindErasure, trust.AssuranceHigh)
		res, err := Resolve(fixtureResolutionSpec(t, req, allRedClasses()))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		raw, err := os.ReadFile(filepath.Join("testdata", "priv006_resolution.golden.txt"))
		if err != nil {
			t.Fatalf("read golden oracle: %v", err)
		}
		want := "ev:privacy:dsr:resolution:" + strings.TrimSpace(string(raw))
		if res.EvidenceID != want {
			t.Errorf("EvidenceID = %q, want %q (recorded golden digest; update testdata/priv006_resolution.golden.txt deliberately if the canonical encoding intentionally changed)", res.EvidenceID, want)
		}
	})

	t.Run("the certificate digest is stable under input reordering", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-erase-order", KindErasure, trust.AssuranceHigh)
		forward, err := Resolve(fixtureResolutionSpec(t, req, allRedClasses()))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		reversed := allRedClasses()
		for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
			reversed[i], reversed[j] = reversed[j], reversed[i]
		}
		backward, err := Resolve(fixtureResolutionSpec(t, req, reversed))
		if err != nil {
			t.Fatalf("Resolve (reordered): %v", err)
		}
		if forward.Digest() != backward.Digest() {
			t.Errorf("reordered inputs digest differently: %q vs %q (item order must be canonicalized)", forward.Digest(), backward.Digest())
		}
	})
}
