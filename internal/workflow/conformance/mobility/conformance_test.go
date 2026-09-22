package mobility

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

func mustSetup(t *testing.T, env *Environment, params Params) *Setup {
	t.Helper()
	setup, err := NewSetupWithParams(env, params)
	if err != nil {
		t.Fatalf("NewSetupWithParams: %v", err)
	}
	return setup
}
func mustRun(t *testing.T, setup *Setup) simulate.Receipt {
	t.Helper()
	receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return receipt
}

func TestTodo_CONF_015(t *testing.T) {
	setup := mustSetup(t, GoldenEnvironment(), Params{})
	receipt := mustRun(t, setup)
	if receipt.Mode != workflow.ModeSimulate || receipt.WorkflowID != WorkflowID || receipt.PlanDigest != setup.Plan.Digest() {
		t.Fatalf("receipt identity = mode %s workflow %q digest %q", receipt.Mode, receipt.WorkflowID, receipt.PlanDigest)
	}
	wantPath := []string{NodeReadMobilityLegs, NodeReadWorkAuthorization, NodeResolveTaxPE, NodeResolvePrivacy, NodeComputeFootprint, NodeBuildProposal, NodeMobilityDecision, NodeObserveImmigration, NodeEndPendingApprovals}
	got := receipt.NodeIDs()
	if len(got) != len(wantPath) {
		t.Fatalf("path = %v, want %v", got, wantPath)
	}
	for i := range wantPath {
		if got[i] != wantPath[i] {
			t.Fatalf("node %d = %q, want %q", i, got[i], wantPath[i])
		}
	}
	if receipt.Terminal.TerminalCode != "MOBILITY_PENDING_APPROVALS" {
		t.Fatalf("terminal code = %q", receipt.Terminal.TerminalCode)
	}
	wantObligations := []string{ObligationDatedLegEvidence, ObligationAuthorization, ObligationTaxPEReview, ObligationPrivacyReview, ObligationVendorObservation, ObligationRecordsRetention}
	if !equalSets(receipt.Terminal.OutstandingObligationRefs, wantObligations) {
		t.Fatalf("obligations = %v, want %v", receipt.Terminal.OutstandingObligationRefs, wantObligations)
	}
	wantLifecycle := simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_STARTED", ConsistencyState: "PENDING_OBSERVATION", ObligationState: "PENDING"}
	if receipt.Lifecycle != wantLifecycle {
		t.Fatalf("lifecycle = %+v, want %+v", receipt.Lifecycle, wantLifecycle)
	}
	if !receipt.EffectCounters().IsZero() {
		t.Fatalf("simulation counted effects: %v", receipt.EffectCounters().NonZero())
	}
	if len(receipt.WorkItems) != 5 {
		t.Fatalf("work items = %d, want 5", len(receipt.WorkItems))
	}
	if receipt.Digest() != mustRun(t, mustSetup(t, GoldenEnvironment(), Params{})).Digest() {
		t.Fatal("receipt digest is not deterministic")
	}
	if err := receipt.Verify(); err != nil {
		t.Fatalf("receipt does not verify: %v", err)
	}
}

