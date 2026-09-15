package promotionexec

import (
	"context"
	"slices"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

func TestGovernedReadDefinitionsRegisterAsReadOnlyGraphCapabilities(t *testing.T) {
	defs := GovernedReadDefinitions()
	registry := capability.NewRegistry()
	ids := make([]string, 0, len(defs))
	for _, def := range defs {
		if def.EffectClass != capability.EffectReadOnly || def.RiskClass != "HIGH" {
			t.Fatalf("%s effect %s risk %q, want READ_ONLY/HIGH", def.ID, def.EffectClass, def.RiskClass)
		}
		if err := registry.Register(def, func(context.Context, any) (any, error) { return nil, nil }); err != nil {
			t.Fatalf("register %s: %v", def.ID, err)
		}
		ids = append(ids, def.ID)
	}
	want := []string{CapabilityRevalidate, CapabilityObservePayroll, CapabilityObserveAccess, CapabilityObserveReconciliation}
	if !slices.Equal(ids, want) {
		t.Fatalf("definitions = %v, want %v", ids, want)
	}
	for _, id := range want {
		if !slices.Contains(CapabilityIDs(), id) {
			t.Fatalf("%s is not a capability the graph invokes", id)
		}
	}
	for _, id := range []string{CapabilitySnapshotWorker, CapabilitySimulateCompensation, CapabilityEvaluateBand, CapabilityExecutePromotion} {
		if !slices.Contains(CapabilityIDs(), id) {
			t.Fatalf("%s is not a capability the graph invokes", id)
		}
	}
}
