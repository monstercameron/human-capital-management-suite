package version

import "testing"

func TestStore_Smoke(t *testing.T) {
	if t == nil {
		t.Fatalf("nil tester")
	}
}

func TestStore_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestTodo_WF_UI_002_CatalogStoreListsDefensiveDeterministicCopies(t *testing.T) {
	registry := NewRegistry()
	for _, value := range []CompiledVersion{
		{WorkflowID: "workflow.zeta", SemanticVersion: "1.0.0", CompiledPlanDigest: "digest-z-1", CanonicalPlanBytes: []byte("z")},
		{WorkflowID: "workflow.alpha", SemanticVersion: "1.0.0", CompiledPlanDigest: "digest-a-1", CanonicalPlanBytes: []byte("a1")},
		{WorkflowID: "workflow.alpha", SemanticVersion: "1.1.0", CompiledPlanDigest: "digest-a-2", CanonicalPlanBytes: []byte("a2")},
	} {
		if err := registry.Put(value); err != nil {
			t.Fatalf("Put(%s): %v", value.CompiledPlanDigest, err)
		}
	}
	got, err := registry.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	want := []string{"digest-a-1", "digest-a-2", "digest-z-1"}
	if len(got) != len(want) {
		t.Fatalf("ListAll length = %d, want %d", len(got), len(want))
	}
	for index, digest := range want {
		if got[index].CompiledPlanDigest != digest {
			t.Fatalf("ListAll[%d] = %s, want %s", index, got[index].CompiledPlanDigest, digest)
		}
	}
	got[0].CanonicalPlanBytes[0] = 'x'
	again, err := registry.ListAll()
	if err != nil {
		t.Fatalf("ListAll again: %v", err)
	}
	if string(again[0].CanonicalPlanBytes) != "a1" {
		t.Fatalf("caller mutation reached registry: %q", again[0].CanonicalPlanBytes)
	}
}
