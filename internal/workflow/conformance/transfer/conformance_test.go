package transfer

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// mustSetup wires env against the compiled reference workflow, failing the
// test with the full diagnostic set if the reference stops compiling.
func mustSetup(t *testing.T, env *Environment) *Setup {
	t.Helper()
	setup, err := NewSetup(env)
	if err != nil {
		t.Fatalf("NewSetup: %v", err)
	}
	return setup
}

// mustRun walks the plan and fails on any refusal.
func mustRun(t *testing.T, setup *Setup) simulate.Receipt {
	t.Helper()
	receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return receipt
}

// TestTodo_CONF_003 is the PRIMARY conformance claim: the compiled
// cross-company transfer reference workflow walks, through the real
// SIMULATE-mode interpreter, along the golden path CONF-003's GREEN clause
// names - source and destination employment/authority reads, jurisdiction
// resolution, a bound conflict footprint, an authority-and-conflict decision
// and a post-decision observation - and ends at a terminal that reports
// exact, separately-tracked lifecycle dimensions with the transfer's exit,
// start and access-recalculation obligations outstanding rather than
// collapsed into a false success.
func TestTodo_CONF_003(t *testing.T) {
	setup := mustSetup(t, GoldenEnvironment())
	receipt := mustRun(t, setup)

	if receipt.Mode != workflow.ModeSimulate {
		t.Errorf("mode = %s, want %s", receipt.Mode, workflow.ModeSimulate)
	}
	if receipt.WorkflowID != WorkflowID {
		t.Errorf("workflow id = %q, want %q", receipt.WorkflowID, WorkflowID)
	}
	if receipt.PlanDigest != setup.Plan.Digest() {
		t.Errorf("receipt pins plan %q, plan digest is %q", receipt.PlanDigest, setup.Plan.Digest())
	}

	wantPath := []string{
		NodeReadSourceAuthority,
		NodeReadDestinationAuthority,
		NodeResolveJurisdiction,
		NodeComputeConflict,
		NodeBuildProposal,
		NodeAuthorityDecision,
		NodeObserveConflict,
		NodeEndPendingApprovals,
	}
	got := receipt.NodeIDs()
	if len(got) != len(wantPath) {
		t.Fatalf("walked %d nodes %v, want %d %v", len(got), got, len(wantPath), wantPath)
	}
	for i := range wantPath {
		if got[i] != wantPath[i] {
			t.Fatalf("node %d = %q, want %q (whole path %v)", i, got[i], wantPath[i], got)
		}
	}

	if receipt.Terminal.TerminalCode != "TRANSFER_SIMULATION_PENDING_APPROVALS" {
		t.Errorf("terminal code = %q, want TRANSFER_SIMULATION_PENDING_APPROVALS", receipt.Terminal.TerminalCode)
	}
	wantObligations := []string{ObligationExitStartEffects, ObligationAccessRecalc, ObligationEvidence}
	if !equalSets(receipt.Terminal.OutstandingObligationRefs, wantObligations) {
		t.Errorf("outstanding obligations = %v, want %v", receipt.Terminal.OutstandingObligationRefs, wantObligations)
	}

	wantLifecycle := simulate.LifecycleState{
		RequestState:     "SIMULATED",
		ExecutionState:   "NOT_PLANNED",
		BusinessState:    "NOT_STARTED",
		ConsistencyState: "PENDING_OBSERVATION",
		ObligationState:  "PENDING",
	}
	if receipt.Lifecycle != wantLifecycle {
		t.Errorf("lifecycle = %+v, want %+v", receipt.Lifecycle, wantLifecycle)
	}

	counters := receipt.EffectCounters()
	if !counters.IsZero() {
		t.Fatalf("simulation counted effects: %v", counters.NonZero())
	}
	if err := receipt.ZeroEffect.Validate(); err != nil {
		t.Fatalf("zero-effect receipt: %v", err)
	}

	if len(receipt.WorkItems) != 5 {
		t.Fatalf("work items = %d, want 5 (one per declared approval requirement)", len(receipt.WorkItems))
	}
	wantApprovals := []string{ApprovalSourceManager, ApprovalDestinationManager, ApprovalHRBP, ApprovalCompensation, ApprovalFinance}
	var gotApprovals []string
	for _, item := range receipt.WorkItems {
		if item.State != simulate.WouldAwait {
			t.Errorf("work item %s state = %q, want %q", item.RequirementID, item.State, simulate.WouldAwait)
		}
		if len(item.Candidates) == 0 {
			t.Errorf("work item %s resolved no candidate approver", item.RequirementID)
		}
		gotApprovals = append(gotApprovals, item.RequirementID)
	}
	if !equalSets(gotApprovals, wantApprovals) {
		t.Errorf("approval requirement refs = %v, want %v", gotApprovals, wantApprovals)
	}

	// The receipt digest is a content identity, not a timestamp: a second
	// independent run of the same environment and inputs reproduces it
	// exactly.
	second := mustRun(t, mustSetup(t, GoldenEnvironment()))
	if receipt.Digest() != second.Digest() {
		t.Fatalf("receipt digest drifted between runs:\n first  %s\n second %s", receipt.Digest(), second.Digest())
	}
	if err := receipt.Verify(); err != nil {
		t.Errorf("receipt does not verify: %v", err)
	}
}

