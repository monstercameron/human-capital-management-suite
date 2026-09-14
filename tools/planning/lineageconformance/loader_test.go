package lineageconformance

import (
	"path/filepath"
	"testing"
)

func TestLoadInputReadsLiveRegistries(t *testing.T) {
	in, err := LoadInput(filepath.Join("..", "..", ".."), "2026-09-13")
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Witnesses.Witnesses) == 0 || len(in.Witnesses.Witnesses) != len(in.Definitions) {
		t.Fatalf("witnesses %d for %d definitions", len(in.Witnesses.Witnesses), len(in.Definitions))
	}
	if len(in.Bindings) == 0 || len(in.Producers) == 0 || len(in.Todos) == 0 || !in.TestExists["TestTodo_DATA_015"] {
		t.Fatal("LoadInput did not load bindings, producers, todos and test names")
	}
	if _, err := LoadInput(filepath.Join("..", "..", ".."), "13/09/2026"); err == nil {
		t.Fatal("an invalid as-of date was accepted")
	}
}
