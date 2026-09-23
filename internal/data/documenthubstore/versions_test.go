package documenthubstore

import (
	"errors"
	"strings"
	"testing"
)

// TestTodo_HUB_004 is the PRIMARY test for HUB-004: stored versions preserve
// normalized Markdown with a deterministic content hash.
func TestTodo_HUB_004(t *testing.T) {
	if got := NormalizeMarkdown("a\r\nb\r\n"); got != "a\nb\n" {
		t.Fatalf("line endings not canonicalized: %q", got)
	}
	if NormalizeMarkdown("# T\n\nbody\n") == "# T\n\nbody  \n" {
		t.Fatal("normalization must not equate distinct Markdown")
	}
	h1, h2 := HashContent("same\n"), HashContent("same\n")
	if h1 == "" || h1 != h2 {
		t.Fatalf("hash not deterministic: %q vs %q", h1, h2)
	}
	if HashContent("same\n") == HashContent("same \n") {
		t.Fatal("distinct bytes share a hash")
	}
	if len(HashContent("x\n")) != 64 {
		t.Fatalf("hash is not hex sha256: %q", HashContent("x\n"))
	}
}

// TestTodo_HUB_004_Mutation is the MUTATION test for HUB-004. Each case kills
// one seeded semantic mutant of the version path:
//   - skip-normalization: CRLF input must hash exactly like LF input;
//   - skip-verification: a caller-supplied forged hash must be refused;
//   - constant-hash: any single-byte change must change the digest;
//   - truncation: Normalized must carry the full source, never a prefix.
func TestTodo_HUB_004_Mutation(t *testing.T) {
	if HashContent(NormalizeMarkdown("head\r\nbody\r\n")) != HashContent("head\nbody\n") {
		t.Fatal("skip-normalization mutant survived")
	}
	v := Version{Markdown: "head\nbody\n", Hash: strings.Repeat("0", 64)}
	if err := checkVersionHash(v); !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("skip-verification mutant survived: %v", err)
	}
	base := HashContent("head\nbody\n")
	for _, mutant := range []string{"head\nbody!\n", "head\nBody\n", "head\nbody\n\n"} {
		if HashContent(mutant) == base {
			t.Fatalf("constant-hash mutant survived for %q", mutant)
		}
	}
	long := strings.Repeat("line\n", 500) + "tail\n"
	if NormalizeMarkdown(long) != long {
		t.Fatal("truncation mutant survived")
	}
}
