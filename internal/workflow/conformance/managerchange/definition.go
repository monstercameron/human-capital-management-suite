// Package managerchange contains the P1A Manager Change reference workflow.
//
// The package is a compiler and simulation fixture only. Its one planned
// write is the manager relationship in the shared WorkflowSimulationContract;
// the compiled workflow itself is read-only and pure, so SIMULATE can never
// call an HRIS write boundary.
package managerchange

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
)

const (
	WorkflowID = "hcmnext.workflows.change_manager"
	Version    = 1
)

const (
	NodeReadWorker  = "read_worker"
	NodeReadManager = "read_proposed_manager"
	NodeValidate    = "validate_manager_change"
	NodeSimulate    = "simulate_manager_change"
	NodeEndComplete = "end_simulated_complete"
	NodeEndRejected = "end_simulated_rejected"
)

const (
	CapabilityWorker  = "hcmnext.conformance.managerchange.read_worker"
	CapabilityManager = "hcmnext.conformance.managerchange.read_manager"
	TransformValidate = "transforms.managerchange.validate"
	TransformSimulate = "transforms.managerchange.simulate"
)

const (
	purpose             = "SIMULATE_MANAGER_CHANGE"
	classification      = "CONFIDENTIAL_HR"
	organizationScope   = "acme/people"
	dataAccessManifest  = "dam.manager_change.simulation/v1"
	managerWriteBinding = "people.manager_relationship"
)

func branded(brand string) workflow.ValueType {
	return workflow.ValueType{Kind: workflow.KindString, Brand: brand}
}

func plainString() workflow.ValueType { return workflow.ValueType{Kind: workflow.KindString} }
func boolean() workflow.ValueType     { return workflow.ValueType{Kind: workflow.KindBool} }
func localDate() workflow.ValueType   { return workflow.ValueType{Kind: workflow.KindLocalDate} }

func schema(name string) workflow.SchemaRef {
	return workflow.SchemaRef{
		SchemaID:         name + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.workflows.v1." + name,
	}
}

func capabilitySchema(id, slot string) workflow.SchemaRef {
	return workflow.SchemaRef{
		SchemaID:         id + "." + slot + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
	}
}

func governance(boundary workflow.RevalidationBoundary) workflow.NodeGovernance {
	return workflow.NodeGovernance{
		Purpose:               purpose,
		Classification:        classification,
		RequiredDecisions:     []workflow.GovernanceKind{workflow.GovernanceAuthZ, workflow.GovernanceLegal, workflow.GovernancePurpose, workflow.GovernanceRisk},
		RevalidationBoundary:  boundary,
		DataAccessManifestRef: dataAccessManifest,
	}
}

func capabilityDefinition(id, domain string) capability.Definition {
	schema := func(slot string) capability.SchemaRef {
		return capability.SchemaRef{SchemaID: id + "." + slot + "/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"}
	}
	return capability.Definition{
		ID: id, Version: 1, OwnerDomain: domain,
		RequestSchema: schema("request"), ResponseSchema: schema("response"), ErrorSchema: schema("error"),
		EffectClass: capability.EffectReadOnly, ReadData: capability.DataDomainFieldSet{DataDomains: []string{domain}},
		RiskClass: "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1", AuthZScopeRef: "scope:people.read",
		LegalBasisRef: "legal.p1a.observation-only.v1", EntitlementRef: "entitlement.pilot.p1a.v1",
		SLOClassRef: "slo.interactive.p95-2s.v1", TestRef: "conformance:" + id + "/v1",
	}
}

func defaultResolver() workflow.CapabilityResolver {
	r := capability.NewRegistry()
	noop := func(context.Context, any) (any, error) { return nil, nil }
	_ = r.Register(capabilityDefinition(CapabilityWorker, "people"), noop)
	_ = r.Register(capabilityDefinition(CapabilityManager, "people"), noop)
	return r
}

func terminalGovernance() workflow.NodeGovernance {
	return workflow.NodeGovernance{
		Purpose:               purpose,
		Classification:        classification,
		RevalidationBoundary:  workflow.RevalidatePreClosure,
		DataAccessManifestRef: dataAccessManifest,
	}
}

