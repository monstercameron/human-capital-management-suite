package termination

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// mustSetup wires env/params against the compiled reference workflow,
// failing the test with the full diagnostic set if the reference stops
// compiling.
func mustSetup(t *testing.T, env *Environment, params Params) *Setup {
	t.Helper()
	setup, err := NewSetupWithParams(env, params)
	if err != nil {
		t.Fatalf("NewSetupWithParams: %v", err)
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

// TestTodo_CONF_005 is the PRIMARY conformance claim: the compiled
// termination-and-offboarding reference workflow walks, through the real
// SIMULATE-mode interpreter, along the golden path CONF-005's GREEN clause
// names - employment/authority read, legal-hold/jurisdiction resolution,
// final-pay computation, a bound proposal, an approval-and-separation-of-
// duties decision and a P0 access-revocation observation - and ends at a
// terminal that reports exact, separately-tracked lifecycle dimensions with
// every one of the termination's six obligations (final pay, P0 access
// revocation, notice/evidence, records retention, equipment, benefits)
// outstanding rather than collapsed into a false success.
func TestTodo_CONF_005(t *testing.T) {
	setup := mustSetup(t, GoldenEnvironment(), Params{})
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
		NodeReadEmploymentAuthority,
		NodeResolveLegalHolds,
		NodeComputeFinalPay,
		NodeBuildProposal,
		NodeApprovalDecision,
		NodeObserveAccessRevocation,
		NodeEndPendingObligations,
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

	if receipt.Terminal.TerminalCode != "TERMINATION_SIMULATION_PENDING_OBLIGATIONS" {
		t.Errorf("terminal code = %q, want TERMINATION_SIMULATION_PENDING_OBLIGATIONS", receipt.Terminal.TerminalCode)
	}
	wantObligations := []string{
		ObligationFinalPay, ObligationAccessRevocation, ObligationNoticeEvidence,
		ObligationRecordsRetention, ObligationEquipment, ObligationBenefits,
	}
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
	wantApprovals := []string{ApprovalHR, ApprovalLegal, ApprovalManager, ApprovalEmployeeRelations, ApprovalFinance}
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

	second := mustRun(t, mustSetup(t, GoldenEnvironment(), Params{}))
	if receipt.Digest() != second.Digest() {
		t.Fatalf("receipt digest drifted between runs:\n first  %s\n second %s", receipt.Digest(), second.Digest())
	}
	if err := receipt.Verify(); err != nil {
		t.Errorf("receipt does not verify: %v", err)
	}
}

// TestTodo_CONF_005_Fault exercises the RED cases CONF-005 names: an
// omitted section (jurisdiction/legal-hold context, without which a
// final-pay deadline cannot legitimately exist), a blocked approval (a
// legal hold, and a requester acting as their own approver), a degraded P0
// access-revocation observation that must never be folded into success, and
// bounded rather than blind retry on that same observation.
func TestTodo_CONF_005_Fault(t *testing.T) {
	t.Run("omitted_legal_context_blocks_final_pay_not_success", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, GoldenEnvironment(), Params{OmitLegalContext: true}))
		if receipt.Terminal.NodeID != NodeEndUnknown {
			t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, NodeEndUnknown, receipt.NodeIDs())
		}
		if receipt.Lifecycle.BusinessState == "COMPLETED" {
			t.Fatalf("an omitted legal-hold/jurisdiction section must never resolve to BusinessState=COMPLETED")
		}
	})

	t.Run("legal_hold_blocks_rather_than_proceeds", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, LegalHoldEnvironment(), Params{}))
		if receipt.Terminal.NodeID != NodeEndLegalHoldBlocked {
			t.Fatalf("terminal = %q, want %q; an active legal hold must block, not be silently proceeded past", receipt.Terminal.NodeID, NodeEndLegalHoldBlocked)
		}
		if receipt.Lifecycle.RequestState != "REJECTED" {
			t.Fatalf("lifecycle request state = %q, want REJECTED", receipt.Lifecycle.RequestState)
		}
	})

	t.Run("requester_as_own_approver_is_refused", func(t *testing.T) {
		// The requester names the current manager as themselves: the
		// requester != approver rule must refuse this rather than resolve
		// it, which is the runtime analog of "revoked approval... is
		// omitted" - an approval this workflow may never grant silently.
		receipt := mustRun(t, mustSetup(t, GoldenEnvironment(), Params{RequesterID: GoldenEnvironment().CurrentManagerID}))
		if receipt.Terminal.NodeID != NodeEndSoDViolationBlocked {
			t.Fatalf("terminal = %q, want %q", receipt.Terminal.NodeID, NodeEndSoDViolationBlocked)
		}
	})

	t.Run("degraded_access_revocation_routes_to_repair_not_success", func(t *testing.T) {
		setup := mustSetup(t, DegradedObservationEnvironment(), Params{})
		receipt := mustRun(t, setup)
		if receipt.Terminal.NodeID != NodeEndDegradedRepair {
			t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, NodeEndDegradedRepair, receipt.NodeIDs())
		}
		if receipt.Lifecycle.ConsistencyState == "CONSISTENT" || receipt.Lifecycle.BusinessState == "COMPLETED" {
			t.Fatalf("a degraded P0 access-revocation observation must never be reported as consistent, completed: %+v", receipt.Lifecycle)
		}
		node, ok := setup.Plan.Node(NodeEndDegradedRepair)
		if !ok || node.Terminal == nil || len(node.Terminal.RepairRefs) == 0 {
			t.Fatalf("degraded terminal carries no RepairRefs; a degraded P0 dimension must create bounded repair evidence")
		}
	})

	t.Run("access_revocation_retry_is_bounded_not_blind", func(t *testing.T) {
		// "Irreversible ambiguity triggers blind retry" is the RED case this
		// proves against: the compiled OBSERVE node declares a finite retry
		// budget and a declared exhaustion route to the repair terminal,
		// never an unbounded or absent retry policy.
		setup := mustSetup(t, DegradedObservationEnvironment(), Params{})
		node, ok := setup.Plan.Node(NodeObserveAccessRevocation)
		if !ok {
			t.Fatalf("plan declares no %s node", NodeObserveAccessRevocation)
		}
		if node.Retry == nil || node.Retry.MaxAttempts == 0 {
			t.Fatalf("P0 access-revocation observation declares no bounded retry policy: %+v", node.Retry)
		}
		if node.Observe == nil || node.Observe.RetryExhaustionRoute != NodeEndDegradedRepair {
			t.Fatalf("retry exhaustion route = %v, want %q", node.Observe, NodeEndDegradedRepair)
		}
	})
}

