package app

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

func TestTodo_WF_EXT_005_RegistryManifestsMatchCompiledPromotion(t *testing.T) {
	registry, err := newCapabilityRegistry(&domainHandlers{})
	if err != nil {
		t.Fatalf("newCapabilityRegistry: %v", err)
	}
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("promotionexec.Compile: %v", err)
	}
	for _, nodeID := range []string{
		promotionexec.NodeSnapshotWorker,
		promotionexec.NodeSimulateCompensation,
		promotionexec.NodeEvaluateBand,
		promotionexec.NodeRevalidate,
		promotionexec.NodeObservePayroll,
		promotionexec.NodeObserveAccess,
		promotionexec.NodeObserveReconciliation,
	} {
		node, ok := plan.Node(nodeID)
		if !ok || node.Capability == nil {
			t.Fatalf("compiled node %s has no capability", nodeID)
		}
		record, ok := registry.Lookup(capability.Key{ID: node.Capability.ID, Version: node.Capability.Version})
		if !ok {
			t.Fatalf("served capability registry has no %s/v%d", node.Capability.ID, node.Capability.Version)
		}
		if record.Digest != node.Capability.Digest {
			t.Fatalf("%s plan digest %s != served registry digest %s", nodeID, node.Capability.Digest, record.Digest)
		}
	}
	for _, key := range []capability.Key{
		{ID: promotionexec.CapabilitySnapshotWorker, Version: 1},
		{ID: promotionexec.CapabilitySimulateCompensation, Version: 1},
		{ID: promotionexec.CapabilityEvaluateBand, Version: 1},
		{ID: promotionexec.CapabilityRevalidate, Version: 1},
		{ID: promotionexec.CapabilityObservePayroll, Version: 1},
		{ID: promotionexec.CapabilityObserveAccess, Version: 1},
		{ID: promotionexec.CapabilityObserveReconciliation, Version: 1},
	} {
		if _, ok := registry.Lookup(key); !ok {
			t.Fatalf("served registry does not publish Promotion read capability %s", key)
		}
	}
}
