package privacy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTodo_PRIV_008_Golden is the GOLDEN matrix test for PRIV-008: the
// fixture boundary's classification digest and disclosure-log head digest
// pin to recorded values in testdata/priv008.golden.txt (one hex digest
// per line). Any deliberate change to the canonical encodings needs that
// file updated in the same change.
func TestTodo_PRIV_008_Golden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "priv008.golden.txt"))
	if err != nil {
		t.Fatalf("read golden oracle: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("golden oracle has %d lines, want 2 (classification digest, log head digest)", len(lines))
	}

	boundary := fixtureFTIBoundary(t)

	if got := boundary.Classification.Digest(); got != strings.TrimSpace(lines[0]) {
		t.Errorf("classification digest = %q, want recorded %q", got, strings.TrimSpace(lines[0]))
	}
	if err := boundary.Log.Verify(); err != nil {
		t.Fatalf("fixture log does not verify: %v", err)
	}
	if got := boundary.Log.HeadDigest; got != strings.TrimSpace(lines[1]) {
		t.Errorf("log head digest = %q, want recorded %q", got, strings.TrimSpace(lines[1]))
	}
}
