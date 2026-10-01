package application

import (
	"testing"
	"time"
)

func TestTodo_ASSURANCE_001_Served(t *testing.T) {
	composed, _, _ := composeStub(t, stubServeConfig())

	register := composed.AssuranceRegister()
	if register == nil {
		t.Fatal("serve composition returned no assurance register")
	}
	component, ok := composed.Graph().Component(ComponentAssuranceRegister)
	if !ok || component.Kind != KindGovernance {
		t.Fatalf("assurance graph component = %+v, present=%t", component, ok)
	}

	decision := register.Gate(time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC))
	if decision.Allowed || len(decision.Reasons) != 1 || decision.Reasons[0] != "no assurance evidence" {
		t.Fatalf("empty served assurance register = %+v, want fail-closed no-evidence decision", decision)
	}
}
