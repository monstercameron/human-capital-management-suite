package simulate_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// Capability identities of the effect-carrying fixture. They are deliberately
// not bootstrap capabilities: P1A's table is zero-effect by design, and
// proving that a write-class node cannot be simulated needs something that
// really declares a mutation.
const (
	capReadWorker     = "fixture.people.read_worker"
	capSyncPayroll    = "fixture.payroll.sync_worker"
	capObservePayroll = "fixture.payroll.observe_worker"
)

// Node ids of the effect-carrying fixture.
const (
	fxRead        = "read_worker"
	fxSync        = "sync_payroll"
	fxObserve     = "observe_payroll"
	fxEndCommit   = "end_committed"
	fxEndDegraded = "end_degraded"
	fxEndRepair   = "end_repair"
)

func fixtureSchema(id, slot string) capability.SchemaRef {
	return capability.SchemaRef{
		SchemaID:         id + "." + slot + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
	}
}

func fixtureCapability(id, domain string, effect capability.EffectClass) capability.Definition {
	def := capability.Definition{
		ID:                   id,
		Version:              1,
		OwnerDomain:          domain,
		RequestSchema:        fixtureSchema(id, "request"),
		ResponseSchema:       fixtureSchema(id, "response"),
		ErrorSchema:          fixtureSchema(id, "error"),
		EffectClass:          effect,
		ReadData:             capability.DataDomainFieldSet{DataDomains: []string{domain}},
		RiskClass:            "LOW",
		IdempotencyPolicyRef: "idempotency.read-safe.v1",
		AuthZScopeRef:        "scope:" + domain + ".read",
		LegalBasisRef:        "legal.p1a.observation-only.v1",
		EntitlementRef:       "entitlement.pilot.p1a.v1",
		SLOClassRef:          "slo.interactive.p95-2s.v1",
		TestRef:              "conformance:" + id + "/v1",
	}
	if effect.IsWrite() {
		def.WriteData = capability.DataDomainFieldSet{DataDomains: []string{domain}}
		def.AuthZScopeRef = "scope:" + domain + ".write"
		def.IdempotencyPolicyRef = "idempotency.effect-key.v1"
		def.RiskClass = "HIGH"
	}
	return def
}

// mutatingRegistry publishes the fixture capabilities. The mutating one is
// bound to a handler that calls reached, so a test can prove the handler was
// never invoked rather than merely asserting that it should not have been.
func mutatingRegistry(t *testing.T, reached func()) *capability.Registry {
	t.Helper()
	r := capability.NewRegistry()
	defs := []capability.Definition{
		fixtureCapability(capReadWorker, "people", capability.EffectReadOnly),
		fixtureCapability(capSyncPayroll, "payroll", capability.EffectExternalMutation),
		fixtureCapability(capObservePayroll, "payroll", capability.EffectReadOnly),
	}
	for _, def := range defs {
		handler := func(_ context.Context, _ any) (any, error) {
			return simulate.CapabilityResponse{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{}}, nil
		}
		if def.EffectClass.IsWrite() {
			handler = func(_ context.Context, _ any) (any, error) {
				reached()
				return simulate.CapabilityResponse{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{}}, nil
			}
		}
		if err := r.Register(def, handler); err != nil {
			t.Fatalf("publish fixture capability %s: %v", def.Key(), err)
		}
	}
	return r
}

// mustCompileMutatingPlan compiles the effect-carrying fixture under P1B.
//
// P1A refuses a write effect at compile time, so a P1A plan with a mutating
// node cannot exist; compiling under P1B is what makes the interpreter's own
// refusal the thing under test.
func mustCompileMutatingPlan(t *testing.T, reached func()) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := workflow.Compile(mutatingDefinition(), workflow.Options{
		Phase:        workflow.PhaseP1B,
		Capabilities: mutatingRegistry(t, reached),
	})
	if err != nil {
		t.Fatalf("effect-carrying fixture must compile under P1B: %v", err)
	}
	return plan
}

func capSchemaRef(id, slot string) workflow.SchemaRef {
	return workflow.SchemaRef{
		SchemaID:         id + "." + slot + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
	}
}

func fixtureWorkflowSchema(name string) workflow.SchemaRef {
	return workflow.SchemaRef{
		SchemaID:         name + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.workflows.v1." + name,
	}
}

func stringType() workflow.ValueType { return workflow.ValueType{Kind: workflow.KindString} }

func brandedString(brand string) workflow.ValueType {
	return workflow.ValueType{Kind: workflow.KindString, Brand: brand}
}

