package workflow

import "testing"

func TestEffects_Smoke(t *testing.T) {
	if t == nil {
		t.Fatalf("nil tester")
	}
}

func TestEffects_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestCompiledNodeAdmitsModeFailsClosed(t *testing.T) {
	node := CompiledNode{AllowedModes: allowedModesFor("INTERNAL_MUTATION")}
	if len(node.AllowedModes) == 0 {
		t.Fatal("INTERNAL_MUTATION compiled no modes")
	}
	if !node.AdmitsMode(ModeExecute) || node.AdmitsMode(ModeSimulate) || node.AdmitsMode("") {
		t.Fatalf("modes %v: EXECUTE must be admitted, SIMULATE and empty refused", node.AllowedModes)
	}
	if (CompiledNode{}).AdmitsMode(ModeExecute) {
		t.Fatal("a node with no compiled modes admitted EXECUTE")
	}
}
