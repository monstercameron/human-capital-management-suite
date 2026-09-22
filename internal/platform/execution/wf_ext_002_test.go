package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionsteps"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionhiperf"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestTodo_WF_EXT_002 proves the WF-EXT-002 GREEN: execution composes a list
// of WorkflowRegistration values (definition, match, capability-keyed step
// handlers, approval compilers, admitted intent types) instead of the
// two-plan Promotion switch; the policy resolver carries one entry per
// registration; RunsInTransaction derives from the compiled EffectRole; and
// the high-performer variant lands as a third registration sharing the
// execute node set rather than a copied package.
func TestTodo_WF_EXT_002(t *testing.T) {
	regs := ShippedRegistrations()
	if len(regs) != 4 {
		t.Fatalf("ShippedRegistrations = %d entries, want 4 (prototype, execute v1.0, execute current, high-performer variant)", len(regs))
	}
	byName := map[string]WorkflowRegistration{}
	for _, reg := range regs {
		if reg.Name == "" || reg.WorkflowID == "" || reg.Definition == nil || reg.Compile == nil {
			t.Fatalf("registration %+v names no identity, definition or compile", reg.Name)
		}
		if len(reg.AdmittedIntentTypes) == 0 {
			t.Fatalf("registration %q admits no intent types", reg.Name)
		}
		byName[reg.Name] = reg
	}
	for _, name := range []string{"promotion-approval", "promotion-execute-v1.0", "promotion-execute", "promotion-high-performer"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("ShippedRegistrations carries no %q registration", name)
		}
	}

	variant := byName["promotion-high-performer"]
	if variant.WorkflowID != promotionhiperf.WorkflowID {
		t.Fatalf("variant WorkflowID = %q, want %q: the variant must land as a registration, not a copied package", variant.WorkflowID, promotionhiperf.WorkflowID)
	}
	variantDef := variant.Definition()
	executeDef := promotionexec.Definition()
	executeNodes := map[string]bool{}
	for _, node := range executeDef.Nodes {
		executeNodes[node.ID] = true
	}
	for id := range executeNodes {
		found := false
		for _, node := range variantDef.Nodes {
			if node.ID == id {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("variant definition carries no node %q: it must share the execute node set", id)
		}
	}
	fetchFound := false
	for _, node := range variantDef.Nodes {
		if node.ID == promotionhiperf.NodeFetchMarketRate {
			fetchFound = true
		}
	}
	if !fetchFound {
		t.Fatalf("variant definition carries no %q node", promotionhiperf.NodeFetchMarketRate)
	}
	if _, ok := variant.StepHandlers[promotionhiperf.CapabilityMarketRate]; !ok {
		t.Fatalf("variant registration has no step handler for capability %q", promotionhiperf.CapabilityMarketRate)
	}

	execute := byName["promotion-execute"]
	plan, err := execute.Compile()
	if err != nil {
		t.Fatalf("execute.Compile: %v", err)
	}
	for _, node := range plan.Nodes {
		if node.Capability == nil {
			continue
		}
		if !execute.HandlesCapability(node.Capability.ID) {
			t.Fatalf("execute registration handles no capability %q carried by node %q", node.Capability.ID, node.ID)
		}
		handler, ok := execute.HandlerFor(node)
		if !ok {
			t.Fatalf("HandlerFor(%q) = false, want the capability-keyed handler for %q", node.ID, node.Capability.ID)
		}
		if handler.Run == nil {
			t.Fatalf("HandlerFor(%q) has no Run func", node.ID)
		}
	}
	if len(byName["promotion-approval"].StepHandlers) != 0 {
		t.Fatalf("prototype registration carries %d step handlers, want none: it has no capability nodes", len(byName["promotion-approval"].StepHandlers))
	}

	// Approval compilers reproduce the direct requirement compilations.
	deadline := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	financeCompiler, ok := execute.ApprovalCompilers[promotionexec.NodeApproveFinance]
	if !ok {
		t.Fatalf("execute registration compiles no requirement for %q", promotionexec.NodeApproveFinance)
	}
	managerCompiler, ok := execute.ApprovalCompilers[promotionexec.NodeApproveManager]
	if !ok {
		t.Fatalf("execute registration compiles no requirement for %q", promotionexec.NodeApproveManager)
	}
	financeReq, err := financeCompiler("principal:finance", deadline)
	if err != nil {
		t.Fatalf("finance compiler: %v", err)
	}
	directFinance, err := promotionexec.CompileFinanceApprovalRequirement("principal:finance", deadline)
	if err != nil {
		t.Fatalf("direct finance compile: %v", err)
	}
	if financeReq.Digest() != directFinance.Digest() {
		t.Fatalf("finance requirement digest = %q, want direct %q", financeReq.Digest(), directFinance.Digest())
	}
	managerReq, err := managerCompiler("principal:manager", deadline)
	if err != nil {
		t.Fatalf("manager compiler: %v", err)
	}
	directManager, err := promotionexec.CompileManagerApprovalRequirement("principal:manager", deadline)
	if err != nil {
		t.Fatalf("direct manager compile: %v", err)
	}
	if managerReq.Digest() != directManager.Digest() {
		t.Fatalf("manager requirement digest = %q, want direct %q", managerReq.Digest(), directManager.Digest())
	}
	prototypeCompiler, ok := byName["promotion-approval"].ApprovalCompilers[prototype.NodeApproval]
	if !ok {
		t.Fatalf("prototype registration compiles no requirement for %q", prototype.NodeApproval)
	}
	prototypeReq, err := prototypeCompiler("principal:approver", deadline)
	if err != nil {
		t.Fatalf("prototype compiler: %v", err)
	}
	directPrototype, err := prototype.CompileApprovalRequirement("principal:approver", deadline)
	if err != nil {
		t.Fatalf("direct prototype compile: %v", err)
	}
	if prototypeReq.Digest() != directPrototype.Digest() {
		t.Fatalf("prototype requirement digest = %q, want direct %q", prototypeReq.Digest(), directPrototype.Digest())
	}

	// RunsInTransaction derives from the compiled EffectRole, never a
	// node-id list: a foreign id with a write role is claimed, and a known
	// id stripped of its role is not.
	runner := promotionStepRunner{plan: PLAN_EXECUTE}
	foreignCore := workflow.CompiledNode{ID: "custom_core", EffectRole: workflow.RoleAuthoritativeCore}
	if !runner.RunsInTransaction(foreignCore) {
		t.Error("RunsInTransaction(custom_core/AUTHORITATIVE_CORE) = false, want true: the claim derives from EffectRole")
	}
	foreignDownstream := workflow.CompiledNode{ID: "custom_compensation", EffectRole: workflow.RoleDownstreamEffect}
	if !runner.RunsInTransaction(foreignDownstream) {
		t.Error("RunsInTransaction(custom_compensation/DOWNSTREAM_EFFECT) = false, want true: the claim derives from EffectRole")
	}
	knownIDNoRole := workflow.CompiledNode{ID: promotionexec.NodeExecutePromotion}
	if runner.RunsInTransaction(knownIDNoRole) {
		t.Error("RunsInTransaction(execute_promotion without role) = true, want false: a node-id list must not claim it")
	}
	derived := workflow.CompiledNode{ID: "custom_projection", EffectRole: workflow.RoleDerivedUpdate}
	if runner.RunsInTransaction(derived) {
		t.Error("RunsInTransaction(DERIVED_UPDATE) = true, want false: derived updates settle through the effect-role policy, not the advance transaction")
	}
	executeNode, ok := plan.Node(promotionexec.NodeExecutePromotion)
	if !ok {
		t.Fatalf("compiled plan carries no node %q", promotionexec.NodeExecutePromotion)
	}
	if !runner.RunsInTransaction(executeNode) {
		t.Error("RunsInTransaction(execute_promotion) = false, want true")
	}
	compensateNode, ok := plan.Node(promotionexec.NodeCompensateHold)
	if !ok {
		t.Fatalf("compiled plan carries no node %q", promotionexec.NodeCompensateHold)
	}
	if !runner.RunsInTransaction(compensateNode) {
		t.Error("RunsInTransaction(compensate_budget_hold) = false, want true")
	}
	snapshotNode, ok := plan.Node(promotionexec.NodeSnapshotWorker)
	if !ok {
		t.Fatalf("compiled plan carries no node %q", promotionexec.NodeSnapshotWorker)
	}
	if runner.RunsInTransaction(snapshotNode) {
		t.Error("RunsInTransaction(snapshot_worker) = true, want false: reads never enter the advance transaction")
	}

	pinned, err := ComposeWorkflowRegistrations(PLAN_EXECUTE)
	if err != nil {
		t.Fatalf("ComposeWorkflowRegistrations(execute): %v", err)
	}
	if len(pinned) != len(regs) {
		t.Fatalf("composed %d pinned registrations, want one per registration (%d)", len(pinned), len(regs))
	}
	resolver := ResolverForRegistrations(pinned)
	if len(resolver.Entries) != len(pinned) {
		t.Fatalf("resolver carries %d entries, want one per registration (%d)", len(resolver.Entries), len(pinned))
	}
	digests := map[string]string{}
	for _, p := range pinned {
		digests[p.Registration.Name] = p.Pin.CompiledPlanDigest
	}
	if digests["promotion-execute"] == digests["promotion-high-performer"] {
		t.Fatalf("variant digest equals the execute digest: the variant must differ by its market-rate node")
	}
	current, v1_0, variantPinned, prototypePinned := pinned[2], pinned[1], pinned[3], pinned[0]
	resolve := func(pin string) string {
		sel, err := resolver.ResolveWorkflow(context.Background(), runtime.StartRequest{PinnedCompiledPlanDigest: pin})
		if err != nil {
			return "error:" + err.Error()
		}
		return sel.Pin.CompiledPlanDigest
	}
	if got := resolve(""); got != current.Pin.CompiledPlanDigest {
		t.Fatalf("new start resolves %q, want current %q", got, current.Pin.CompiledPlanDigest)
	}
	if got := resolve(v1_0.Pin.CompiledPlanDigest); got != v1_0.Pin.CompiledPlanDigest {
		t.Fatalf("v1.0 continuation resolves %q, want %q", got, v1_0.Pin.CompiledPlanDigest)
	}
	if got := resolve(variantPinned.Pin.CompiledPlanDigest); got != variantPinned.Pin.CompiledPlanDigest {
		t.Fatalf("variant continuation resolves %q, want %q", got, variantPinned.Pin.CompiledPlanDigest)
	}
	if got := resolve(prototypePinned.Pin.CompiledPlanDigest); got != prototypePinned.Pin.CompiledPlanDigest {
		t.Fatalf("prototype continuation resolves %q, want %q", got, prototypePinned.Pin.CompiledPlanDigest)
	}
	if got := resolve("sha256:unknown"); got[:6] != "error:" {
		t.Fatalf("unknown pin resolves %q, want refusal", got)
	}

	union := UnionAdmittedIntentTypes(pinned)
	if !union[promotion.IntentType] {
		t.Fatalf("union admits %#v, want the promotion intent type", union)
	}
}

