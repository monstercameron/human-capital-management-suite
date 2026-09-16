package hrcase

import (
	"context"
	"errors"
	"sync"
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

// TestTodo_CONF_014 is the PRIMARY conformance claim: the compiled HR case
// investigation-and-disposition reference workflow walks, through the real
// SIMULATE-mode interpreter, along the golden path CONF-014's GREEN clause
// names - a relationship-scoped assignment fence, a purpose-scoped
// participant read, a retaliation-signal read, a matter-wall/legal-hold
// resolution, an investigator-conflict footprint, a bound disposition
// proposal, a case-disposition decision and a post-decision record-custody
// observation - and ends at a terminal that reports exact, separately-
// tracked lifecycle dimensions with the case's evidence-custody, SLA,
// finding-record and records-retention obligations outstanding rather than
// collapsed into a false success.
func TestTodo_CONF_014(t *testing.T) {
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
		NodeReadCaseAssignment,
		NodeReadParticipantAuthz,
		NodeReadRetaliationSignal,
		NodeResolveMatterWallAndHold,
		NodeComputeCaseFootprint,
		NodeBuildDispositionProposal,
		NodeCaseDecision,
		NodeObserveRecordCustody,
		NodeEndPendingDisposition,
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

	if receipt.Terminal.TerminalCode != "HRCASE_SIMULATION_PENDING_DISPOSITION" {
		t.Errorf("terminal code = %q, want HRCASE_SIMULATION_PENDING_DISPOSITION", receipt.Terminal.TerminalCode)
	}
	wantObligations := []string{ObligationEvidenceCustody, ObligationSLATracking, ObligationFindingRecord, ObligationRecordsRetention}
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

	if len(receipt.WorkItems) != 3 {
		t.Fatalf("work items = %d, want 3 (one per declared approval requirement on this terminal)", len(receipt.WorkItems))
	}
	wantApprovals := []string{ApprovalHRCaseManager, ApprovalEmployeeRelations, ApprovalLegal}
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
	second := mustRun(t, mustSetup(t, GoldenEnvironment(), Params{}))
	if receipt.Digest() != second.Digest() {
		t.Fatalf("receipt digest drifted between runs:\n first  %s\n second %s", receipt.Digest(), second.Digest())
	}
	if err := receipt.Verify(); err != nil {
		t.Errorf("receipt does not verify: %v", err)
	}
}

// TestTodo_CONF_014_Conformance proves the whole terminal lattice at once,
// including that the confidential-evidence-custody obligation is
// independently tracked: the degraded terminal names it and records
// retention, never the unrelated SLA or finding-record obligations that were
// never touched by this particular failure. It also folds in the omitted-
// context and degraded-observation RED cases, since this matrix carries no
// separate FAULT test.
func TestTodo_CONF_014_Conformance(t *testing.T) {
	pendingObligations := []string{ObligationEvidenceCustody, ObligationSLATracking, ObligationFindingRecord, ObligationRecordsRetention}
	escalationObligations := append(append([]string(nil), pendingObligations...), ObligationRetaliationEscalat)

	cases := []struct {
		name        string
		env         *Environment
		params      Params
		terminal    string
		want        simulate.LifecycleState
		obligations []string
	}{
		{
			"pending_disposition", GoldenEnvironment(), Params{}, NodeEndPendingDisposition,
			simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_STARTED", ConsistencyState: "PENDING_OBSERVATION", ObligationState: "PENDING"},
			pendingObligations,
		},
		{
			"retaliation_escalation_pending", RetaliationSignalEnvironment(), Params{}, NodeEndRetaliationEscalationPending,
			simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_STARTED", ConsistencyState: "PENDING_OBSERVATION", ObligationState: "PENDING"},
			escalationObligations,
		},
		{
			"unauthorized_participant_blocked", UnauthorizedParticipantEnvironment(), Params{}, NodeEndUnauthorizedParticipant,
			simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "PENDING"},
			[]string{ObligationRecordsRetention},
		},
		{
			"investigator_conflict_blocked", GoldenEnvironment(), Params{InvestigatorID: defaultSubjectWorkerID}, NodeEndInvestigatorConflictBlocked,
			simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "PENDING"},
			[]string{ObligationRecordsRetention},
		},
		{
			"matter_wall_breach_blocked", MatterWallEnvironment(), Params{}, NodeEndMatterWallBreachBlocked,
			simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "PENDING"},
			[]string{ObligationRecordsRetention},
		},
		{
			"legal_hold_race_blocked", LegalHoldEnvironment(), Params{}, NodeEndLegalHoldRaceBlocked,
			simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "PENDING"},
			[]string{ObligationRecordsRetention},
		},
		{
			"already_disposed_invalid", AlreadyDisposedEnvironment(), Params{}, NodeEndAlreadyDisposedInvalid,
			simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "PENDING"},
			[]string{ObligationRecordsRetention},
		},
		{
			"appeal_requires_reopen", AlreadyDisposedEnvironment(), Params{AppealRequested: true}, NodeEndAppealRequiresReopen,
			simulate.LifecycleState{RequestState: "SUPERSEDED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "PENDING"},
			[]string{ObligationRecordsRetention},
		},
		{
			"degraded_repair", DegradedObservationEnvironment(), Params{}, NodeEndDegradedRepair,
			simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "BLOCKED", BusinessState: "UNKNOWN", ConsistencyState: "UNKNOWN", ObligationState: "PENDING"},
			[]string{ObligationEvidenceCustody, ObligationRecordsRetention},
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
		for _, unrelated := range []string{ObligationSLATracking, ObligationFindingRecord, ObligationRetaliationEscalat} {
			for _, got := range receipt.Terminal.OutstandingObligationRefs {
				if got == unrelated {
					t.Fatalf("degraded record-custody terminal names unrelated obligation %q", unrelated)
				}
			}
		}
	})

	// CONF-014's "appeal overwrites history" RED case: an appeal against an
	// already-disposed case and a plain cancellation against the same
	// already-disposed case must reach two different terminals, and the
	// appeal path must never look like the original disposition simply
	// continuing - it is SUPERSEDED, a distinct intent, never a mutation.
	t.Run("appeal_after_disposition_does_not_mutate_original_history", func(t *testing.T) {
		env := AlreadyDisposedEnvironment()

		withAppeal := mustRun(t, mustSetup(t, env, Params{AppealRequested: true}))
		if withAppeal.Terminal.NodeID != NodeEndAppealRequiresReopen {
			t.Fatalf("appeal_requested=true against a disposed case: terminal = %q, want %q",
				withAppeal.Terminal.NodeID, NodeEndAppealRequiresReopen)
		}
		if withAppeal.Lifecycle.RequestState != "SUPERSEDED" {
			t.Fatalf("appeal path RequestState = %q, want SUPERSEDED (a distinct intent, not a mutation of the original)", withAppeal.Lifecycle.RequestState)
		}

		withoutAppeal := mustRun(t, mustSetup(t, env, Params{AppealRequested: false}))
		if withoutAppeal.Terminal.NodeID != NodeEndAlreadyDisposedInvalid {
			t.Fatalf("appeal_requested=false against a disposed case: terminal = %q, want %q",
				withoutAppeal.Terminal.NodeID, NodeEndAlreadyDisposedInvalid)
		}
		if withAppeal.Terminal.NodeID == withoutAppeal.Terminal.NodeID {
			t.Fatal("appeal_requested made no difference to the terminal reached; the REFACTOR clause's reopen route is not independently reachable")
		}
	})
}