// ReferenceDefinition returns the P1A Manager Change reference definition.
// It declares only simulation and has no mutating node.
func ReferenceDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        WorkflowID,
		Version:           Version,
		Name:              "Manager Change (simulation)",
		InputSchema:       schema("ManagerChangeInput"),
		OutputSchema:      schema("ManagerChangeResult"),
		VariablesSchema:   schema("ManagerChangeVariables"),
		TenantScope:       "acme",
		OrganizationScope: organizationScope,
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeSimulate},
		TerminalProfile:   workflow.TerminalProfileSimulateOnly,
		StartNodeID:       NodeReadWorker,
		Inputs: []workflow.Field{
			{Path: "worker_id", Type: branded("WorkerID")},
			{Path: "current_manager_id", Type: branded("WorkerID")},
			{Path: "proposed_manager_id", Type: branded("WorkerID")},
			{Path: "effective_date", Type: localDate()},
		},
		Outputs: []workflow.Field{
			{Path: "worker_id", Type: branded("WorkerID")},
			{Path: "terminal_code", Type: plainString()},
		},
		Limits:                workflow.Limits{MaxFanOut: 4, MaxDepth: 8, MaxNodes: 8},
		FailurePolicyRef:      "policy.workflow.failure.simulation/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.simulation/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.hr-simulation/v1",
		Nodes:                 nodes(),
		Edges:                 edges(),
	}
}

func nodes() []workflow.Node {
	return []workflow.Node{
		{
			ID: NodeReadWorker, Type: workflow.StepCapability,
			InputSchema: capabilitySchema(CapabilityWorker, "request"), OutputSchema: capabilitySchema(CapabilityWorker, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: branded("WorkerID")}, {Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "worker_id", Type: branded("WorkerID")}, {Path: "employment_active", Type: boolean()},
				{Path: "current_manager_id", Type: branded("WorkerID")},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")}, {Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{ID: CapabilityWorker, Version: 1, OperationMode: workflow.ModeSimulate, AuthorityScopes: []string{"scope:people.read"}},
			Governance: governance(workflow.RevalidatePreExecution),
		},
		{
			ID: NodeReadManager, Type: workflow.StepCapability,
			InputSchema: capabilitySchema(CapabilityManager, "request"), OutputSchema: capabilitySchema(CapabilityManager, "response"),
			Inputs: []workflow.Field{
				{Path: "proposed_manager_id", Type: branded("WorkerID")}, {Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "proposed_manager_id", Type: branded("WorkerID")}, {Path: "employment_active", Type: boolean()},
				{Path: "management_eligible", Type: boolean()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "proposed_manager_id", Source: builders.FromInput("proposed_manager_id")}, {Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{ID: CapabilityManager, Version: 1, OperationMode: workflow.ModeSimulate, AuthorityScopes: []string{"scope:people.read"}},
			Governance: governance(workflow.RevalidatePreExecution),
		},
		{
			ID: NodeValidate, Type: workflow.StepTransform,
			InputSchema: schema("ManagerChangeValidationInput"), OutputSchema: schema("ManagerChangeValidationResult"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: branded("WorkerID")}, {Path: "current_manager_id", Type: branded("WorkerID")},
				{Path: "proposed_manager_id", Type: branded("WorkerID")}, {Path: "worker_active", Type: boolean()},
				{Path: "proposed_manager_active", Type: boolean()}, {Path: "management_eligible", Type: boolean()},
			},
			Outputs: []workflow.Field{{Path: "validation_status", Type: plainString()}},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromNode(NodeReadWorker, "worker_id")},
				{Target: "current_manager_id", Source: builders.FromNode(NodeReadWorker, "current_manager_id")},
				{Target: "proposed_manager_id", Source: builders.FromNode(NodeReadManager, "proposed_manager_id")},
				{Target: "worker_active", Source: builders.FromNode(NodeReadWorker, "employment_active")},
				{Target: "proposed_manager_active", Source: builders.FromNode(NodeReadManager, "employment_active")},
				{Target: "management_eligible", Source: builders.FromNode(NodeReadManager, "management_eligible")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformValidate, Version: 1, NormalizationProfile: "hcmnext.canonical.manager_change_validation/v1",
				InputTaint:  []workflow.TaintInput{{Source: "worker_id", Level: workflow.TaintTrusted}, {Source: "proposed_manager_id", Level: workflow.TaintTrusted}},
				OutputTaint: workflow.TaintTrusted, Limits: workflow.TransformLimits{MaxInputBytes: 64 * 1024, MaxOutputBytes: 64 * 1024, MaxSteps: 5_000},
			},
			Governance: governance(workflow.RevalidateNone),
		},
		{
			ID: NodeSimulate, Type: workflow.StepTransform,
			InputSchema: schema("ManagerChangeSimulationInput"), OutputSchema: schema("ManagerChangeSimulationResult"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: branded("WorkerID")}, {Path: "current_manager_id", Type: branded("WorkerID")},
				{Path: "proposed_manager_id", Type: branded("WorkerID")}, {Path: "effective_date", Type: localDate()},
				{Path: "validation_status", Type: plainString()},
			},
			Outputs: []workflow.Field{{Path: "proposal_digest", Type: plainString()}, {Path: "write_boundary", Type: plainString()}},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromNode(NodeReadWorker, "worker_id")},
				{Target: "current_manager_id", Source: builders.FromNode(NodeReadWorker, "current_manager_id")},
				{Target: "proposed_manager_id", Source: builders.FromNode(NodeReadManager, "proposed_manager_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
				{Target: "validation_status", Source: builders.FromNode(NodeValidate, "validation_status")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformSimulate, Version: 1, NormalizationProfile: "hcmnext.canonical.manager_change_simulation/v1",
				InputTaint:  []workflow.TaintInput{{Source: "worker_id", Level: workflow.TaintTrusted}, {Source: "current_manager_id", Level: workflow.TaintTrusted}, {Source: "proposed_manager_id", Level: workflow.TaintTrusted}},
				OutputTaint: workflow.TaintTrusted, Limits: workflow.TransformLimits{MaxInputBytes: 64 * 1024, MaxOutputBytes: 64 * 1024, MaxSteps: 5_000},
			},
			Governance: governance(workflow.RevalidateNone),
		},
		{
			ID: NodeEndComplete, Type: workflow.StepEnd, Inputs: builders.TerminalInputs("worker_id", "WorkerID"), InputMappings: builders.TerminalMappings("worker_id", "SIMULATION_COMPLETE"),
			End:        &workflow.EndSpec{TerminalCode: "SIMULATION_COMPLETE", RuntimeStatus: workflow.RuntimeCompleted, CompletionMapping: builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "NOT_APPLICABLE", "SATISFIED")},
			Governance: terminalGovernance(),
		},
		{
			ID: NodeEndRejected, Type: workflow.StepEnd, Inputs: builders.TerminalInputs("worker_id", "WorkerID"), InputMappings: builders.TerminalMappings("worker_id", "SIMULATION_REJECTED"),
			End:        &workflow.EndSpec{TerminalCode: "SIMULATION_REJECTED", RuntimeStatus: workflow.RuntimeCompleted, CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE")},
			Governance: terminalGovernance(),
		},
	}
}