// TestWorkflowRegistrationCapabilityDispatch proves the execute runner
// dispatches on the compiled capability id: a shared capability node runs
// the shared runner (failing closed here on unbound services), the
// variant's market-rate fetch fails closed naming its capability, and a
// node without a capability takes the legacy node-id path.
func TestWorkflowRegistrationCapabilityDispatch(t *testing.T) {
	pinned, err := ComposeWorkflowRegistrations(PLAN_EXECUTE)
	if err != nil {
		t.Fatalf("ComposeWorkflowRegistrations(execute): %v", err)
	}
	table := capabilityDispatch(pinned)
	runner := promotionStepRunner{plan: PLAN_EXECUTE, ports: &promotionStepPorts{}, capabilities: table}

	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("promotionexec.Compile: %v", err)
	}
	snapshot, ok := plan.Node(promotionexec.NodeSnapshotWorker)
	if !ok {
		t.Fatalf("compiled plan carries no node %q", promotionexec.NodeSnapshotWorker)
	}
	outcome, _, err := runner.Run(context.Background(), execute.StepRequest{Node: snapshot})
	if err != nil {
		t.Fatalf("Run(snapshot_worker) = %v, want dispatch to the shared runner", err)
	}
	if !outcome.Failed || outcome.ErrorClass != promotionsteps.FailureNotWired {
		t.Fatalf("outcome = %+v, want failed PORT_NOT_CONFIGURED: the capability dispatch must reach the runner", outcome)
	}

	fetch := workflow.CompiledNode{
		ID: promotionhiperf.NodeFetchMarketRate, Type: workflow.StepCapability,
		Capability: &workflow.CompiledCapability{ID: promotionhiperf.CapabilityMarketRate, Version: 1},
	}
	if _, _, err := runner.Run(context.Background(), execute.StepRequest{Node: fetch}); err == nil ||
		!strings.Contains(err.Error(), promotionhiperf.CapabilityMarketRate) {
		t.Fatalf("Run(fetch_market_rate) = %v, want the failure to name the unbound capability", err)
	}

	legacy := promotionStepRunner{plan: PLAN_EXECUTE, ports: &promotionStepPorts{}}
	approval, ok := plan.Node(promotionexec.NodeApproveFinance)
	if !ok {
		t.Fatalf("compiled plan carries no node %q", promotionexec.NodeApproveFinance)
	}
	outcome, _, err = legacy.Run(context.Background(), execute.StepRequest{Node: approval})
	if err != nil {
		t.Fatalf("Run(approve_finance) without a dispatch table = %v, want the legacy path", err)
	}
	if outcome.NodeID != promotionexec.NodeApproveFinance {
		t.Fatalf("outcome NodeID = %q, want %q", outcome.NodeID, promotionexec.NodeApproveFinance)
	}

	if _, _, err := defaultStepRun(context.Background(), nil, execute.StepRequest{}); err == nil {
		t.Fatal("defaultStepRun(nil runner): expected an error")
	}
}