func fixtureGovernance(boundary workflow.RevalidationBoundary) workflow.NodeGovernance {
	return workflow.NodeGovernance{
		Purpose:        "FIXTURE_EXECUTE",
		Classification: "CONFIDENTIAL_HR",
		RequiredDecisions: []workflow.GovernanceKind{
			workflow.GovernanceAuthZ,
			workflow.GovernanceLegal,
			workflow.GovernancePurpose,
			workflow.GovernanceRisk,
		},
		RevalidationBoundary:  boundary,
		DataAccessManifestRef: "dam.fixture/v1",
	}
}

func fixtureTerminalGovernance() workflow.NodeGovernance {
	return workflow.NodeGovernance{
		Purpose:               "FIXTURE_EXECUTE",
		Classification:        "CONFIDENTIAL_HR",
		RevalidationBoundary:  workflow.RevalidatePreClosure,
		DataAccessManifestRef: "dam.fixture/v1",
	}
}

func fixtureTerminalInputs() []workflow.Field {
	return []workflow.Field{
		{Path: "worker_id", Type: brandedString("WorkerID")},
		{Path: "terminal_code", Type: stringType()},
	}
}

func fixtureTerminalMappings(code string) []workflow.Mapping {
	return []workflow.Mapping{
		{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
		{Target: "terminal_code", Source: workflow.Source{Kind: workflow.SourceConstant, Constant: code, Type: stringType()}},
	}
}

func fixtureCompletion(request, execution, business, consistency, obligation string) map[string]string {
	return map[string]string{
		"RequestState":     request,
		"ExecutionState":   execution,
		"BusinessState":    business,
		"ConsistencyState": consistency,
		"ObligationState":  obligation,
	}
}

// mutatingDefinition reads a worker, syncs an external payroll system,
// observes the result and ends in a committed, degraded or repair terminal.
func mutatingDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        "fixture.workflows.payroll_sync",
		Version:           1,
		Name:              "Payroll sync fixture",
		InputSchema:       fixtureWorkflowSchema("PayrollSyncInput"),
		OutputSchema:      fixtureWorkflowSchema("PayrollSyncResult"),
		VariablesSchema:   fixtureWorkflowSchema("PayrollSyncVariables"),
		TenantScope:       "acme",
		OrganizationScope: "acme/engineering",
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeExecute},
		TerminalProfile:   workflow.TerminalProfileExecute,
		StartNodeID:       fxRead,
		Inputs:            []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
		Outputs: []workflow.Field{
			{Path: "worker_id", Type: brandedString("WorkerID")},
			{Path: "terminal_code", Type: stringType()},
		},
		Limits:                workflow.Limits{MaxFanOut: 4, MaxDepth: 10, MaxNodes: 12},
		FailurePolicyRef:      "policy.workflow.failure.execute/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.execute/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.payroll/v1",
		Nodes: []workflow.Node{
			{
				ID:           fxRead,
				Type:         workflow.StepCapability,
				InputSchema:  capSchemaRef(capReadWorker, "request"),
				OutputSchema: capSchemaRef(capReadWorker, "response"),
				Inputs:       []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
				Outputs: []workflow.Field{
					{Path: "worker_id", Type: brandedString("WorkerID")},
					{Path: "effect_key", Type: stringType()},
				},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
				},
				Capability: &workflow.CapabilityRef{
					ID: capReadWorker, Version: 1,
					OperationMode:   workflow.ModeExecute,
					AuthorityScopes: []string{"scope:people.read"},
				},
				Governance: fixtureGovernance(workflow.RevalidatePreExecution),
			},
			{
				ID:           fxSync,
				Type:         workflow.StepCapability,
				InputSchema:  capSchemaRef(capSyncPayroll, "request"),
				OutputSchema: capSchemaRef(capSyncPayroll, "response"),
				Inputs: []workflow.Field{
					{Path: "worker_id", Type: brandedString("WorkerID")},
					{Path: "effect_key", Type: stringType()},
				},
				Outputs: []workflow.Field{{Path: "submission_id", Type: stringType()}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: fxRead, Path: "worker_id"}},
					{Target: "effect_key", Source: workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: fxRead, Path: "effect_key"}},
				},
				Capability: &workflow.CapabilityRef{
					ID: capSyncPayroll, Version: 1,
					OperationMode:         workflow.ModeExecute,
					AuthorityScopes:       []string{"scope:payroll.write"},
					IdempotencyKeyMapping: "effect_key",
					EffectBinding:         "payroll.worker_sync",
				},
				Retry:        &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.effect.bounded/v1"},
				FailureRoute: fxEndRepair,
				EffectRole:   workflow.RoleDownstreamEffect,
				Governance:   fixtureGovernance(workflow.RevalidatePreEffect),
			},
			{
				ID:           fxObserve,
				Type:         workflow.StepObserve,
				InputSchema:  capSchemaRef(capObservePayroll, "request"),
				OutputSchema: capSchemaRef(capObservePayroll, "response"),
				Inputs: []workflow.Field{
					{Path: "worker_id", Type: brandedString("WorkerID")},
					{Path: "expected_status", Type: stringType()},
				},
				Outputs: []workflow.Field{
					{Path: "observed_status", Type: stringType()},
					{Path: "source_watermark", Type: stringType()},
				},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: fxRead, Path: "worker_id"}},
					{Target: "expected_status", Source: workflow.Source{Kind: workflow.SourceConstant, Constant: "SYNCED", Type: stringType()}},
				},
				Capability: &workflow.CapabilityRef{
					ID: capObservePayroll, Version: 1,
					OperationMode:   workflow.ModeExecute,
					AuthorityScopes: []string{"scope:payroll.read"},
				},
				Observe: &workflow.ObserveSpec{
					EvidenceKind:         workflow.EvidenceAuthoritativeRead,
					SourceAuthority:      "payroll.provider.acme",
					ExpectedStateFields:  []string{"expected_status"},
					RequiredWatermarks:   []string{"payroll.provider.acme.cursor"},
					MaxAgeSeconds:        600,
					ComparisonProfile:    "comparison.payroll.worker_status/v1",
					RetryExhaustionRoute: fxEndDegraded,
				},
				Retry:      &workflow.RetryPolicy{MaxAttempts: 5, BackoffRef: "policy.retry.observation.bounded/v1"},
				Governance: fixtureGovernance(workflow.RevalidatePreExecution),
			},
			{
				ID:            fxEndCommit,
				Type:          workflow.StepEnd,
				Inputs:        fixtureTerminalInputs(),
				InputMappings: fixtureTerminalMappings("COMMITTED"),
				End: &workflow.EndSpec{
					TerminalCode:      "COMMITTED",
					RuntimeStatus:     workflow.RuntimeCompleted,
					CompletionMapping: fixtureCompletion("APPROVED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"),
					CommitReceiptRef:  "receipt.fixture/v1",
				},
				Governance: fixtureTerminalGovernance(),
			},
			{
				ID:            fxEndDegraded,
				Type:          workflow.StepEnd,
				Inputs:        fixtureTerminalInputs(),
				InputMappings: fixtureTerminalMappings("COMMITTED_DEGRADED"),
				End: &workflow.EndSpec{
					TerminalCode:      "COMMITTED_DEGRADED",
					RuntimeStatus:     workflow.RuntimeCompleted,
					CompletionMapping: fixtureCompletion("APPROVED", "COMMITTED", "COMPLETED", "DEGRADED", "SATISFIED"),
					CommitReceiptRef:  "receipt.fixture/v1",
					RepairRefs:        []string{"repair.fixture/v1"},
				},
				Governance: fixtureTerminalGovernance(),
			},
			{
				ID:            fxEndRepair,
				Type:          workflow.StepEnd,
				Inputs:        fixtureTerminalInputs(),
				InputMappings: fixtureTerminalMappings("REPAIR_REQUIRED"),
				End: &workflow.EndSpec{
					TerminalCode:      "REPAIR_REQUIRED",
					RuntimeStatus:     workflow.RuntimeRepairRequired,
					CompletionMapping: fixtureCompletion("APPROVED", "REPAIR_REQUIRED", "UNKNOWN", "UNKNOWN", "PENDING"),
					RepairRefs:        []string{"repair.fixture/v1"},
				},
				Governance: fixtureTerminalGovernance(),
			},
		},
		Edges: []workflow.Edge{
			{From: fxRead, To: fxSync, RouteKey: "SUCCEEDED"},
			{From: fxRead, To: fxEndRepair, RouteKey: "REJECTED"},
			{From: fxRead, To: fxEndRepair, RouteKey: "UNKNOWN"},
			{From: fxRead, To: fxEndRepair, RouteKey: "AMBIGUOUS"},

			{From: fxSync, To: fxObserve, RouteKey: "SUCCEEDED"},
			{From: fxSync, To: fxEndRepair, RouteKey: "REJECTED"},
			{From: fxSync, To: fxEndRepair, RouteKey: "UNKNOWN"},
			{From: fxSync, To: fxEndRepair, RouteKey: "AMBIGUOUS"},

			{From: fxObserve, To: fxEndCommit, RouteKey: "PASS"},
			{From: fxObserve, To: fxEndDegraded, RouteKey: "FAIL"},
			{From: fxObserve, To: fxEndDegraded, RouteKey: "PARTIAL"},
			{From: fxObserve, To: fxEndRepair, RouteKey: "UNKNOWN"},
		},
	}
}
