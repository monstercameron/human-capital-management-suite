package execute

import "testing"

func TestTodo_WF_EXT_007_TerminalBindings(t *testing.T) {
	writerA := &fakeTerminalWriter{}
	writerB := &fakeTerminalWriter{}
	bindings := TerminalBindings{
		"workflow:a": {"APPROVED": writerA, "REJECTED": writerB},
		"workflow:b": {"APPROVED": writerB},
	}
	got, ok := bindings.ResolveTerminal("workflow:a", "APPROVED")
	if !ok || got != writerA {
		t.Fatalf("workflow a approval writer = %v, %v", got, ok)
	}
	got, ok = bindings.ResolveTerminal("workflow:a", "REJECTED")
	if !ok || got != writerB {
		t.Fatalf("workflow a rejection writer = %v, %v", got, ok)
	}
	if _, ok := bindings.ResolveTerminal("workflow:a", "UNKNOWN"); ok {
		t.Fatal("undeclared terminal code resolved")
	}
	if _, ok := bindings.ResolveTerminal("workflow:other", "APPROVED"); ok {
		t.Fatal("writer crossed workflow registration")
	}
}
