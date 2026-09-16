package execution

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestComposedEffectRolesIsDurable proves serve hands the driver a WF-RUN-037
// policy backed by the durable settlement store, one fresh policy per
// composition, that execute.New accepts.
func TestComposedEffectRolesIsDurable(t *testing.T) {
	policy := composedEffectRoles()
	if policy == nil {
		t.Fatal("serve composes no effect-role policy")
	}
	if _, ok := policy.Settlements.(runtime.EffectRoleSettlementStore); !ok {
		t.Fatalf("settlements = %T, want runtime.EffectRoleSettlementStore", policy.Settlements)
	}
	if composedEffectRoles() == policy {
		t.Fatal("composedEffectRoles shares one mutable policy across compositions")
	}
	if _, err := execute.New(execute.Options{DB: unusedBeginner{}, Steps: unusedSteps{}, EffectRoles: policy}); err != nil {
		t.Fatalf("execute.New refused the composed policy: %v", err)
	}
}