// TestTodo_CONF_014_Security proves case access is relationship- and
// purpose-scoped structurally and at runtime, that an investigator cannot
// investigate their own case, and that a write-class node - a hidden effect
// - cannot execute under SIMULATE no matter how it is reached.
func TestTodo_CONF_014_Security(t *testing.T) {
	t.Run("case_access_is_relationship_and_purpose_scoped", func(t *testing.T) {
		def := ReferenceDefinition()
		var assignmentScopes, participantScopes []string
		for _, n := range def.Nodes {
			switch n.ID {
			case NodeReadCaseAssignment:
				assignmentScopes = n.Capability.AuthorityScopes
			case NodeReadParticipantAuthz:
				participantScopes = n.Capability.AuthorityScopes
			}
		}
		if len(assignmentScopes) == 0 || len(participantScopes) == 0 {
			t.Fatalf("both the assignment fence and the participant read must declare an authority scope: assignment=%v participant=%v", assignmentScopes, participantScopes)
		}
		for _, a := range assignmentScopes {
			for _, p := range participantScopes {
				if a == p {
					t.Fatalf("assignment and participant reads share authority scope %q; case access is not both relationship- and purpose-scoped", a)
				}
			}
		}

		// A participant lacking the case's purpose scope is refused at
		// runtime, not merely by a UI-level check: the compiled DECISION
		// itself blocks it.
		receipt := mustRun(t, mustSetup(t, UnauthorizedParticipantEnvironment(), Params{}))
		if receipt.Terminal.NodeID != NodeEndUnauthorizedParticipant {
			t.Fatalf("terminal = %q, want %q; an unauthorized/out-of-purpose-scope participant must be refused at runtime", receipt.Terminal.NodeID, NodeEndUnauthorizedParticipant)
		}
	})

	t.Run("investigator_equals_subject_is_blocked_at_runtime", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, GoldenEnvironment(), Params{InvestigatorID: defaultSubjectWorkerID}))
		if receipt.Terminal.NodeID != NodeEndInvestigatorConflictBlocked {
			t.Fatalf("terminal = %q, want %q; an investigator investigating themselves must be blocked", receipt.Terminal.NodeID, NodeEndInvestigatorConflictBlocked)
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
			Values: simulate.Bag{"case_id": simulate.NewBranded("CaseID", defaultCaseID)},
		}, simulate.Options{Capabilities: registry})
		if err == nil {
			t.Fatal("Run executed a plan containing a write-class node")
		}
		if !errors.Is(err, simulate.ErrSimulate) {
			t.Errorf("refusal does not unwrap to ErrSimulate: %v", err)
		}
		if invoked {
			t.Fatal("the mutating capability handler was reached; SIMULATE must refuse, not suppress a hidden write to the confidential case record")
		}
	})
}