// wantPrototypeDigestWFEXT002 and wantExecuteDigestWFEXT002 pin the
// prototype and execute plan digests the registration path must serve
// byte-identical.
const (
	wantPrototypeDigestWFEXT002 = "6ad5a72214b7200f0957d40c983b3763de6201ba3a4ea209a49bb0f587a974c0"
	wantExecuteDigestWFEXT002   = "9c97ca67a56638f631bebd17c0bbdb62336ca7ae801d43571e9b8f908f46d2bf"
)

// TestTodo_WF_EXT_002_Golden proves the registration path changes no plan
// bytes: the prototype and execute digests served through registrations are
// byte-identical to the direct compilations, pinned here.
func TestTodo_WF_EXT_002_Golden(t *testing.T) {
	prototypePlan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatalf("prototype.CompileApproval: %v", err)
	}
	executePlan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("promotionexec.Compile: %v", err)
	}
	if prototypePlan.Digest() != wantPrototypeDigestWFEXT002 {
		t.Fatalf("prototype digest = %q, want golden %q", prototypePlan.Digest(), wantPrototypeDigestWFEXT002)
	}
	if executePlan.Digest() != wantExecuteDigestWFEXT002 {
		t.Fatalf("execute digest = %q, want golden %q", executePlan.Digest(), wantExecuteDigestWFEXT002)
	}
	pinned, err := ComposeWorkflowRegistrations(PLAN_EXECUTE)
	if err != nil {
		t.Fatalf("ComposeWorkflowRegistrations(execute): %v", err)
	}
	byName := map[string]string{}
	for _, p := range pinned {
		byName[p.Registration.Name] = p.Plan.Digest()
	}
	if byName["promotion-approval"] != wantPrototypeDigestWFEXT002 {
		t.Fatalf("registration prototype digest = %q, want golden %q", byName["promotion-approval"], wantPrototypeDigestWFEXT002)
	}
	if byName["promotion-execute"] != wantExecuteDigestWFEXT002 {
		t.Fatalf("registration execute digest = %q, want golden %q", byName["promotion-execute"], wantExecuteDigestWFEXT002)
	}
}