// TestTodo_CONF_005_Conformance proves the whole terminal lattice at once,
// including that the P0 access-revocation obligation is independently
// tracked: the degraded terminal names it and records retention, never the
// unrelated final-pay, notice, equipment or benefits obligations that were
// never touched by this particular failure.
func TestTodo_CONF_005_Conformance(t *testing.T) {
	cases := []struct {
		name        string
		env         *Environment
		params      Params
		terminal    string
		want        simulate.LifecycleState
		obligations []string
	}{
		{
			"pending_obligations", GoldenEnvironment(), Params{}, NodeEndPendingObligations,
			simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_STARTED", ConsistencyState: "PENDING_OBSERVATION", ObligationState: "PENDING"},
			[]string{ObligationFinalPay, ObligationAccessRevocation, ObligationNoticeEvidence, ObligationRecordsRetention, ObligationEquipment, ObligationBenefits},
		},
		{
			"legal_hold_blocked", LegalHoldEnvironment(), Params{}, NodeEndLegalHoldBlocked,
			simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"},
			nil,
		},
		{
			"sod_violation_blocked", GoldenEnvironment(), Params{RequesterID: GoldenEnvironment().CurrentManagerID}, NodeEndSoDViolationBlocked,
			simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"},
			nil,
		},
		{
			"requires_reinstatement", AlreadyEndedEnvironment(), Params{CancelRequested: true}, NodeEndRequiresReinstate,
			simulate.LifecycleState{RequestState: "SUPERSEDED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"},
			nil,
		},
		{
			"already_ended_invalid", AlreadyEndedEnvironment(), Params{}, NodeEndRejectedInvalid,
			simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"},
			nil,
		},
		{
			"degraded_repair", DegradedObservationEnvironment(), Params{}, NodeEndDegradedRepair,
			simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "BLOCKED", BusinessState: "UNKNOWN", ConsistencyState: "UNKNOWN", ObligationState: "PENDING"},
			[]string{ObligationAccessRevocation, ObligationRecordsRetention},
		},
		{
			"missing_context_unknown", GoldenEnvironment(), Params{OmitLegalContext: true}, NodeEndUnknown,
			simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "BLOCKED", BusinessState: "UNKNOWN", ConsistencyState: "UNKNOWN", ObligationState: "PENDING"},
			[]string{ObligationRecordsRetention},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			receipt := mustRun(t, mustSetup(t, c.env, c.params))
			if receipt.Terminal.NodeID != c.terminal {
				t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, c.terminal, receipt.NodeIDs())
			}
			if receipt.Lifecycle != c.want {
				t.Fatalf("lifecycle = %+v, want %+v", receipt.Lifecycle, c.want)
			}
			if c.obligations != nil && !equalSets(receipt.Terminal.OutstandingObligationRefs, c.obligations) {
				t.Fatalf("outstanding obligations = %v, want %v", receipt.Terminal.OutstandingObligationRefs, c.obligations)
			}
		})
	}

	// The degraded terminal's obligation set must never widen to include an
	// obligation this particular failure never touched: that is what
	// "independently tracked" means in practice.
	t.Run("degraded_repair_does_not_widen_to_unrelated_obligations", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, DegradedObservationEnvironment(), Params{}))
		for _, unrelated := range []string{ObligationFinalPay, ObligationNoticeEvidence, ObligationEquipment, ObligationBenefits} {
			for _, got := range receipt.Terminal.OutstandingObligationRefs {
				if got == unrelated {
					t.Fatalf("degraded P0 access-revocation terminal names unrelated obligation %q", unrelated)
				}
			}
		}
	})
}