// TestTodo_CONF_003_Fault exercises the RED cases CONF-003 names: an omitted
// section (jurisdiction context) and a partial external effect (a degraded
// post-decision observation) must never be quietly reported as success.
func TestTodo_CONF_003_Fault(t *testing.T) {
	t.Run("omitted_legal_context_is_unknown_not_success", func(t *testing.T) {
		setup, err := NewSetupMissingLegalContext(GoldenEnvironment())
		if err != nil {
			t.Fatalf("NewSetupMissingLegalContext: %v", err)
		}
		receipt := mustRun(t, setup)
		if receipt.Terminal.NodeID != NodeEndUnknown {
			t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, NodeEndUnknown, receipt.NodeIDs())
		}
		if receipt.Lifecycle.BusinessState == "COMPLETED" {
			t.Fatalf("an omitted jurisdiction section must never resolve to BusinessState=COMPLETED")
		}
	})

	t.Run("degraded_observation_routes_to_repair_not_success", func(t *testing.T) {
		setup := mustSetup(t, DegradedObservationEnvironment())
		receipt := mustRun(t, setup)
		if receipt.Terminal.NodeID != NodeEndDegradedRepair {
			t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, NodeEndDegradedRepair, receipt.NodeIDs())
		}
		if receipt.Lifecycle.ConsistencyState == "CONSISTENT" || receipt.Lifecycle.BusinessState == "COMPLETED" {
			t.Fatalf("a degraded observation must never be reported as a consistent, completed outcome: %+v", receipt.Lifecycle)
		}
		node, ok := setup.Plan.Node(NodeEndDegradedRepair)
		if !ok || node.Terminal == nil || len(node.Terminal.RepairRefs) == 0 {
			t.Fatalf("degraded terminal carries no RepairRefs; a degraded dimension must create bounded repair evidence")
		}
	})

	t.Run("unavailable_destination_is_a_conflict_not_a_partial_success", func(t *testing.T) {
		setup := mustSetup(t, UnavailableDestinationEnvironment())
		receipt := mustRun(t, setup)
		if receipt.Terminal.NodeID != NodeEndConflictBlocked {
			t.Fatalf("terminal = %q, want %q; an unavailable destination position must never be called a success", receipt.Terminal.NodeID, NodeEndConflictBlocked)
		}
	})
}

