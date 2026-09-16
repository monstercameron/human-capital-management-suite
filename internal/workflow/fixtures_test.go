package workflow_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Fixture capability identities for the effect-carrying P1B definition. They
// are deliberately not bootstrap capabilities: P1A's bootstrap table is
// zero-effect by design, and the effect analysis needs something that mutates.
const (
	capReadWorker    = "fixture.people.read_worker"
	capSyncPayroll   = "fixture.payroll.sync_worker"
	capObservePayrol = "fixture.payroll.observe_worker"
	capAgentAnalyze  = "fixture.intelligence.analyze_worker"
	capBurnLetter    = "fixture.documents.mail_letter"
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

// effectsRegistry publishes the fixture capabilities, including mutating ones.
func effectsRegistry(t *testing.T) *capability.Registry {
	t.Helper()
	r := capability.NewRegistry()
	defs := []capability.Definition{
		fixtureCapability(capReadWorker, "people", capability.EffectReadOnly),
		fixtureCapability(capSyncPayroll, "payroll", capability.EffectExternalMutation),
		fixtureCapability(capObservePayrol, "payroll", capability.EffectReadOnly),
		fixtureCapability(capBurnLetter, "documents", capability.EffectIrreversibleExternalMutation),
	}
	agent := fixtureCapability(capAgentAnalyze, "intelligence", capability.EffectReadOnly)
	agent.AgentEligible = true
	defs = append(defs, agent)
	for _, d := range defs {
		if err := r.Register(d, echoHandler); err != nil {
			t.Fatalf("publish fixture capability %s: %v", d.Key(), err)
		}
	}
	return r
}

func effectsOptions(t *testing.T) workflow.Options {
	t.Helper()
	return workflow.Options{Phase: workflow.PhaseP1B, Capabilities: effectsRegistry(t)}
}

func capSchema(id, slot string) workflow.SchemaRef {
	return workflow.SchemaRef{
		SchemaID:         id + "." + slot + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
	}
}

func fixtureSchemaRef(name string) workflow.SchemaRef {
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

func inputSource(path string) workflow.Source {
	return workflow.Source{Kind: workflow.SourceWorkflowInput, Path: path}
}

func nodeSource(nodeID, path string) workflow.Source {
	return workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: nodeID, Path: path}
}

func constantSource(value string) workflow.Source {
	return workflow.Source{Kind: workflow.SourceConstant, Constant: value, Type: stringType()}
}

func governedNode(boundary workflow.RevalidationBoundary) workflow.NodeGovernance {
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

func fixtureTerminalInputs() []workflow.Field {
	return []workflow.Field{
		{Path: "worker_id", Type: brandedString("WorkerID")},
		{Path: "terminal_code", Type: stringType()},
	}
}

func fixtureTerminalMappings(code string) []workflow.Mapping {
	return []workflow.Mapping{
		{Target: "worker_id", Source: inputSource("worker_id")},
		{Target: "terminal_code", Source: constantSource(code)},
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

// effectsDefinition is a P1B definition that really mutates: it reads a
// worker, syncs an external payroll system, observes the result and ends in
// one of a committed, degraded or repair-required terminal. It is the fixture
// the side-effect, idempotency and governance analyses need, because a
// zero-effect P1A plan cannot exercise them.
func effectsDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        "fixture.workflows.payroll_sync",
		Version:           1,
		Name:              "Payroll sync fixture",
		InputSchema:       fixtureSchemaRef("PayrollSyncInput"),
		OutputSchema:      fixtureSchemaRef("PayrollSyncResult"),
		VariablesSchema:   fixtureSchemaRef("PayrollSyncVariables"),
		TenantScope:       "acme",
		OrganizationScope: "acme/engineering",
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeExecute},
		TerminalProfile:   workflow.TerminalProfileExecute,
		StartNodeID:       fxRead,
		Inputs: []workflow.Field{
			{Path: "worker_id", Type: brandedString("WorkerID")},
		},
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
				InputSchema:  capSchema(capReadWorker, "request"),
				OutputSchema: capSchema(capReadWorker, "response"),
				Inputs:       []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
				Outputs: []workflow.Field{
					{Path: "worker_id", Type: brandedString("WorkerID")},
					{Path: "effect_key", Type: stringType()},
				},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: inputSource("worker_id")},
				},
				Capability: &workflow.CapabilityRef{
					ID: capReadWorker, Version: 1,
					OperationMode:   workflow.ModeExecute,
					AuthorityScopes: []string{"scope:people.read"},
				},
				Governance: governedNode(workflow.RevalidatePreExecution),
			},
			{
				ID:           fxSync,
				Type:         workflow.StepCapability,
				InputSchema:  capSchema(capSyncPayroll, "request"),
				OutputSchema: capSchema(capSyncPayroll, "response"),
				Inputs: []workflow.Field{
					{Path: "worker_id", Type: brandedString("WorkerID")},
					{Path: "effect_key", Type: stringType()},
				},
				Outputs: []workflow.Field{{Path: "submission_id", Type: stringType()}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: nodeSource(fxRead, "worker_id")},
					{Target: "effect_key", Source: nodeSource(fxRead, "effect_key")},
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
				Governance:   governedNode(workflow.RevalidatePreEffect),
			},
			{
				ID:           fxObserve,
				Type:         workflow.StepObserve,
				InputSchema:  capSchema(capObservePayrol, "request"),
				OutputSchema: capSchema(capObservePayrol, "response"),
				Inputs: []workflow.Field{
					{Path: "worker_id", Type: brandedString("WorkerID")},
					{Path: "expected_status", Type: stringType()},
				},
				Outputs: []workflow.Field{
					{Path: "observed_status", Type: stringType()},
					{Path: "source_watermark", Type: stringType()},
				},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: nodeSource(fxRead, "worker_id")},
					{Target: "expected_status", Source: constantSource("SYNCED")},
				},
				Capability: &workflow.CapabilityRef{
					ID: capObservePayrol, Version: 1,
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
				Governance: governedNode(workflow.RevalidatePreExecution),
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
				Governance: workflow.NodeGovernance{
					Purpose:               "FIXTURE_EXECUTE",
					Classification:        "CONFIDENTIAL_HR",
					RevalidationBoundary:  workflow.RevalidatePreClosure,
					DataAccessManifestRef: "dam.fixture/v1",
				},
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
				Governance: workflow.NodeGovernance{
					Purpose:               "FIXTURE_EXECUTE",
					Classification:        "CONFIDENTIAL_HR",
					RevalidationBoundary:  workflow.RevalidatePreClosure,
					DataAccessManifestRef: "dam.fixture/v1",
				},
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
				Governance: workflow.NodeGovernance{
					Purpose:               "FIXTURE_EXECUTE",
					Classification:        "CONFIDENTIAL_HR",
					RevalidationBoundary:  workflow.RevalidatePreClosure,
					DataAccessManifestRef: "dam.fixture/v1",
				},
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

// nodeRef returns a pointer to a node inside a definition so a mutation can
// edit it in place.
func nodeRef(t *testing.T, def *workflow.Definition, id string) *workflow.Node {
	t.Helper()
	for i := range def.Nodes {
		if def.Nodes[i].ID == id {
			return &def.Nodes[i]
		}
	}
	t.Fatalf("definition declares no node %q", id)
	return nil
}

// edgeRef returns a pointer to the edge leaving from with route key.
func edgeRef(t *testing.T, def *workflow.Definition, from, routeKey string) *workflow.Edge {
	t.Helper()
	for i := range def.Edges {
		if def.Edges[i].From == from && def.Edges[i].RouteKey == routeKey {
			return &def.Edges[i]
		}
	}
	t.Fatalf("definition declares no edge %s --%s-->", from, routeKey)
	return nil
}

// mutationCase is one targeted edit that must flip a compiling definition into
// a rejection with a named diagnostic.
type mutationCase struct {
	name   string
	mutate func(t *testing.T, def *workflow.Definition)
	code   string
}

// runMutations proves each mutant is killed: the base definition compiles, and
// every single-point edit is rejected with the expected code.
func runMutations(
	t *testing.T,
	base func() workflow.Definition,
	options func(*testing.T) workflow.Options,
	cases []mutationCase,
) {
	t.Helper()
	if _, err := workflow.Compile(base(), options(t)); err != nil {
		t.Fatalf("base definition must compile before mutation: %v", err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := base()
			tc.mutate(t, &def)
			mustReject(t, def, options(t), tc.code)
		})
	}
}
