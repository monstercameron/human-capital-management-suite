package privacy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTodo_PRIV_009_Golden is the GOLDEN matrix test for PRIV-009: the
// fixture incident's decision digest pins to the recorded value in
// testdata/priv009.golden.txt. Any deliberate change to the canonical
// encodings needs that file updated in the same change.
func TestTodo_PRIV_009_Golden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "priv009.golden.txt"))
	if err != nil {
		t.Fatalf("read golden oracle: %v", err)
	}
	want := strings.TrimSpace(string(raw))

	decision, err := DecideNotifications(fixtureBreachIncident(t), fixtureNotificationMatrix(t), mustInstant(t, fxBreachDecidedAt))
	if err != nil {
		t.Fatalf("DecideNotifications: %v", err)
	}
	if got := decision.Digest(); got != want {
		t.Errorf("decision digest = %q, want recorded %q", got, want)
	}
	if got := decision.EvidenceID; got != breachEvidencePrefix+want {
		t.Errorf("EvidenceID = %q, want it bound to the recorded digest", got)
	}
}