// TestTodo_CONF_003_Security proves the REFACTOR clause structurally and at
// runtime: source and destination companies remain separate authority
// scopes, and a write-class node - a hidden effect - cannot execute under
// SIMULATE no matter how it is reached.
func TestTodo_CONF_003_Security(t *testing.T) {
	t.Run("source_and_destination_capabilities_declare_distinct_authority_scopes", func(t *testing.T) {
		def := ReferenceDefinition()
		var sourceScopes, destScopes []string
		for _, n := range def.Nodes {
			switch n.ID {
			case NodeReadSourceAuthority:
				sourceScopes = n.Capability.AuthorityScopes
			case NodeReadDestinationAuthority:
				destScopes = n.Capability.AuthorityScopes
			}
		}
		if len(sourceScopes) == 0 || len(destScopes) == 0 {
			t.Fatalf("both company reads must declare an authority scope: source=%v destination=%v", sourceScopes, destScopes)
		}
		for _, s := range sourceScopes {
			for _, d := range destScopes {
				if s == d {
					t.Fatalf("source and destination reads share authority scope %q; companies are not separate authority scopes", s)
				}
			}
		}
	})

	t.Run("collapsed_authority_scopes_are_blocked_at_runtime", func(t *testing.T) {
		// The conflict footprint transform does not itself compare scopes
		// (ports.go): this proves the DECISION's own separateness check is
		// what blocks a collapsed scope, not an accident of the footprint.
		receipt := mustRun(t, mustSetup(t, SameScopeEnvironment()))
		if receipt.Terminal.NodeID != NodeEndConflictBlocked {
			t.Fatalf("terminal = %q, want %q; collapsed authority scopes must be blocked", receipt.Terminal.NodeID, NodeEndConflictBlocked)
		}
	})

	t.Run("hidden_write_effect_cannot_execute_under_simulate", func(t *testing.T) {
		invoked := false
		plan := mustCompileHiddenEffectPlan(t, func() { invoked = true })

		if err := simulate.Admit(plan); err == nil {
			t.Fatal("Admit accepted a plan containing a write-class node")
		} else if code := simulate.CodeOf(err); code != simulate.CodeWriteEffectInSimulate {
			t.Fatalf("Admit refusal code = %q, want %q (%v)", code, simulate.CodeWriteEffectInSimulate, err)
		}

		registry := hiddenEffectRegistry(t, func() { invoked = true })
		_, err := simulate.Run(context.Background(), plan, simulate.Inputs{
			Values: simulate.Bag{"worker_id": simulate.NewBranded("WorkerID", "11111111-1111-4111-8111-111111111111")},
		}, simulate.Options{Capabilities: registry})
		if err == nil {
			t.Fatal("Run executed a plan containing a write-class node")
		}
		if !errors.Is(err, simulate.ErrSimulate) {
			t.Errorf("refusal does not unwrap to ErrSimulate: %v", err)
		}
		if invoked {
			t.Fatal("the mutating capability handler was reached; SIMULATE must refuse, not suppress a hidden effect")
		}
	})
}

// TestTodo_CONF_003_Conformance proves the whole terminal lattice at once:
// every reachable terminal reports the exact five-dimension tuple CONF-003's
// GREEN clause requires, never a collapsed or approximate one.
func TestTodo_CONF_003_Conformance(t *testing.T) {
	cases := []struct {
		name     string
		env      *Environment
		terminal string
		want     simulate.LifecycleState
	}{
		{
			"pending_approvals", GoldenEnvironment(), NodeEndPendingApprovals,
			simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_STARTED", ConsistencyState: "PENDING_OBSERVATION", ObligationState: "PENDING"},
		},
		{
			"conflict_blocked", SameScopeEnvironment(), NodeEndConflictBlocked,
			simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"},
		},
		{
			"degraded_repair", DegradedObservationEnvironment(), NodeEndDegradedRepair,
			simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "BLOCKED", BusinessState: "UNKNOWN", ConsistencyState: "UNKNOWN", ObligationState: "PENDING"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			receipt := mustRun(t, mustSetup(t, c.env))
			if receipt.Terminal.NodeID != c.terminal {
				t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, c.terminal, receipt.NodeIDs())
			}
			if receipt.Lifecycle != c.want {
				t.Fatalf("lifecycle = %+v, want %+v", receipt.Lifecycle, c.want)
			}
		})
	}

	t.Run("missing_context_unknown", func(t *testing.T) {
		setup, err := NewSetupMissingLegalContext(GoldenEnvironment())
		if err != nil {
			t.Fatalf("NewSetupMissingLegalContext: %v", err)
		}
		receipt := mustRun(t, setup)
		want := simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "BLOCKED", BusinessState: "UNKNOWN", ConsistencyState: "UNKNOWN", ObligationState: "PENDING"}
		if receipt.Terminal.NodeID != NodeEndUnknown || receipt.Lifecycle != want {
			t.Fatalf("terminal = %q lifecycle = %+v, want %q %+v", receipt.Terminal.NodeID, receipt.Lifecycle, NodeEndUnknown, want)
		}
	})
}