func TestTodo_CONF_015_Security(t *testing.T) {
	t.Run("scoped_reads_and_pinned_contexts", func(t *testing.T) {
		def := ReferenceDefinition()
		var legs, auth, tax, privacy []string
		for _, node := range def.Nodes {
			if node.Capability == nil {
				continue
			}
			switch node.ID {
			case NodeReadMobilityLegs:
				legs = node.Capability.AuthorityScopes
			case NodeReadWorkAuthorization:
				auth = node.Capability.AuthorityScopes
			case NodeResolveTaxPE:
				tax = node.Capability.AuthorityScopes
			case NodeResolvePrivacy:
				privacy = node.Capability.AuthorityScopes
			}
		}
		if len(legs) == 0 || len(auth) == 0 || len(tax) == 0 || len(privacy) == 0 {
			t.Fatalf("missing authority scope: legs=%v auth=%v tax=%v privacy=%v", legs, auth, tax, privacy)
		}
		if legs[0] == auth[0] || tax[0] == privacy[0] {
			t.Fatalf("mobility reads collapsed authority scopes: legs=%v auth=%v tax=%v privacy=%v", legs, auth, tax, privacy)
		}
		if got := mustRun(t, mustSetup(t, ProhibitedPrivacyEnvironment(), Params{})).Terminal.NodeID; got != NodeEndPrivacyBlocked {
			t.Fatalf("privacy terminal = %q", got)
		}
		if got := mustRun(t, mustSetup(t, AuthorizationExpiredEnvironment(), Params{})).Terminal.NodeID; got != NodeEndAuthorizationBlocked {
			t.Fatalf("expired authorization terminal = %q", got)
		}
	})
	t.Run("hidden_write_is_refused_before_handler", func(t *testing.T) {
		invoked := false
		plan := mustCompileHiddenEffectPlan(t, func() { invoked = true })
		if err := simulate.Admit(plan); err == nil {
			t.Fatal("Admit accepted a write-class plan")
		} else if code := simulate.CodeOf(err); code != simulate.CodeWriteEffectInSimulate {
			t.Fatalf("Admit code = %q, want %q", code, simulate.CodeWriteEffectInSimulate)
		}
		_, err := simulate.Run(context.Background(), plan, simulate.Inputs{Values: simulate.Bag{"worker_id": simulate.NewBranded("WorkerID", defaultWorkerID)}}, simulate.Options{Capabilities: hiddenEffectRegistry(t, func() { invoked = true })})
		if err == nil || !errors.Is(err, simulate.ErrSimulate) {
			t.Fatalf("Run error = %v, want simulate refusal", err)
		}
		if invoked {
			t.Fatal("mutating handler was reached")
		}
	})
}

func TestTodo_CONF_015_Conformance(t *testing.T) {
	cases := []struct {
		name        string
		env         *Environment
		params      Params
		terminal    string
		lifecycle   simulate.LifecycleState
		obligations []string
	}{
		{"pending", GoldenEnvironment(), Params{}, NodeEndPendingApprovals, simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_STARTED", ConsistencyState: "PENDING_OBSERVATION", ObligationState: "PENDING"}, []string{ObligationDatedLegEvidence, ObligationAuthorization, ObligationTaxPEReview, ObligationPrivacyReview, ObligationVendorObservation, ObligationRecordsRetention}},
		{"expiring", ExpiringAuthorizationEnvironment(), Params{}, NodeEndExpiringAuthorization, simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_STARTED", ConsistencyState: "PENDING_OBSERVATION", ObligationState: "PENDING"}, []string{ObligationDatedLegEvidence, ObligationAuthorization, ObligationTaxPEReview, ObligationPrivacyReview, ObligationVendorObservation, ObligationRenewalReminder, ObligationRecordsRetention}},
		{"authorization", AuthorizationExpiredEnvironment(), Params{}, NodeEndAuthorizationBlocked, rejectedLifecycle(), []string{ObligationAuthorization, ObligationRecordsRetention}},
		{"milestone", AuthorizationMilestoneEnvironment(), Params{}, NodeEndMilestoneBlocked, rejectedLifecycle(), []string{ObligationAuthorization, ObligationRecordsRetention}},
		{"overlap", OverlappingLegEnvironment(), Params{}, NodeEndOverlappingLegBlocked, rejectedLifecycle(), []string{ObligationDatedLegEvidence, ObligationRecordsRetention}},
		{"privacy", ProhibitedPrivacyEnvironment(), Params{}, NodeEndPrivacyBlocked, rejectedLifecycle(), []string{ObligationPrivacyReview, ObligationRecordsRetention}},
		{"tax_pe_unknown", UnknownTaxPEEnvironment(), Params{}, NodeEndTaxPEUnknown, unknownLifecycle(), []string{ObligationTaxPEReview, ObligationRecordsRetention}},
		{"degraded", DegradedObservationEnvironment(), Params{}, NodeEndDegradedRepair, unknownLifecycle(), []string{ObligationVendorObservation, ObligationRecordsRetention}},
		{"unknown_provider", AmbiguousObservationEnvironment(), Params{}, NodeEndUnknown, unknownLifecycle(), []string{ObligationRecordsRetention}},
		{"missing_legal_context", GoldenEnvironment(), Params{OmitLegalContext: true}, NodeEndUnknown, unknownLifecycle(), []string{ObligationRecordsRetention}},
		{"missing_privacy_context", GoldenEnvironment(), Params{OmitPrivacyContext: true}, NodeEndUnknown, unknownLifecycle(), []string{ObligationRecordsRetention}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			receipt := mustRun(t, mustSetup(t, c.env, c.params))
			if receipt.Terminal.NodeID != c.terminal {
				t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, c.terminal, receipt.NodeIDs())
			}
			if receipt.Lifecycle != c.lifecycle {
				t.Fatalf("lifecycle = %+v, want %+v", receipt.Lifecycle, c.lifecycle)
			}
			if !equalSets(receipt.Terminal.OutstandingObligationRefs, c.obligations) {
				t.Fatalf("obligations = %v, want %v", receipt.Terminal.OutstandingObligationRefs, c.obligations)
			}
		})
	}
}

