package workorder

import (
	"errors"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestDefinitionCompilesToGovernedWorkOrderPath(t *testing.T) {
	definition := Definition()
	plan, err := Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if plan.WorkflowID != WorkflowID || plan.Version != Version || plan.Phase != workflow.PhaseP1B {
		t.Fatalf("compiled identity = %s/%d %s", plan.WorkflowID, plan.Version, plan.Phase)
	}
	if plan.StartNodeID != NodeSubmitRequest || plan.TerminalProfile != workflow.TerminalProfileExecute {
		t.Fatalf("compiled start/profile = %s/%s", plan.StartNodeID, plan.TerminalProfile)
	}
	compiledNodes := make(map[string]bool, len(plan.Nodes))
	for _, node := range plan.Nodes {
		compiledNodes[node.ID] = true
	}
	for _, node := range definition.Nodes {
		if !compiledNodes[node.ID] {
			t.Errorf("definition node %q was not compiled", node.ID)
		}
	}
	if got := plan.NodesWithRole(workflow.RoleAuthoritativeCore); !reflect.DeepEqual(got, []string{NodeSubmitRequest}) {
		t.Fatalf("authoritative core = %v, want [%s]", got, NodeSubmitRequest)
	}
	if got := plan.NodesWithRole(workflow.RoleDownstreamEffect); !reflect.DeepEqual(got, []string{NodeClose, NodeRelease}) {
		t.Fatalf("downstream effects = %v, want close/release", got)
	}
	for _, approval := range []struct{ node, requirement string }{
		{NodeApproveScope, ApprovalScope}, {NodeApproveBudget, ApprovalBudget}, {NodeAccept, ApprovalAccept},
	} {
		node, ok := plan.Node(approval.node)
		if !ok || !reflect.DeepEqual(node.Governance.ApprovalRequirements, []string{approval.requirement}) {
			t.Errorf("approval node %s governance = %+v", approval.node, node.Governance)
		}
	}
	if plan.Digest() == "" {
		t.Fatal("compiled plan has no digest")
	}
}

func TestTemplatePublishesPinnedAndReconfigurableVariants(t *testing.T) {
	defaultDefinition := Definition()
	defaultPlan, err := Compile(defaultDefinition)
	if err != nil {
		t.Fatalf("compile default: %v", err)
	}
	custom := DefaultTemplate()
	custom.Version = 2
	custom.ScopeApproverExpression = "RegionalProjectApproverFor(work_order)"
	custom.BudgetApproverExpression = "FinanceDirectorFor(work_order)"
	customDefinition := DefinitionFor(custom)
	customPlan, err := Compile(customDefinition)
	if err != nil {
		t.Fatalf("compile custom: %v", err)
	}
	if customPlan.Version != 2 || customPlan.Digest() == defaultPlan.Digest() {
		t.Fatalf("custom identity/digest = %d/%s, want version 2 with new digest", customPlan.Version, customPlan.Digest())
	}
	if _, ok := customPlan.Node(NodeInspect); !ok {
		t.Fatal("mandatory inspection node is missing from the compiled plan")
	}
	approval, ok := customPlan.Node(NodeApproveScope)
	if !ok || approval.Governance.Purpose != purpose {
		t.Fatalf("custom scope approval is absent or ungoverned: %+v", approval)
	}
	firstDigest := defaultPlan.Digest()
	defaultDefinition.Nodes[0].Metadata = map[string]string{"mutated": "after publication"}
	if defaultPlan.Digest() != firstDigest {
		t.Fatalf("published plan digest changed after authoring definition mutation: %s -> %s", firstDigest, defaultPlan.Digest())
	}
	second, err := Compile(Definition())
	if err != nil || second.Digest() != firstDigest {
		t.Fatalf("same published definition digest = %s, err %v; want %s", second.Digest(), err, firstDigest)
	}
	defaultPlan.Nodes[0].EffectClass = ""
	if err := defaultPlan.Verify(); err == nil {
		t.Fatal("compiled plan accepted a mutation to its published content")
	}
}

func TestCompilePinnedBindsTenantScopeAndPublishedTemplateIdentity(t *testing.T) {
	template := DefaultTemplate()
	template.TenantScope = "tenant-ironridge"
	template.OrganizationScope = "tenant-ironridge/projects/riverside"
	pin := TemplatePin{TemplateID: "STAIR_REPAIR", Version: "1.2.0", Digest: "sha256:published-template"}
	definition, err := DefinitionForPin(template, pin)
	if err != nil {
		t.Fatalf("DefinitionForPin: %v", err)
	}
	if definition.TenantScope != template.TenantScope || definition.OrganizationScope != template.OrganizationScope {
		t.Fatalf("compiled scopes = %s/%s", definition.TenantScope, definition.OrganizationScope)
	}
	if !reflect.DeepEqual(definition.MatchPredicate, map[string]string{
		"work_order.template_id":      pin.TemplateID,
		"work_order.template_version": pin.Version,
		"work_order.template_digest":  pin.Digest,
	}) {
		t.Fatalf("template selector binding = %v", definition.MatchPredicate)
	}
	plan, err := CompilePinned(template, pin)
	if err != nil || plan.Digest() == "" {
		t.Fatalf("CompilePinned = %v, %v", plan, err)
	}
	if _, err := DefinitionForPin(template, TemplatePin{TemplateID: pin.TemplateID}); !errors.Is(err, ErrInvalidTemplatePin) {
		t.Fatalf("incomplete template pin error = %v", err)
	}
}

func TestDefinitionCarriesTypedCapabilitiesAndCorrelatedExecutionSignal(t *testing.T) {
	plan, err := Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, id := range []string{NodeSubmitRequest, NodeRelease, NodeClose} {
		node, ok := plan.Node(id)
		if !ok || node.Capability == nil {
			t.Fatalf("capability node %s missing", id)
		}
		if node.Capability.ID == "" || node.Capability.Version != 1 || node.Capability.Digest == "" || node.Capability.IdempotencyKeyMapping != "work_order_id" {
			t.Errorf("capability binding on %s is incomplete: %+v", id, node.Capability)
		}
	}
	signal, ok := plan.Node(NodeAwaitExecution)
	if !ok || signal.Signal == nil || signal.Signal.CorrelationKeyExpression != "work_order_id" || len(signal.Signal.AcceptedSources) != 1 {
		t.Fatalf("execution signal binding = %+v", signal.Signal)
	}
	if signal.Signal.EventType != "hcmnext.events.work_order.execution_completed" {
		t.Fatalf("execution signal event type = %q", signal.Signal.EventType)
	}
}

func TestDefinitionRoutesEveryBusinessFailureToExplicitDisposition(t *testing.T) {
	plan, err := Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := []workflow.Edge{
		{From: NodeApproveScope, To: NodeEndRejected, RouteKey: string(workflow.OutcomeRejected)},
		{From: NodeApproveBudget, To: NodeEndRejected, RouteKey: string(workflow.OutcomeRejected)},
		{From: NodeRelease, To: NodeEndFailed, RouteKey: string(workflow.OutcomeRejected)},
		{From: NodeAwaitExecution, To: NodeEndFailed, RouteKey: "TIMED_OUT"},
		{From: NodeInspect, To: NodeEndFailed, RouteKey: string(workflow.OutcomeRejected)},
		{From: NodeAccept, To: NodeEndRejected, RouteKey: string(workflow.OutcomeRejected)},
		{From: NodeClose, To: NodeEndFailed, RouteKey: string(workflow.OutcomeRejected)},
	}
	edges := make(map[workflow.Edge]bool, len(plan.Edges))
	for _, edge := range plan.Edges {
		edges[edge] = true
	}
	for _, edge := range want {
		if !edges[edge] {
			t.Errorf("compiled graph lacks disposition edge %+v", edge)
		}
	}
	if err := plan.Verify(); err != nil {
		t.Fatalf("compiled plan does not verify: %v", err)
	}
}

func TestCompileRejectsUnboundCapabilityAndMissingSignalTimeoutRoute(t *testing.T) {
	badCapability := Definition()
	for index := range badCapability.Nodes {
		if badCapability.Nodes[index].ID == NodeRelease {
			badCapability.Nodes[index].Capability.ID = "hcmnext.field.work_order.unregistered"
		}
	}
	if _, err := Compile(badCapability); err == nil {
		t.Fatal("Compile accepted a capability absent from the published capability table")
	}

	badRoutes := Definition()
	filtered := badRoutes.Edges[:0]
	for _, edge := range badRoutes.Edges {
		if edge.From == NodeAwaitExecution && edge.RouteKey == "TIMED_OUT" {
			continue
		}
		filtered = append(filtered, edge)
	}
	badRoutes.Edges = filtered
	if _, err := Compile(badRoutes); err == nil {
		t.Fatal("Compile accepted a signal without an explicit timeout path")
	}
}