// TestTodo_CONF_003_Mutation proves the authority-and-conflict DECISION's
// scope-equality check is independently load-bearing: the conflict-footprint
// TRANSFORM does not itself flag equal scopes as a conflict, so a mutation
// that deleted the DECISION's own check would let a collapsed-scope transfer
// reach the pending-approvals terminal. This test fails the moment that
// happens.
func TestTodo_CONF_003_Mutation(t *testing.T) {
	result, err := Transforms{}.Transform(context.Background(), simulate.TransformRequest{
		Transform: workflow.CompiledTransform{TransformRef: TransformConflictFootprint},
		Inputs: simulate.Bag{
			"source_authority_scope":         simulate.NewString("scope-authority:same"),
			"destination_authority_scope":    simulate.NewString("scope-authority:same"),
			"source_employment_active":       simulate.NewBool(true),
			"destination_position_available": simulate.NewBool(true),
			"destination_budget_available":   simulate.NewBool(true),
		},
	})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	conflictDetected, err := result.Outputs["conflict_detected"].Bool()
	if err != nil {
		t.Fatalf("conflict_detected: %v", err)
	}
	if conflictDetected {
		t.Fatal("the footprint transform must not itself treat equal scopes as a conflict; that is the DECISION's job")
	}

	receipt := mustRun(t, mustSetup(t, SameScopeEnvironment()))
	if receipt.Terminal.NodeID != NodeEndConflictBlocked {
		t.Fatalf("terminal = %q, want %q; the DECISION's own scope check must still block a collapsed-scope transfer "+
			"even though the footprint transform reported no conflict", receipt.Terminal.NodeID, NodeEndConflictBlocked)
	}
}

// TestTodo_CONF_003_Race proves the walk is deterministic and safe to run
// concurrently: N independent goroutines, each with its own compiled plan and
// environment, produce byte-identical receipts.
func TestTodo_CONF_003_Race(t *testing.T) {
	const n = 16
	digests := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			setup, err := NewSetup(GoldenEnvironment())
			if err != nil {
				errs[i] = err
				return
			}
			receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = receipt.Digest()
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	for i := 1; i < n; i++ {
		if digests[i] != digests[0] {
			t.Fatalf("run %d digest %q differs from run 0 digest %q", i, digests[i], digests[0])
		}
	}
}

// ---- hidden-effect fixture: a minimal write-class plan compiled under P1B,
// used only to prove CONF-003's "hidden effects" RED case. P1A refuses a
// write effect at compile time, so a P1A plan with a mutating node cannot
// exist; compiling under P1B is what makes the interpreter's own SIMULATE
// refusal the thing under test.

const (
	hiddenCapRead      = "fixture.conformance.transfer.read_worker"
	hiddenCapWrite     = "fixture.conformance.transfer.mutate_worker"
	hiddenCapObserve   = "fixture.conformance.transfer.observe_worker"
	hiddenNodeRead     = "read_worker"
	hiddenNodeSync     = "mutate_worker"
	hiddenNodeObserve  = "observe_worker"
	hiddenNodeCommit   = "end_committed"
	hiddenNodeDegraded = "end_degraded"
	hiddenNodeRepair   = "end_repair"
)

func hiddenSchema(id, slot string) workflow.SchemaRef {
	return workflow.SchemaRef{SchemaID: id + "." + slot + "/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"}
}

func hiddenEffectDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID: "fixture.conformance.transfer.hidden_effect", Version: 1, Name: "Hidden effect fixture",
		InputSchema: workflowSchema("HiddenEffectInput"), OutputSchema: workflowSchema("HiddenEffectResult"), VariablesSchema: workflowSchema("HiddenEffectVariables"),
		TenantScope: "acme", OrganizationScope: "acme/workforce", RiskClass: "HIGH",
		DeclaredModes: []workflow.ExecutionMode{workflow.ModeExecute}, TerminalProfile: workflow.TerminalProfileExecute,
		StartNodeID:      hiddenNodeRead,
		Inputs:           []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}},
		Outputs:          []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}, {Path: "terminal_code", Type: plainStr()}},
		Limits:           workflow.Limits{MaxFanOut: 4, MaxDepth: 8, MaxNodes: 8},
		FailurePolicyRef: "policy.workflow.failure.execute/v1", CancellationPolicyRef: "policy.workflow.cancellation.execute/v1",
		MigrationPolicyRef: "policy.workflow.migration.pinned/v1", RetentionPolicyRef: "policy.workflow.retention.fixture/v1",
		Nodes: []workflow.Node{
			{
				ID: hiddenNodeRead, Type: workflow.StepCapability,
				InputSchema: hiddenSchema(hiddenCapRead, "request"), OutputSchema: hiddenSchema(hiddenCapRead, "response"),
				Inputs:  []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}},
				Outputs: []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: fromInput("worker_id")},
				},
				Capability: &workflow.CapabilityRef{ID: hiddenCapRead, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:people.read"}},
				Governance: governedInvocation(nil, nil, "acme/workforce"),
			},
			{
				ID: hiddenNodeSync, Type: workflow.StepCapability,
				InputSchema: hiddenSchema(hiddenCapWrite, "request"), OutputSchema: hiddenSchema(hiddenCapWrite, "response"),
				Inputs:  []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}},
				Outputs: []workflow.Field{{Path: "submission_id", Type: plainStr()}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: fromNode(hiddenNodeRead, "worker_id")},
				},
				Capability:   &workflow.CapabilityRef{ID: hiddenCapWrite, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:workforce.write"}, IdempotencyKeyMapping: "worker_id", EffectBinding: "workforce.hidden_effect"},
				Retry:        &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.effect.bounded/v1"},
				FailureRoute: hiddenNodeRepair,
				EffectRole:   workflow.RoleDownstreamEffect,
				Governance: workflow.NodeGovernance{
					Purpose: purpose, Classification: classification,
					RequiredDecisions:     []workflow.GovernanceKind{workflow.GovernanceAuthZ, workflow.GovernanceLegal, workflow.GovernancePurpose, workflow.GovernanceRisk},
					RevalidationBoundary:  workflow.RevalidatePreEffect,
					DataAccessManifestRef: dataAccessManifest,
				},
			},
			{
				ID: hiddenNodeObserve, Type: workflow.StepObserve,
				InputSchema: hiddenSchema(hiddenCapObserve, "request"), OutputSchema: hiddenSchema(hiddenCapObserve, "response"),
				Inputs:  []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}},
				Outputs: []workflow.Field{{Path: "observed_status", Type: plainStr()}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: fromNode(hiddenNodeRead, "worker_id")},
				},
				Capability: &workflow.CapabilityRef{ID: hiddenCapObserve, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:workforce.read"}},
				Observe: &workflow.ObserveSpec{
					EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "fixture.provider",
					ExpectedStateFields: []string{"worker_id"}, RequiredWatermarks: []string{"fixture.provider.cursor"},
					MaxAgeSeconds: 600, ComparisonProfile: "comparison.fixture.status/v1",
					RetryExhaustionRoute: hiddenNodeDegraded,
				},
				Retry:      &workflow.RetryPolicy{MaxAttempts: 5, BackoffRef: "policy.retry.observation.bounded/v1"},
				Governance: governedInvocation(nil, nil, "acme/workforce"),
			},
			{
				ID: hiddenNodeCommit, Type: workflow.StepEnd,
				Inputs: terminalInputs(), InputMappings: terminalMappings("COMMITTED"),
				End: &workflow.EndSpec{
					TerminalCode: "COMMITTED", RuntimeStatus: workflow.RuntimeCompleted,
					CompletionMapping: completion("APPROVED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"),
					CommitReceiptRef:  "receipt.fixture/v1",
				},
				Governance: terminalGovernance(nil, nil),
			},
			{
				ID: hiddenNodeDegraded, Type: workflow.StepEnd,
				Inputs: terminalInputs(), InputMappings: terminalMappings("COMMITTED_DEGRADED"),
				End: &workflow.EndSpec{
					TerminalCode: "COMMITTED_DEGRADED", RuntimeStatus: workflow.RuntimeCompleted,
					CompletionMapping: completion("APPROVED", "COMMITTED", "COMPLETED", "DEGRADED", "SATISFIED"),
					CommitReceiptRef:  "receipt.fixture/v1",
					RepairRefs:        []string{"repair.fixture/v1"},
				},
				Governance: terminalGovernance(nil, nil),
			},
			{
				ID: hiddenNodeRepair, Type: workflow.StepEnd,
				Inputs: terminalInputs(), InputMappings: terminalMappings("REPAIR_REQUIRED"),
				End: &workflow.EndSpec{
					TerminalCode: "REPAIR_REQUIRED", RuntimeStatus: workflow.RuntimeRepairRequired,
					CompletionMapping: completion("APPROVED", "REPAIR_REQUIRED", "UNKNOWN", "UNKNOWN", "PENDING"),
					RepairRefs:        []string{"repair.fixture/v1"},
				},
				Governance: terminalGovernance(nil, nil),
			},
		},
		Edges: []workflow.Edge{
			{From: hiddenNodeRead, To: hiddenNodeSync, RouteKey: string(workflow.OutcomeSucceeded)},
			{From: hiddenNodeRead, To: hiddenNodeRepair, RouteKey: string(workflow.OutcomeRejected)},
			{From: hiddenNodeRead, To: hiddenNodeRepair, RouteKey: string(workflow.OutcomeUnknown)},
			{From: hiddenNodeRead, To: hiddenNodeRepair, RouteKey: string(workflow.OutcomeAmbiguous)},

			{From: hiddenNodeSync, To: hiddenNodeObserve, RouteKey: string(workflow.OutcomeSucceeded)},
			{From: hiddenNodeSync, To: hiddenNodeRepair, RouteKey: string(workflow.OutcomeRejected)},
			{From: hiddenNodeSync, To: hiddenNodeRepair, RouteKey: string(workflow.OutcomeUnknown)},
			{From: hiddenNodeSync, To: hiddenNodeRepair, RouteKey: string(workflow.OutcomeAmbiguous)},

			{From: hiddenNodeObserve, To: hiddenNodeCommit, RouteKey: string(workflow.OutcomePass)},
			{From: hiddenNodeObserve, To: hiddenNodeDegraded, RouteKey: string(workflow.OutcomeFail)},
			{From: hiddenNodeObserve, To: hiddenNodeDegraded, RouteKey: string(workflow.OutcomePartial)},
			{From: hiddenNodeObserve, To: hiddenNodeRepair, RouteKey: string(workflow.OutcomeUnknown)},
		},
	}
}

