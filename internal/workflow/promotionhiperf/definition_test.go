package promotionhiperf

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// promotionExecutePlanDigest pins the execute plan this variant extends
// (internal/workflow/promotionexec/definition_test.go), currently the v3 / 1.2.0
// plan. Keep this aligned with that package's authoritative current golden.
const promotionExecutePlanDigest = "5714988b93c43721bdf2bb0f8693df9870b715017472290b6ff139a168227c95"

// promotionHighPerformerPlanDigest pins the variant plan. It is filled when
// the variant graph lands (GREEN) and never moves without a version bump.
const promotionHighPerformerPlanDigest = "6b431aaed4f9826175bd7a50ad4a8f5b077df7f293bc8d2fa938decced45828d"

// mappingTargets returns the target paths of node's input mappings.
func mappingTargets(node workflow.Node) map[string]workflow.Source {
	out := make(map[string]workflow.Source, len(node.InputMappings))
	for _, m := range node.InputMappings {
		out[m.Target] = m.Source
	}
	return out
}

// mappingFrom reports whether any compiled mapping of the node resolves to
// an output of the named source node.
func mappingFrom(mappings []workflow.CompiledMapping, nodeID string) bool {
	for _, m := range mappings {
		if m.SourceNode == nodeID {
			return true
		}
	}
	return false
}

func variantNode(t *testing.T, def workflow.Definition, id string) workflow.Node {
	t.Helper()
	for _, node := range def.Nodes {
		if node.ID == id {
			return node
		}
	}
	t.Fatalf("variant has no node %q", id)
	return workflow.Node{}
}

// TestTodo_HIPERF_002 is the PRIMARY test for the high-performer variant
// plan: the variant compiles under its own identity with the execute node
// set plus a fetch_market_rate node feeding simulate_compensation and
// raise_threshold, its digest differs from execute's, and the execute
// definition, digest and node set are unchanged.
func TestTodo_HIPERF_002(t *testing.T) {
	def := Definition()
	if def.WorkflowID != WorkflowID || def.Version != Version {
		t.Fatalf("identity = %s/%d, want %s/%d", def.WorkflowID, def.Version, WorkflowID, Version)
	}

	fetch := variantNode(t, def, NodeFetchMarketRate)
	if fetch.Type != workflow.StepCapability || fetch.Capability == nil ||
		fetch.Capability.ID != CapabilityMarketRate || fetch.Capability.Version != 1 {
		t.Fatalf("fetch node = %+v, want a StepCapability invoking %s/v1", fetch, CapabilityMarketRate)
	}

	simulate := variantNode(t, def, promotionexec.NodeSimulateCompensation)
	simulateTargets := mappingTargets(simulate)
	marketSource, ok := simulateTargets["market_anchor"]
	if !ok {
		t.Fatal("simulate_compensation has no market_anchor input: fetch_market_rate does not feed it")
	}
	if marketSource.Kind != workflow.SourceNodeOutput || marketSource.NodeID != NodeFetchMarketRate {
		t.Fatalf("market_anchor source = %+v, want the fetch_market_rate node output", marketSource)
	}

	threshold := variantNode(t, def, promotionexec.NodeRaiseThreshold)
	thresholdTargets := mappingTargets(threshold)
	floorSource, ok := thresholdTargets["market_floor"]
	if !ok {
		t.Fatal("raise_threshold has no market_floor input: the market anchor does not feed it")
	}
	if floorSource.Kind != workflow.SourceNodeOutput || floorSource.NodeID != promotionexec.NodeSimulateCompensation {
		t.Fatalf("market_floor source = %+v, want the simulate_compensation node output", floorSource)
	}
	anchorSource, ok := thresholdTargets["market_anchor"]
	if !ok {
		t.Fatal("raise_threshold has no market_anchor input: fetch_market_rate does not feed it directly")
	}
	if anchorSource.Kind != workflow.SourceNodeOutput || anchorSource.NodeID != NodeFetchMarketRate {
		t.Fatalf("threshold market_anchor source = %+v, want the fetch_market_rate node output", anchorSource)
	}

	plan, err := Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if err := plan.Verify(); err != nil {
		t.Fatalf("variant plan does not verify against its own digest: %v", err)
	}
	if !HasMarketRate(plan) {
		t.Fatal("compiled variant reports no market-rate node")
	}
	if HasMarketRate(nil) {
		t.Fatal("a nil plan reports a market-rate node")
	}
	execute, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("promotionexec.Compile: %v", err)
	}
	if got := execute.Digest(); got != promotionExecutePlanDigest {
		t.Fatalf("execute digest = %q, want the pinned %q: the variant moved the execute graph", got, promotionExecutePlanDigest)
	}
	if plan.Digest() == execute.Digest() {
		t.Fatal("variant digest equals the execute digest: the variant is not a distinct plan")
	}
	if plan.WorkflowID != WorkflowID || plan.Version != Version {
		t.Fatalf("compiled identity = %s/%d, want %s/%d", plan.WorkflowID, plan.Version, WorkflowID, Version)
	}
	compiledSimulate, ok := plan.Node(promotionexec.NodeSimulateCompensation)
	if !ok || !mappingFrom(compiledSimulate.Mappings, NodeFetchMarketRate) {
		t.Fatal("compiled simulate_compensation takes no input from fetch_market_rate")
	}
	compiledThreshold, ok := plan.Node(promotionexec.NodeRaiseThreshold)
	if !ok || !mappingFrom(compiledThreshold.Mappings, NodeFetchMarketRate) {
		t.Fatal("compiled raise_threshold takes no input from fetch_market_rate")
	}
	if _, ok := execute.Node(NodeFetchMarketRate); ok {
		t.Fatal("the execute plan carries the variant fetch node")
	}
	sim, err := CompileSimulation()
	if err != nil {
		t.Fatalf("CompileSimulation: %v", err)
	}
	if !sim.Effects.ZeroEffect {
		t.Fatalf("variant simulation effects = %+v, want zero effects", sim.Effects)
	}
	for _, node := range sim.Nodes {
		if node.EffectClass.IsWrite() {
			t.Fatalf("variant simulation node %q carries write effect %s", node.ID, node.EffectClass)
		}
	}
}

