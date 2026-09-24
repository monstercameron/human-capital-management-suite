package checkpoint_test

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/checkpoint"
)

func TestTodo_DATA_013(t *testing.T) {
	streams := []checkpoint.StreamHead{
		{StreamKey: "worker:1", Sequence: 3, ChainHash: strings.Repeat("a", 64), ChainAlgorithm: "sha256"},
		{StreamKey: "worker:2", Sequence: 7, ChainHash: strings.Repeat("b", 64), ChainAlgorithm: "sha256"},
	}
	root, err := checkpoint.ComputeRootDigest(streams)
	if err != nil || len(root) != 64 {
		t.Fatalf("root digest = %q, %v; want SHA-256 digest", root, err)
	}
}

func TestTodo_DATA_013_Golden(t *testing.T) {
	streams := []checkpoint.StreamHead{{StreamKey: "worker:1", Sequence: 1, ChainHash: strings.Repeat("a", 64), ChainAlgorithm: "sha256"}}
	first, err := checkpoint.ComputeRootDigest(streams)
	if err != nil {
		t.Fatal(err)
	}
	second, err := checkpoint.ComputeRootDigest(append([]checkpoint.StreamHead(nil), streams...))
	if err != nil || first != second {
		t.Fatalf("same stream set digested as %q and %q (%v)", first, second, err)
	}
}

func TestTodo_DATA_013_Security(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	status := checkpoint.KeyStatus{KeyID: "k1", NotBefore: from, RevokedAt: from.Add(time.Hour)}
	if !status.UsableAt(from.Add(time.Minute)) {
		t.Fatal("key should be usable before its revocation instant")
	}
	if status.UsableAt(status.RevokedAt) {
		t.Fatal("revoked key remained usable at the revocation instant")
	}
}

func TestTodo_DATA_013_Recovery(t *testing.T) {
	streams := []checkpoint.StreamHead{{StreamKey: "worker:1", Sequence: 1, ChainHash: strings.Repeat("a", 64), ChainAlgorithm: "sha256"}}
	left, err := checkpoint.ComputeRootDigest(streams)
	if err != nil {
		t.Fatal(err)
	}
	streams[0].Sequence++
	right, err := checkpoint.ComputeRootDigest(streams)
	if err != nil {
		t.Fatal(err)
	}
	if left == right {
		t.Fatal("corrected stream head retained the stale root digest")
	}
}
