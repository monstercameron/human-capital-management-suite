package agentjourney

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestTodo_AGENT_047_Conformance pins the journey transcript: every leg's
// pinned digest and decision in order. A leg that silently changes its
// evidence, skips a gate, or reorders the journey fails here.
func TestTodo_AGENT_047_Conformance(t *testing.T) {
	world := NewWorld(t)
	world.DriveFull()

	raw, err := os.ReadFile(filepath.Join("testdata", "agentjourney_golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	got := world.Transcript()
	if len(got) != len(want) {
		t.Fatalf("transcript lines=%d, want %d:\n%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("transcript line %d=%q, want %q", i, got[i], want[i])
		}
	}
}