// TestTodo_CONF_005_Mutation proves the decision's precedence between the
// reinstatement route and the plain already-ended route is independently
// load-bearing: both environments share EmploymentActive=false, and only
// cancel_requested distinguishes them. A mutation that dropped or reordered
// that check would collapse the two into one outcome; this test fails the
// moment that happens.
func TestTodo_CONF_005_Mutation(t *testing.T) {
	env := AlreadyEndedEnvironment()

	withCancel := mustRun(t, mustSetup(t, env, Params{CancelRequested: true}))
	if withCancel.Terminal.NodeID != NodeEndRequiresReinstate {
		t.Fatalf("cancel_requested=true against an ended employment: terminal = %q, want %q",
			withCancel.Terminal.NodeID, NodeEndRequiresReinstate)
	}

	withoutCancel := mustRun(t, mustSetup(t, env, Params{CancelRequested: false}))
	if withoutCancel.Terminal.NodeID != NodeEndRejectedInvalid {
		t.Fatalf("cancel_requested=false against an ended employment: terminal = %q, want %q",
			withoutCancel.Terminal.NodeID, NodeEndRejectedInvalid)
	}

	if withCancel.Terminal.NodeID == withoutCancel.Terminal.NodeID {
		t.Fatal("cancel_requested made no difference to the terminal reached; the REFACTOR clause's reinstatement route is not independently reachable")
	}
	// Neither path is ever allowed to look like the original request simply
	// completing: the original terminated request is never mutated by a
	// later cancellation.
	if withCancel.Lifecycle.RequestState != "SUPERSEDED" {
		t.Fatalf("reinstatement path RequestState = %q, want SUPERSEDED (a distinct intent, not a mutation of the original)", withCancel.Lifecycle.RequestState)
	}
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

// ---- hidden-effect fixture: a minimal write-class plan compiled under P1B,
// used to prove CONF-005's "hidden effects" RED case independently of
// CONF-003's own proof of the same interpreter guarantee. P1A refuses a
// write effect at compile time, so a P1A plan with a mutating node cannot
// exist; compiling under P1B is what makes the interpreter's own SIMULATE
// refusal the thing under test.

const (
	hiddenCapRead      = "fixture.conformance.termination.read_worker"
	hiddenCapWrite     = "fixture.conformance.termination.revoke_access"
	hiddenCapObserve   = "fixture.conformance.termination.observe_worker"
	hiddenNodeRead     = "read_worker"
	hiddenNodeSync     = "revoke_access"
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
		WorkflowID: "fixture.conformance.termination.hidden_effect", Version: 1, Name: "Hidden effect fixture",
		InputSchema: workflowSchema("HiddenEffectInput"), OutputSchema: workflowSchema("HiddenEffectResult"), VariablesSchema: workflowSchema("HiddenEffectVariables"),
		TenantScope: "acme", OrganizationScope: "acme/lifecycle", RiskClass: "HIGH",
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
				Governance: governedInvocation(nil, nil),
			},
			{
				ID: hiddenNodeSync, Type: workflow.StepCapability,
				InputSchema: hiddenSchema(hiddenCapWrite, "request"), OutputSchema: hiddenSchema(hiddenCapWrite, "response"),
				Inputs:  []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}},
				Outputs: []workflow.Field{{Path: "submission_id", Type: plainStr()}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: fromNode(hiddenNodeRead, "worker_id")},
				},
				Capability:   &workflow.CapabilityRef{ID: hiddenCapWrite, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:iam.write"}, IdempotencyKeyMapping: "worker_id", EffectBinding: "iam.hidden_effect"},
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
				Capability: &workflow.CapabilityRef{ID: hiddenCapObserve, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:iam.read"}},
				Observe: &workflow.ObserveSpec{
					EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "fixture.provider",
					ExpectedStateFields: []string{"worker_id"}, RequiredWatermarks: []string{"fixture.provider.cursor"},
					MaxAgeSeconds: 600, ComparisonProfile: "comparison.fixture.status/v1",
					RetryExhaustionRoute: hiddenNodeDegraded,
				},
				Retry:      &workflow.RetryPolicy{MaxAttempts: 5, BackoffRef: "policy.retry.observation.bounded/v1"},
				Governance: governedInvocation(nil, nil),
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
	writeDef := readOnlyDefinition(hiddenCapWrite, "iam")
	writeDef.EffectClass = capability.EffectExternalMutation
	writeDef.WriteData = capability.DataDomainFieldSet{DataDomains: []string{"iam"}}
	writeDef.AuthZScopeRef = "scope:iam.write"
	writeDef.IdempotencyPolicyRef = "idempotency.effect-key.v1"
	writeDef.RiskClass = "HIGH"
	observeDef := readOnlyDefinition(hiddenCapObserve, "iam")

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

// TestTodo_CONF_005_HiddenEffect proves CONF-005's "hidden effects" RED case
// directly: a privileged IAM write - the exact effect this reference
// workflow's P0 access-revocation obligation is about - cannot execute under
// SIMULATE no matter how it is reached.
func TestTodo_CONF_005_HiddenEffect(t *testing.T) {
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
		t.Fatal("the mutating capability handler was reached; SIMULATE must refuse, not suppress a hidden IAM write")
	}
}