func edges() []workflow.Edge {
	return []workflow.Edge{
		{From: NodeReadWorker, To: NodeReadManager, RouteKey: string(workflow.OutcomeSucceeded)},
		{From: NodeReadWorker, To: NodeEndRejected, RouteKey: string(workflow.OutcomeRejected)},
		{From: NodeReadWorker, To: NodeEndRejected, RouteKey: string(workflow.OutcomeUnknown)},
		{From: NodeReadWorker, To: NodeEndRejected, RouteKey: string(workflow.OutcomeAmbiguous)},
		{From: NodeReadManager, To: NodeValidate, RouteKey: string(workflow.OutcomeSucceeded)},
		{From: NodeReadManager, To: NodeEndRejected, RouteKey: string(workflow.OutcomeRejected)},
		{From: NodeReadManager, To: NodeEndRejected, RouteKey: string(workflow.OutcomeUnknown)},
		{From: NodeReadManager, To: NodeEndRejected, RouteKey: string(workflow.OutcomeAmbiguous)},
		{From: NodeValidate, To: NodeSimulate, RouteKey: string(workflow.OutcomeSucceeded)},
		{From: NodeValidate, To: NodeEndRejected, RouteKey: string(workflow.OutcomeFailed)},
		{From: NodeSimulate, To: NodeEndComplete, RouteKey: string(workflow.OutcomeSucceeded)},
		{From: NodeSimulate, To: NodeEndRejected, RouteKey: string(workflow.OutcomeFailed)},
	}
}

// Compile compiles Manager Change in P1A. The optional argument keeps the
// fixture convenient for callers that already hold a resolver while the
// package's own setup supplies its immutable in-memory registry.
func Compile(resolvers ...workflow.CapabilityResolver) (*workflow.CompiledWorkflow, error) {
	var resolver workflow.CapabilityResolver
	if len(resolvers) > 0 {
		resolver = resolvers[0]
	} else {
		resolver = defaultResolver()
	}
	return workflow.Compile(ReferenceDefinition(), workflow.Options{Phase: workflow.PhaseP1A, Capabilities: resolver})
}

// CapabilityIDs is the exact capability surface of the fixture.
func CapabilityIDs() []capability.Key {
	return []capability.Key{{ID: CapabilityWorker, Version: 1}, {ID: CapabilityManager, Version: 1}}
}
