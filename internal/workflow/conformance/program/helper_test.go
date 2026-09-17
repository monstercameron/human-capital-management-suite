package program

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// seededDigest mints a well-formed deterministic digest for fixtures. It is
// test-only scaffolding: the sealed report digest is computed by the
// package under test, never by this helper.
func seededDigest(seed string) string {
	sum := sha256.Sum256([]byte("program-conf-fixture\x00" + seed))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(".", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return string(raw)
}