// TestTodo_HIPERF_002_Golden pins the variant plan digest and reachability
// order, so a future graph change moves this golden loudly instead of
// silently re-resolving rated workers onto a different plan.
func TestTodo_HIPERF_002_Golden(t *testing.T) {
	plan, err := Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got := plan.Digest(); got != promotionHighPerformerPlanDigest {
		t.Fatalf("variant plan digest = %q, want the pinned %q", got, promotionHighPerformerPlanDigest)
	}
	wantOrder := NodeOrder()
	if len(plan.Reachability.Order) != len(wantOrder) {
		t.Fatalf("reachability order has %d nodes, want %d: %v", len(plan.Reachability.Order), len(wantOrder), plan.Reachability.Order)
	}
	for i, id := range wantOrder {
		if plan.Reachability.Order[i] != id {
			t.Fatalf("reachability order = %v, want %v", plan.Reachability.Order, wantOrder)
		}
	}
}

// TestVariantCapabilityBindings proves the variant resolves every capability
// its graph invokes, including the market-rate read the execute plan does
// not carry, and that CapabilityIDs names exactly that set.
func TestVariantCapabilityBindings(t *testing.T) {
	ids := CapabilityIDs()
	found := false
	for _, id := range ids {
		if id == CapabilityMarketRate {
			found = true
		}
	}
	if !found {
		t.Fatalf("CapabilityIDs = %v, want the market-rate capability", ids)
	}
	for _, id := range promotionexec.CapabilityIDs() {
		hit := false
		for _, got := range ids {
			if got == id {
				hit = true
			}
		}
		if !hit {
			t.Fatalf("CapabilityIDs = %v, missing execute capability %s", ids, id)
		}
	}
	resolver, err := capabilities()
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	for _, id := range ids {
		version := uint32(1)
		if id == promotionexec.CapabilityExecutePromotion {
			version = 2
		}
		record, ok := resolver.Lookup(capability.Key{ID: id, Version: version})
		if !ok {
			t.Fatalf("variant resolver has no record for %s", id)
		}
		if id == CapabilityMarketRate && (record.Definition.EffectClass.IsWrite() || record.Definition.AuthZScopeRef != "scope:rewards.read") {
			t.Fatalf("market-rate record = %+v, want a read-only scope:rewards.read binding", record.Definition)
		}
	}
	if _, ok := resolver.Lookup(capability.Key{ID: "hcmnext.rewards.no_such_capability", Version: 1}); ok {
		t.Fatal("variant resolver answers an unknown capability")
	}
	// One table serves both modes: the compiler derives the SIMULATE
	// projection from each write node's declared mode overlay (WF-EXT-003),
	// so the promote and release records stay writes here and compile to
	// reads only under the projection.
	for _, key := range []capability.Key{{ID: "hcmnext.people.promote_worker", Version: 2}, {ID: "hcmnext.rewards.release_compensation_budget", Version: 1}} {
		record, ok := resolver.Lookup(key)
		if !ok || !record.Definition.EffectClass.IsWrite() {
			t.Fatalf("execute record %s = %+v, want the executable write", key, record.Definition)
		}
	}
	plan, err := Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, nodeID := range []string{promotionexec.NodeExecutePromotion, promotionexec.NodeCompensateHold} {
		node, ok := plan.Node(nodeID)
		if !ok || node.Capability == nil {
			t.Fatalf("compiled node %s has no capability", nodeID)
		}
		record, ok := resolver.Lookup(capability.Key{ID: node.Capability.ID, Version: node.Capability.Version})
		if !ok || record.Digest != node.Capability.Digest {
			t.Fatalf("node %s manifest %+v does not match variant registry record %+v", nodeID, node.Capability, record)
		}
	}
	sim, err := CompileSimulation()
	if err != nil {
		t.Fatalf("CompileSimulation: %v", err)
	}
	for _, id := range []string{promotionexec.NodeExecutePromotion, promotionexec.NodeCompensateHold} {
		node, ok := sim.Node(id)
		if !ok || node.EffectClass.IsWrite() || node.EffectRole != "" {
			t.Fatalf("simulate node %s = %+v, want a role-free read", id, node)
		}
		if node.Capability == nil || node.Capability.OperationMode != workflow.ModeSimulate {
			t.Fatalf("simulate node %s capability = %+v, want SIMULATE mode", id, node.Capability)
		}
	}
}