// TestTodo_WF_EXT_002_Integration serves the prototype, both execute
// versions and the high-performer variant from one composed cell against a
// real store: every pin resolves through the registration-backed resolver
// inside a transaction, and an unknown pin is refused.
func TestTodo_WF_EXT_002_Integration(t *testing.T) {
	ctx := context.Background()
	database := pgtest.New(t)
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	tenant := uuid.New()
	database.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,'wf-ext-002','cell-local','WF-EXT-002 tenant','ACTIVE',$2)`, tenant, at.Add(-time.Hour))
	composition, err := NewPromotionExecution(PromotionExecutionConfig{
		DB: database.Conn, Terminal: stubTerminal{}, Clock: func() time.Time { return at }, Plan: PLAN_EXECUTE,
	})
	if err != nil {
		t.Fatalf("NewPromotionExecution(execute): %v", err)
	}
	if len(composition.Registrations) != 4 {
		t.Fatalf("composed %d registrations, want 4", len(composition.Registrations))
	}
	pinned, err := ComposeWorkflowRegistrations(PLAN_EXECUTE)
	if err != nil {
		t.Fatalf("ComposeWorkflowRegistrations(execute): %v", err)
	}
	// The cell's own resolver serves every registration: new starts land
	// on the current execute plan and each pinned continuation resolves
	// its own plan.
	served := map[string]string{}
	for _, pin := range []string{"", pinned[0].Pin.CompiledPlanDigest, pinned[1].Pin.CompiledPlanDigest, pinned[2].Pin.CompiledPlanDigest, pinned[3].Pin.CompiledPlanDigest} {
		sel, err := composition.Resolver.ResolveWorkflow(ctx, runtime.StartRequest{PinnedCompiledPlanDigest: pin})
		if err != nil {
			t.Fatalf("cell resolver pin %q: %v", pin, err)
		}
		served[pin] = sel.Pin.CompiledPlanDigest
	}
	if served[""] != pinned[2].Pin.CompiledPlanDigest {
		t.Fatalf("cell serves new starts at %q, want current execute %q", served[""], pinned[2].Pin.CompiledPlanDigest)
	}
	policy, ok := composition.Resolver.(effects.PolicyResolver)
	if !ok {
		t.Fatalf("cell resolver = %T, want the registration-backed effects.PolicyResolver", composition.Resolver)
	}
	if len(policy.Entries) != len(pinned) {
		t.Fatalf("cell resolver carries %d entries, want one per registration (%d)", len(policy.Entries), len(pinned))
	}
	conn := database.NewConn(t)
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatalf("scope: %v", err)
	}
	for _, p := range pinned {
		sel, err := policy.ResolveWorkflowInTx(ctx, tx, runtime.StartRequest{PinnedCompiledPlanDigest: p.Pin.CompiledPlanDigest})
		if err != nil {
			t.Fatalf("ResolveWorkflowInTx(%s): %v", p.Registration.Name, err)
		}
		if sel.Pin.CompiledPlanDigest != p.Pin.CompiledPlanDigest {
			t.Fatalf("ResolveWorkflowInTx(%s) = %q, want %q", p.Registration.Name, sel.Pin.CompiledPlanDigest, p.Pin.CompiledPlanDigest)
		}
		if sel.Plan == nil || sel.Plan.Digest() != p.Pin.CompiledPlanDigest {
			t.Fatalf("ResolveWorkflowInTx(%s) serves a plan digesting to %v, want %q", p.Registration.Name, sel.Plan, p.Pin.CompiledPlanDigest)
		}
	}
	if _, err := policy.ResolveWorkflowInTx(ctx, tx, runtime.StartRequest{PinnedCompiledPlanDigest: "sha256:unknown"}); err == nil {
		t.Fatal("ResolveWorkflowInTx(unknown pin): expected refusal")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}
}