func TestTodo_CONF_015_Mutation(t *testing.T) {
	result, err := Transforms{}.Transform(context.Background(), simulate.TransformRequest{Transform: workflow.CompiledTransform{TransformRef: TransformMobilityFootprint}, Inputs: simulate.Bag{"mobility_active": simulate.NewBool(true), "overlap_detected": simulate.NewBool(true), "source_jurisdiction": simulate.NewString("US-CA"), "destination_jurisdiction": simulate.NewString("DE-BE"), "home_payroll_group": simulate.NewString("home"), "host_payroll_group": simulate.NewString("host"), "payroll_model": simulate.NewString("SPLIT"), "leg_start": simulate.NewLocalDate(mustDate(t, "2026-12-01")), "leg_end": simulate.NewLocalDate(mustDate(t, "2027-05-31")), "authorization_valid": simulate.NewBool(true), "authorization_milestone_complete": simulate.NewBool(true)}})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	overlap, err := result.Outputs["overlapping_assignment"].Bool()
	if err != nil {
		t.Fatalf("overlap: %v", err)
	}
	if !overlap {
		t.Fatal("footprint did not preserve the overlap fact")
	}
	if got := mustRun(t, mustSetup(t, OverlappingLegEnvironment(), Params{})).Terminal.NodeID; got != NodeEndOverlappingLegBlocked {
		t.Fatalf("decision did not independently block overlap: %q", got)
	}
	if got := mustRun(t, mustSetup(t, DegradedObservationEnvironment(), Params{})).Terminal.NodeID; got != NodeEndDegradedRepair {
		t.Fatalf("vendor observation did not produce repair terminal: %q", got)
	}
}

func rejectedLifecycle() simulate.LifecycleState {
	return simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "PENDING"}
}
func unknownLifecycle() simulate.LifecycleState {
	return simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "BLOCKED", BusinessState: "UNKNOWN", ConsistencyState: "UNKNOWN", ObligationState: "PENDING"}
}
func equalSets(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(want))
	for _, value := range want {
		seen[value] = true
	}
	for _, value := range got {
		if !seen[value] {
			return false
		}
		delete(seen, value)
	}
	return len(seen) == 0
}

func mustDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	value, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

const (
	hiddenCapWrite   = "fixture.conformance.mobility.mutate_case"
	hiddenCapObserve = "fixture.conformance.mobility.observe_case"
)