// TestTodo_CONF_014_Race proves the walk is deterministic and safe to run
// concurrently: N independent goroutines, each with its own compiled plan and
// environment, produce byte-identical receipts.
func TestTodo_CONF_014_Race(t *testing.T) {
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
// used only to prove CONF-014's "hidden effects" RED case. P1A refuses a
// write effect at compile time, so a P1A plan with a mutating node cannot
// exist; compiling under P1B is what makes the interpreter's own SIMULATE
// refusal the thing under test.

const (
	hiddenCapRead      = "fixture.conformance.hrcase.read_case"
	hiddenCapWrite     = "fixture.conformance.hrcase.mutate_case_record"
	hiddenCapObserve   = "fixture.conformance.hrcase.observe_case"
	hiddenNodeRead     = "read_case"
	hiddenNodeSync     = "mutate_case_record"
	hiddenNodeObserve  = "observe_case"
	hiddenNodeCommit   = "end_committed"
	hiddenNodeDegraded = "end_degraded"
	hiddenNodeRepair   = "end_repair"
)

func hiddenSchema(id, slot string) workflow.SchemaRef {
	return workflow.SchemaRef{SchemaID: id + "." + slot + "/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"}
}

func hiddenEffectDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID: "fixture.conformance.hrcase.hidden_effect", Version: 1, Name: "Hidden effect fixture",
		InputSchema: workflowSchema("HiddenEffectInput"), OutputSchema: workflowSchema("HiddenEffectResult"), VariablesSchema: workflowSchema("HiddenEffectVariables"),
		TenantScope: "acme", OrganizationScope: "acme/employee_relations", RiskClass: "HIGH",
		DeclaredModes: []workflow.ExecutionMode{workflow.ModeExecute}, TerminalProfile: workflow.TerminalProfileExecute,
		StartNodeID:      hiddenNodeRead,
		Inputs:           []workflow.Field{{Path: "case_id", Type: str("CaseID")}},
		Outputs:          []workflow.Field{{Path: "case_id", Type: str("CaseID")}, {Path: "terminal_code", Type: plainStr()}},
		Limits:           workflow.Limits{MaxFanOut: 4, MaxDepth: 8, MaxNodes: 8},
		FailurePolicyRef: "policy.workflow.failure.execute/v1", CancellationPolicyRef: "policy.workflow.cancellation.execute/v1",
		MigrationPolicyRef: "policy.workflow.migration.pinned/v1", RetentionPolicyRef: "policy.workflow.retention.fixture/v1",
		Nodes: []workflow.Node{
			{
				ID: hiddenNodeRead, Type: workflow.StepCapability,
				InputSchema: hiddenSchema(hiddenCapRead, "request"), OutputSchema: hiddenSchema(hiddenCapRead, "response"),
				Inputs:  []workflow.Field{{Path: "case_id", Type: str("CaseID")}},
				Outputs: []workflow.Field{{Path: "case_id", Type: str("CaseID")}},
				InputMappings: []workflow.Mapping{
					{Target: "case_id", Source: fromInput("case_id")},
				},
				Capability: &workflow.CapabilityRef{ID: hiddenCapRead, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:hrcase.assignment.read"}},
				Governance: governedInvocation(nil, nil),
			},
			{
				ID: hiddenNodeSync, Type: workflow.StepCapability,
				InputSchema: hiddenSchema(hiddenCapWrite, "request"), OutputSchema: hiddenSchema(hiddenCapWrite, "response"),
				Inputs:  []workflow.Field{{Path: "case_id", Type: str("CaseID")}},
				Outputs: []workflow.Field{{Path: "submission_id", Type: plainStr()}},
				InputMappings: []workflow.Mapping{
					{Target: "case_id", Source: fromNode(hiddenNodeRead, "case_id")},
				},
				Capability:   &workflow.CapabilityRef{ID: hiddenCapWrite, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:hrcase.records.write"}, IdempotencyKeyMapping: "case_id", EffectBinding: "hrcase.hidden_effect"},
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
				Inputs:  []workflow.Field{{Path: "case_id", Type: str("CaseID")}},
				Outputs: []workflow.Field{{Path: "observed_status", Type: plainStr()}},
				InputMappings: []workflow.Mapping{
					{Target: "case_id", Source: fromNode(hiddenNodeRead, "case_id")},
				},
				Capability: &workflow.CapabilityRef{ID: hiddenCapObserve, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:hrcase.records.read"}},
				Observe: &workflow.ObserveSpec{
					EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "fixture.provider",
					ExpectedStateFields: []string{"case_id"}, RequiredWatermarks: []string{"fixture.provider.cursor"},
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
	readDef := readOnlyDefinition(hiddenCapRead, "hrcase.assignment")
	writeDef := readOnlyDefinition(hiddenCapWrite, "hrcase.records")
	writeDef.EffectClass = capability.EffectExternalMutation
	writeDef.WriteData = capability.DataDomainFieldSet{DataDomains: []string{"hrcase.records"}}
	writeDef.AuthZScopeRef = "scope:hrcase.records.write"
	writeDef.IdempotencyPolicyRef = "idempotency.effect-key.v1"
	writeDef.RiskClass = "HIGH"
	observeDef := readOnlyDefinition(hiddenCapObserve, "hrcase.records")

	readHandler := func(_ context.Context, _ any) (any, error) {
		return simulate.CapabilityResponse{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{"case_id": simulate.NewBranded("CaseID", defaultCaseID)}}, nil
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