func hiddenEffectRegistry(t *testing.T, reached func()) *capability.Registry {
	t.Helper()
	r := capability.NewRegistry()
	readDef := readOnlyDefinition(hiddenCapRead, "people")
	writeDef := readOnlyDefinition(hiddenCapWrite, "workforce")
	writeDef.EffectClass = capability.EffectExternalMutation
	writeDef.WriteData = capability.DataDomainFieldSet{DataDomains: []string{"workforce"}}
	writeDef.AuthZScopeRef = "scope:workforce.write"
	writeDef.IdempotencyPolicyRef = "idempotency.effect-key.v1"
	writeDef.RiskClass = "HIGH"
	observeDef := readOnlyDefinition(hiddenCapObserve, "workforce")

	readHandler := func(_ context.Context, _ any) (any, error) {
		return simulate.CapabilityResponse{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{"worker_id": simulate.NewBranded("WorkerID", "w1")}}, nil
	}
	writeHandler := func(_ context.Context, _ any) (any, error) {
		reached()
		return simulate.CapabilityResponse{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{"submission_id": simulate.NewString("sub-1")}}, nil
	}
	observeHandler := func(_ context.Context, _ any) (any, error) {
		return simulate.CapabilityResponse{Outcome: workflow.OutcomeUnknown, Outputs: simulate.Bag{}}, nil
	}
	if err := r.Register(readDef, readHandler); err != nil {
		t.Fatalf("publish %s: %v", hiddenCapRead, err)
	}
	if err := r.Register(writeDef, writeHandler); err != nil {
		t.Fatalf("publish %s: %v", hiddenCapWrite, err)
	}
	if err := r.Register(observeDef, observeHandler); err != nil {
		t.Fatalf("publish %s: %v", hiddenCapObserve, err)
	}
	return r
}

func mustCompileHiddenEffectPlan(t *testing.T, reached func()) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := workflow.Compile(hiddenEffectDefinition(), workflow.Options{
		Phase:        workflow.PhaseP1B,
		Capabilities: hiddenEffectRegistry(t, reached),
	})
	if err != nil {
		t.Fatalf("hidden-effect fixture must compile under P1B: %v", err)
	}
	return plan
}

func equalSets(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(want))
	for _, w := range want {
		seen[w] = true
	}
	for _, g := range got {
		if !seen[g] {
			return false
		}
		delete(seen, g)
	}
	return len(seen) == 0
}