func hiddenEffectDefinition() workflow.Definition {
	writeGovernance := workflow.NodeGovernance{Purpose: purpose, Classification: classification, RequiredDecisions: []workflow.GovernanceKind{workflow.GovernanceAuthZ, workflow.GovernanceLegal, workflow.GovernancePurpose, workflow.GovernanceRisk}, RevalidationBoundary: workflow.RevalidatePreEffect, DataAccessManifestRef: dataAccessManifest}
	return workflow.Definition{
		WorkflowID: "fixture.conformance.mobility.hidden_effect", Version: 1, Name: "Mobility hidden effect", InputSchema: workflowSchema("HiddenEffectInput"), OutputSchema: workflowSchema("HiddenEffectResult"), VariablesSchema: workflowSchema("HiddenEffectVariables"), TenantScope: "acme", OrganizationScope: organizationScope, RiskClass: "HIGH", DeclaredModes: []workflow.ExecutionMode{workflow.ModeExecute}, TerminalProfile: workflow.TerminalProfileExecute, StartNodeID: "mutate",
		Inputs: []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}}, Outputs: []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}, {Path: "terminal_code", Type: plainStr()}}, Limits: workflow.Limits{MaxFanOut: 4, MaxDepth: 8, MaxNodes: 8}, FailurePolicyRef: "policy.workflow.failure.execute/v1", CancellationPolicyRef: "policy.workflow.cancellation.execute/v1", MigrationPolicyRef: "policy.workflow.migration.pinned/v1", RetentionPolicyRef: "policy.workflow.retention.fixture/v1",
		Nodes: []workflow.Node{
			{ID: "mutate", Type: workflow.StepCapability, InputSchema: capabilitySchema(hiddenCapWrite, "request"), OutputSchema: capabilitySchema(hiddenCapWrite, "response"), Inputs: []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}}, Outputs: []workflow.Field{{Path: "submission_id", Type: plainStr()}}, InputMappings: []workflow.Mapping{{Target: "worker_id", Source: builders.FromInput("worker_id")}}, Capability: &workflow.CapabilityRef{ID: hiddenCapWrite, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:mobility.write"}, IdempotencyKeyMapping: "worker_id", EffectBinding: "mobility.hidden_effect"}, Retry: &workflow.RetryPolicy{MaxAttempts: 2, BackoffRef: "policy.retry.effect.bounded/v1"}, FailureRoute: "repair", EffectRole: workflow.RoleDownstreamEffect, Governance: writeGovernance},
			{ID: "observe", Type: workflow.StepObserve, InputSchema: capabilitySchema(hiddenCapObserve, "request"), OutputSchema: capabilitySchema(hiddenCapObserve, "response"), Inputs: []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}}, Outputs: []workflow.Field{{Path: "observed_state", Type: plainStr()}}, InputMappings: []workflow.Mapping{{Target: "worker_id", Source: builders.FromInput("worker_id")}}, Capability: &workflow.CapabilityRef{ID: hiddenCapObserve, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:mobility.read"}}, Observe: &workflow.ObserveSpec{EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "fixture.mobility.provider", ExpectedStateFields: []string{"worker_id"}, RequiredWatermarks: []string{"fixture.mobility.cursor"}, MaxAgeSeconds: 600, ComparisonProfile: "comparison.fixture.mobility/v1", RetryExhaustionRoute: "repair"}, Retry: &workflow.RetryPolicy{MaxAttempts: 2, BackoffRef: "policy.retry.observation.bounded/v1"}, Governance: invocationGovernance(nil, nil)},
			{ID: "end", Type: workflow.StepEnd, Inputs: builders.TerminalInputs("worker_id", "WorkerID"), InputMappings: builders.TerminalMappings("worker_id", "COMMITTED"), End: &workflow.EndSpec{TerminalCode: "COMMITTED", RuntimeStatus: workflow.RuntimeCompleted, CompletionMapping: builders.Completion("APPROVED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"), CommitReceiptRef: "receipt.fixture/v1"}, Governance: terminalGovernance(nil, nil)},
			{ID: "degraded", Type: workflow.StepEnd, Inputs: builders.TerminalInputs("worker_id", "WorkerID"), InputMappings: builders.TerminalMappings("worker_id", "DEGRADED"), End: &workflow.EndSpec{TerminalCode: "DEGRADED", RuntimeStatus: workflow.RuntimeBlocked, CompletionMapping: builders.Completion("APPROVED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"), RepairRefs: []string{"repair.fixture/v1"}}, Governance: terminalGovernance(nil, nil)},
			{ID: "repair", Type: workflow.StepEnd, Inputs: builders.TerminalInputs("worker_id", "WorkerID"), InputMappings: builders.TerminalMappings("worker_id", "REPAIR_REQUIRED"), End: &workflow.EndSpec{TerminalCode: "REPAIR_REQUIRED", RuntimeStatus: workflow.RuntimeRepairRequired, CompletionMapping: builders.Completion("APPROVED", "REPAIR_REQUIRED", "UNKNOWN", "UNKNOWN", "PENDING"), RepairRefs: []string{"repair.fixture/v1"}}, Governance: terminalGovernance(nil, nil)},
		},
		Edges: []workflow.Edge{{From: "mutate", To: "observe", RouteKey: string(workflow.OutcomeSucceeded)}, {From: "mutate", To: "repair", RouteKey: string(workflow.OutcomeRejected)}, {From: "mutate", To: "repair", RouteKey: string(workflow.OutcomeUnknown)}, {From: "mutate", To: "repair", RouteKey: string(workflow.OutcomeAmbiguous)}, {From: "observe", To: "end", RouteKey: string(workflow.OutcomePass)}, {From: "observe", To: "degraded", RouteKey: string(workflow.OutcomeFail)}, {From: "observe", To: "degraded", RouteKey: string(workflow.OutcomePartial)}, {From: "observe", To: "repair", RouteKey: string(workflow.OutcomeUnknown)}},
	}
}
func hiddenEffectRegistry(t *testing.T, reached func()) *capability.Registry {
	t.Helper()
	r := capability.NewRegistry()
	writeDef := readOnlyDefinition(hiddenCapWrite, "mobility")
	writeDef.EffectClass = capability.EffectExternalMutation
	writeDef.WriteData = capability.DataDomainFieldSet{DataDomains: []string{"mobility"}}
	writeDef.AuthZScopeRef = "scope:mobility.write"
	writeDef.IdempotencyPolicyRef = "idempotency.effect-key.v1"
	writeDef.RiskClass = "HIGH"
	writeHandler := func(_ context.Context, _ any) (any, error) {
		reached()
		return simulate.CapabilityResponse{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{"submission_id": simulate.NewString("sub-1")}}, nil
	}
	observeDef := readOnlyDefinition(hiddenCapObserve, "mobility")
	observeHandler := func(_ context.Context, _ any) (any, error) {
		return simulate.CapabilityResponse{Outcome: workflow.OutcomeUnknown, Outputs: simulate.Bag{}}, nil
	}
	if err := r.Register(writeDef, writeHandler); err != nil {
		t.Fatalf("publish hidden capability: %v", err)
	}
	if err := r.Register(observeDef, observeHandler); err != nil {
		t.Fatalf("publish hidden observe capability: %v", err)
	}
	return r
}
func mustCompileHiddenEffectPlan(t *testing.T, reached func()) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := workflow.Compile(hiddenEffectDefinition(), workflow.Options{Phase: workflow.PhaseP1B, Capabilities: hiddenEffectRegistry(t, reached)})
	if err != nil {
		t.Fatalf("hidden effect fixture must compile: %v", err)
	}
	return plan
}
