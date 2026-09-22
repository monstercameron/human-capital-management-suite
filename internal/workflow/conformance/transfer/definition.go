// Package transfer is CONF-003's proof that the cross-company transfer
// reference workflow (planning/workflows/workforce/org-transfer-compensation.md)
// compiles under P1A and walks to completion through the real SIMULATE-mode
// interpreter (internal/workflow/simulate), exactly as CONF-001 established for
// the promote-into-management reference (internal/workflow/promotion.go,
// internal/workflow/simulate/promotion.go).
//
// It is a conformance fixture, not a domain implementation: the capability
// handlers behind every CAPABILITY/OBSERVE node are canned, in-memory
// projections owned by this package, never a real HRIS, payroll or IAM
// integration. What is under test is the workflow's own composition -
// exit/start obligations, effective-dated authority, reservations declared as
// obligations, external observation and a terminal that reports exact,
// separately-tracked lifecycle dimensions - not any domain's business logic.
package transfer

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
)

// Workflow identity.
const (
	WorkflowID = "hcmnext.workflows.transfer_cross_company"
	Version    = 1
)

// Node ids. Exported so the conformance tests can name the exact path a golden
// walk takes without re-deriving it from the definition.
const (
	NodeReadSourceAuthority      = "read_source_authority"
	NodeReadDestinationAuthority = "read_destination_authority"
	NodeResolveJurisdiction      = "resolve_jurisdiction"
	NodeComputeConflict          = "compute_conflict_footprint"
	NodeBuildProposal            = "build_transfer_proposal"
	NodeAuthorityDecision        = "authority_and_conflict_decision"
	NodeObserveConflict          = "observe_effective_date_conflict"

	NodeEndPendingApprovals = "end_pending_approvals"
	NodeEndConflictBlocked  = "end_conflict_blocked"
	NodeEndDegradedRepair   = "end_degraded_repair"
	NodeEndUnknown          = "end_unknown"
)

// Decision route keys the authority-and-conflict DECISION declares.
const (
	RouteCrossCompanyOK   = "CROSS_COMPANY_OK"
	RouteConflictBlocked  = "CONFLICT_BLOCKED"
	RuleAuthorityConflict = "rules.transfer.authority_and_conflict/v1"
)

// Transform references the two TRANSFORM nodes bind.
const (
	TransformConflictFootprint = "transforms.transfer.compute_conflict_footprint"
	TransformBuildProposal     = "transforms.transfer.build_proposal"
)

// Capability identities. They are this package's own fixture capabilities,
// never a bootstrap or domain capability: CONF-003 proves the workflow's
// composition, not a real People/Org/Payroll integration.
const (
	CapReadSourceAuthority      = "hcmnext.conformance.transfer.read_source_authority"
	CapReadDestinationAuthority = "hcmnext.conformance.transfer.read_destination_authority"
	CapResolveJurisdiction      = "hcmnext.conformance.transfer.resolve_jurisdiction"
	CapObserveConflict          = "hcmnext.conformance.transfer.observe_conflict"
)

// Approval requirement ids: step 12 of the reference workflow names source
// manager, destination manager, HRBP, compensation and finance decisions.
const (
	ApprovalSourceManager      = "approval.transfer.source_manager"
	ApprovalDestinationManager = "approval.transfer.destination_manager"
	ApprovalHRBP               = "approval.transfer.hrbp"
	ApprovalCompensation       = "approval.transfer.compensation_partner"
	ApprovalFinance            = "approval.transfer.finance_partner"
)

// Obligation ids.
const (
	ObligationExitStartEffects = "obligation.transfer.exit_start_effects"
	ObligationAccessRecalc     = "obligation.transfer.access_recalculation"
	ObligationEvidence         = "obligation.transfer.simulation_evidence_retained"
)

const (
	organizationScope  = "acme/workforce"
	purpose            = "SIMULATE_CROSS_COMPANY_TRANSFER"
	classification     = "CONFIDENTIAL_HR"
	dataAccessManifest = "dam.transfer.simulation/v1"
	mappingDigest      = "hcmnext.workflow.InputMappingSet/v1"
	// repairRef is the bounded repair artifact the degraded terminal links.
	// A RepairRef is what makes ExecutionState=BLOCKED with an open access
	// drift a tracked repair rather than a silently dropped observation.
	repairRef = "repair.transfer.access_or_conflict_drift/v1"
)

// ---- small typed-value helpers, mirrored from internal/workflow/promotion.go
// so this package never has to reach into that package's unexported helpers.

func str(brand string) workflow.ValueType {
	return workflow.ValueType{Kind: workflow.KindString, Brand: brand}
}
func plainStr() workflow.ValueType  { return workflow.ValueType{Kind: workflow.KindString} }
func boolean() workflow.ValueType   { return workflow.ValueType{Kind: workflow.KindBool} }
func localDate() workflow.ValueType { return workflow.ValueType{Kind: workflow.KindLocalDate} }

func bootstrapCapabilitySchema(id, slot string) workflow.SchemaRef {
	return workflow.SchemaRef{
		SchemaID:         id + "." + slot + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
	}
}

func workflowSchema(name string) workflow.SchemaRef {
	return workflow.SchemaRef{
		SchemaID:         name + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.workflows.v1." + name,
	}
}

func governedInvocation(obligations, approvals []string, scope string) workflow.NodeGovernance {
	return workflow.NodeGovernance{
		Purpose:               purpose,
		Classification:        classification,
		RequiredDecisions:     []workflow.GovernanceKind{workflow.GovernanceAuthZ, workflow.GovernanceLegal, workflow.GovernancePurpose, workflow.GovernanceRisk},
		ObligationRefs:        obligations,
		ApprovalRequirements:  approvals,
		RevalidationBoundary:  workflow.RevalidatePreExecution,
		DataAccessManifestRef: dataAccessManifest,
	}
}

func terminalGovernance(obligations, approvals []string) workflow.NodeGovernance {
	return workflow.NodeGovernance{
		Purpose:               purpose,
		Classification:        classification,
		ObligationRefs:        obligations,
		ApprovalRequirements:  approvals,
		RevalidationBoundary:  workflow.RevalidatePreClosure,
		DataAccessManifestRef: dataAccessManifest,
	}
}

// ReferenceDefinition returns the P1A cross-company transfer reference
// workflow as typed Go values.
//
// It composes exactly what CONF-003's GREEN clause names: source and
// destination employment/authority reads scoped to two distinct company
// authority scopes (the REFACTOR clause - a workflow cannot resolve an
// approver or an authority fact across the boundary it is not authorized
// for), jurisdiction resolution, a conflict footprint, a bound proposal, an
// authority-and-conflict routing decision and a post-decision observation
// whose PASS/FAIL/UNKNOWN answer decides between a consistent pending-
// approvals terminal and a bounded repair route.
func ReferenceDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        WorkflowID,
		Version:           Version,
		Name:              "Cross-company transfer (simulation)",
		InputSchema:       workflowSchema("TransferCrossCompanyInput"),
		OutputSchema:      workflowSchema("TransferCrossCompanyResult"),
		VariablesSchema:   workflowSchema("TransferCrossCompanyVariables"),
		TenantScope:       "acme",
		OrganizationScope: organizationScope,
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeSimulate},
		TerminalProfile:   workflow.TerminalProfileSimulateOnly,
		StartNodeID:       NodeReadSourceAuthority,

		Inputs: []workflow.Field{
			{Path: "worker_id", Type: str("WorkerID")},
			{Path: "source_company_id", Type: str("CompanyID")},
			{Path: "destination_company_id", Type: str("CompanyID")},
			{Path: "target_position_id", Type: str("PositionID")},
			{Path: "effective_date", Type: localDate()},
		},
		Outputs: []workflow.Field{
			{Path: "worker_id", Type: str("WorkerID")},
			{Path: "terminal_code", Type: plainStr()},
		},

		Limits: workflow.Limits{MaxFanOut: 4, MaxDepth: 12, MaxNodes: 24},

		FailurePolicyRef:      "policy.workflow.failure.simulation/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.simulation/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.hr-simulation/v1",

		ApprovalRequirements: []workflow.ApprovalRequirement{
			{ID: ApprovalSourceManager, ResolverExpression: "SourceManagerOf(worker)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalDestinationManager, ResolverExpression: "DestinationManagerOf(target_position)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalHRBP, ResolverExpression: "HRBPFor(destination_company)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalCompensation, ResolverExpression: "CompensationPartnerFor(destination_company)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalFinance, ResolverExpression: "FinancePartnerFor(destination_company)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
		},

		Obligations: []workflow.ObligationRequirement{
			{
				ID: ObligationExitStartEffects, Authority: "customer.policy.workforce.transfer",
				InsertionPoint:   workflow.InsertSimulation,
				RequiredAction:   "Compose the exit obligation on the source company and the start obligation on the destination company as one bound effect graph",
				ResponsibleParty: "workforce.transfer.coordinator", SatisfactionCondition: "proposal digest cites both an exit effect and a start effect scoped to their own company",
				SourceVersion: RuleAuthorityConflict, ReevaluationPolicy: workflow.ReevalReevaluate, Mandatory: true,
			},
			{
				ID: ObligationAccessRecalc, Authority: "iam.entitlement.recalculation",
				InsertionPoint: workflow.InsertSimulation,
				RequiredAction: "Recalculate IAM entitlement for the destination company and revoke source-company-scoped entitlement", ResponsibleParty: "iam.entitlement", SatisfactionCondition: "access recalculation intent is recorded against the effective date",
				SourceVersion: "iam.entitlement.recalculation/v2", ReevaluationPolicy: workflow.ReevalRequireReview,
			},
			{
				ID: ObligationEvidence, Authority: "records.retention",
				InsertionPoint: workflow.InsertClosure,
				RequiredAction: "Retain the simulation artifact and its evidence references", ResponsibleParty: "operations.records", SatisfactionCondition: "simulation artifact digest recorded with the terminal result",
				SourceVersion: "records.retention.hr-simulation/v1", ReevaluationPolicy: workflow.ReevalPin, Mandatory: true,
			},
		},

		Nodes: nodes(),
		Edges: edges(),
	}
}

func nodes() []workflow.Node {
	return []workflow.Node{
		{
			ID:           NodeReadSourceAuthority,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapReadSourceAuthority, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapReadSourceAuthority, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "source_company_id", Type: str("CompanyID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "source_employment_active", Type: boolean()},
				{Path: "source_authority_scope", Type: plainStr()},
				{Path: "source_position_id", Type: str("PositionID")},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "source_company_id", Source: builders.FromInput("source_company_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapReadSourceAuthority, Version: 1, OperationMode: workflow.ModeSimulate,
				// The source company's own scope: a node reading the
				// destination side never carries this scope, which is what
				// keeps the two companies separate authority domains rather
				// than one workflow-wide grant.
				AuthorityScopes: []string{"scope:company.source.read"},
			},
			Governance: governedInvocation(nil, nil, organizationScope),
		},
		{
			ID:           NodeReadDestinationAuthority,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapReadDestinationAuthority, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapReadDestinationAuthority, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "destination_company_id", Type: str("CompanyID")},
				{Path: "target_position_id", Type: str("PositionID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "destination_position_available", Type: boolean()},
				{Path: "destination_authority_scope", Type: plainStr()},
				{Path: "destination_budget_available", Type: boolean()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "destination_company_id", Source: builders.FromInput("destination_company_id")},
				{Target: "target_position_id", Source: builders.FromInput("target_position_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapReadDestinationAuthority, Version: 1, OperationMode: workflow.ModeSimulate,
				// The destination company's own, distinct scope.
				AuthorityScopes: []string{"scope:company.destination.read"},
			},
			Governance: governedInvocation(nil, nil, organizationScope),
		},
		{
			ID:           NodeResolveJurisdiction,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapResolveJurisdiction, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapResolveJurisdiction, "response"),
			Inputs: []workflow.Field{
				{Path: "destination_company_id", Type: str("CompanyID")},
				{Path: "target_position_id", Type: str("PositionID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "destination_jurisdiction", Type: plainStr()},
				{Path: "payroll_group", Type: plainStr()},
				{Path: "iam_policy_ref", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "destination_company_id", Source: builders.FromInput("destination_company_id")},
				{Target: "target_position_id", Source: builders.FromInput("target_position_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			RequiredContext: []workflow.ContextRequirement{{
				Kind:                  "LegalContext",
				FieldPaths:            []string{"jurisdiction", "applicable_rule_versions"},
				Purpose:               purpose,
				MaximumClassification: classification,
				MaxAgeSeconds:         3600,
				RequiredWatermarks:    []string{"legal.policy.version"},
				Pinned:                true,
				MissingBehavior:       workflow.MissingUnknown,
			}},
			Capability: &workflow.CapabilityRef{
				ID: CapResolveJurisdiction, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:legal.read"},
			},
			Governance: governedInvocation(nil, nil, organizationScope),
		},
		{
			ID:           NodeComputeConflict,
			Type:         workflow.StepTransform,
			InputSchema:  workflowSchema("TransferConflictFootprintDraft"),
			OutputSchema: workflowSchema("TransferConflictFootprint"),
			Inputs: []workflow.Field{
				{Path: "source_authority_scope", Type: plainStr()},
				{Path: "destination_authority_scope", Type: plainStr()},
				{Path: "source_employment_active", Type: boolean()},
				{Path: "destination_position_available", Type: boolean()},
				{Path: "destination_budget_available", Type: boolean()},
			},
			Outputs: []workflow.Field{
				{Path: "conflict_detected", Type: boolean()},
				{Path: "footprint_digest", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "source_authority_scope", Source: builders.FromNode(NodeReadSourceAuthority, "source_authority_scope")},
				{Target: "destination_authority_scope", Source: builders.FromNode(NodeReadDestinationAuthority, "destination_authority_scope")},
				{Target: "source_employment_active", Source: builders.FromNode(NodeReadSourceAuthority, "source_employment_active")},
				{Target: "destination_position_available", Source: builders.FromNode(NodeReadDestinationAuthority, "destination_position_available")},
				{Target: "destination_budget_available", Source: builders.FromNode(NodeReadDestinationAuthority, "destination_budget_available")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformConflictFootprint, Version: 1,
				NormalizationProfile: "hcmnext.canonical.transfer_footprint/v1",
				InputTaint: []workflow.TaintInput{
					{Source: "source_authority_scope", Level: workflow.TaintTrusted},
					{Source: "destination_authority_scope", Level: workflow.TaintTrusted},
				},
				OutputTaint: workflow.TaintTrusted,
				Limits:      workflow.TransformLimits{MaxInputBytes: 64 * 1024, MaxOutputBytes: 64 * 1024, MaxSteps: 5_000},
			},
			Governance: workflow.NodeGovernance{
				Purpose: purpose, Classification: classification,
				RevalidationBoundary: workflow.RevalidateNone, DataAccessManifestRef: dataAccessManifest,
			},
		},
		{
			ID:           NodeBuildProposal,
			Type:         workflow.StepTransform,
			InputSchema:  workflowSchema("TransferProposalDraft"),
			OutputSchema: workflowSchema("TransferProposal"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "source_company_id", Type: str("CompanyID")},
				{Path: "destination_company_id", Type: str("CompanyID")},
				{Path: "destination_jurisdiction", Type: plainStr()},
				{Path: "footprint_digest", Type: plainStr()},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "proposal_digest", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "source_company_id", Source: builders.FromInput("source_company_id")},
				{Target: "destination_company_id", Source: builders.FromInput("destination_company_id")},
				{Target: "destination_jurisdiction", Source: builders.FromNode(NodeResolveJurisdiction, "destination_jurisdiction")},
				{Target: "footprint_digest", Source: builders.FromNode(NodeComputeConflict, "footprint_digest")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformBuildProposal, Version: 1,
				NormalizationProfile: "hcmnext.canonical.transfer_proposal/v1",
				InputTaint: []workflow.TaintInput{
					{Source: "worker_id", Level: workflow.TaintTrusted},
					{Source: "footprint_digest", Level: workflow.TaintTrusted},
				},
				OutputTaint: workflow.TaintTrusted,
				Limits:      workflow.TransformLimits{MaxInputBytes: 64 * 1024, MaxOutputBytes: 64 * 1024, MaxSteps: 5_000},
			},
			Governance: workflow.NodeGovernance{
				Purpose: purpose, Classification: classification,
				RevalidationBoundary: workflow.RevalidateNone, DataAccessManifestRef: dataAccessManifest,
			},
		},
		{
			ID:           NodeAuthorityDecision,
			Type:         workflow.StepDecision,
			InputSchema:  workflowSchema("TransferAuthorityDecisionInput"),
			OutputSchema: workflowSchema("TransferAuthorityDecisionResult"),
			Inputs: []workflow.Field{
				{Path: "conflict_detected", Type: boolean()},
				{Path: "source_authority_scope", Type: plainStr()},
				{Path: "destination_authority_scope", Type: plainStr()},
			},
			Outputs: []workflow.Field{{Path: "route_key", Type: plainStr()}},
			InputMappings: []workflow.Mapping{
				{Target: "conflict_detected", Source: builders.FromNode(NodeComputeConflict, "conflict_detected")},
				{Target: "source_authority_scope", Source: builders.FromNode(NodeReadSourceAuthority, "source_authority_scope")},
				{Target: "destination_authority_scope", Source: builders.FromNode(NodeReadDestinationAuthority, "destination_authority_scope")},
			},
			Decision: &workflow.DecisionSpec{
				EvaluatorRef: "engines.rules.transfer_authority", EvaluatorVersion: 1,
				RuleRef: RuleAuthorityConflict, InputDigestProfile: mappingDigest,
				Routes: []workflow.DecisionRoute{
					{Key: RouteCrossCompanyOK, Predicate: "authority_scopes_remain_separate_and_no_conflict", Precedence: 10},
					{Key: RouteConflictBlocked, Predicate: "authority_scopes_collapse_or_conflict_detected", Precedence: 20},
				},
				DefaultRoute: RouteCrossCompanyOK,
			},
			Governance: workflow.NodeGovernance{
				Purpose: purpose, Classification: classification,
				RevalidationBoundary: workflow.RevalidateNone, DataAccessManifestRef: dataAccessManifest,
			},
		},
		{
			ID:           NodeObserveConflict,
			Type:         workflow.StepObserve,
			InputSchema:  bootstrapCapabilitySchema(CapObserveConflict, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapObserveConflict, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "target_position_id", Type: str("PositionID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "conflict_state", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "target_position_id", Source: builders.FromInput("target_position_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapObserveConflict, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:operations.read"},
			},
			Observe: &workflow.ObserveSpec{
				EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "workforce.assignment.projection",
				ExpectedStateFields: []string{"target_position_id"}, RequiredWatermarks: []string{"workforce.assignment.stream_head"},
				MaxAgeSeconds: 300, ComparisonProfile: "comparison.workforce.effective_date_conflict/v1",
				RetryExhaustionRoute: NodeEndDegradedRepair,
			},
			Retry:      &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.observation.bounded/v1"},
			Governance: governedInvocation(nil, nil, organizationScope),
		},
		{
			ID:            NodeEndPendingApprovals,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TRANSFER_SIMULATION_PENDING_APPROVALS"),
			End: &workflow.EndSpec{
				TerminalCode: "TRANSFER_SIMULATION_PENDING_APPROVALS", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping:         builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "PENDING_OBSERVATION", "PENDING"),
				OutstandingObligationRefs: []string{ObligationExitStartEffects, ObligationAccessRecalc, ObligationEvidence},
			},
			Governance: terminalGovernance(
				[]string{ObligationExitStartEffects, ObligationAccessRecalc, ObligationEvidence},
				[]string{ApprovalSourceManager, ApprovalDestinationManager, ApprovalHRBP, ApprovalCompensation, ApprovalFinance},
			),
		},
		{
			ID:            NodeEndConflictBlocked,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TRANSFER_CONFLICT_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "TRANSFER_CONFLICT_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationEvidence}, nil),
		},
		{
			ID:            NodeEndDegradedRepair,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TRANSFER_SIMULATION_DEGRADED"),
			End: &workflow.EndSpec{
				TerminalCode: "TRANSFER_SIMULATION_DEGRADED", RuntimeStatus: workflow.RuntimeBlocked,
				CompletionMapping:         builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{ObligationAccessRecalc, ObligationEvidence},
				RepairRefs:                []string{repairRef},
			},
			Governance: terminalGovernance([]string{ObligationAccessRecalc, ObligationEvidence}, nil),
		},
		{
			ID:            NodeEndUnknown,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TRANSFER_SIMULATION_UNKNOWN"),
			End: &workflow.EndSpec{
				TerminalCode: "TRANSFER_SIMULATION_UNKNOWN", RuntimeStatus: workflow.RuntimeBlocked,
				CompletionMapping:         builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{ObligationEvidence},
			},
			Governance: terminalGovernance([]string{ObligationEvidence}, nil),
		},
	}
}

func edges() []workflow.Edge {
	capabilityRoutes := func(from, success string) []workflow.Edge {
		return []workflow.Edge{
			{From: from, To: success, RouteKey: string(workflow.OutcomeSucceeded)},
			{From: from, To: NodeEndConflictBlocked, RouteKey: string(workflow.OutcomeRejected)},
			{From: from, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},
			{From: from, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeAmbiguous)},
		}
	}
	out := capabilityRoutes(NodeReadSourceAuthority, NodeReadDestinationAuthority)
	out = append(out, capabilityRoutes(NodeReadDestinationAuthority, NodeResolveJurisdiction)...)
	out = append(out, capabilityRoutes(NodeResolveJurisdiction, NodeComputeConflict)...)
	out = append(out,
		workflow.Edge{From: NodeComputeConflict, To: NodeBuildProposal, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeComputeConflict, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)},

		workflow.Edge{From: NodeBuildProposal, To: NodeAuthorityDecision, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeBuildProposal, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)},

		workflow.Edge{From: NodeAuthorityDecision, To: NodeObserveConflict, RouteKey: RouteCrossCompanyOK},
		workflow.Edge{From: NodeAuthorityDecision, To: NodeEndConflictBlocked, RouteKey: RouteConflictBlocked},
		workflow.Edge{From: NodeAuthorityDecision, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},

		workflow.Edge{From: NodeObserveConflict, To: NodeEndPendingApprovals, RouteKey: string(workflow.OutcomePass)},
		workflow.Edge{From: NodeObserveConflict, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomeFail)},
		workflow.Edge{From: NodeObserveConflict, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomePartial)},
		workflow.Edge{From: NodeObserveConflict, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},
	)
	return out
}

// Compile compiles the reference workflow against a capability registry.
func Compile(registry workflow.CapabilityResolver) (*workflow.CompiledWorkflow, error) {
	return workflow.Compile(ReferenceDefinition(), workflow.Options{
		Phase:        workflow.PhaseP1A,
		Capabilities: registry,
	})
}

// CapabilityIDs is the exact set of capability versions the reference
// workflow binds, so a caller can prove its registry publishes all of them
// before compiling.
func CapabilityIDs() []capability.Key {
	return []capability.Key{
		{ID: CapReadSourceAuthority, Version: 1},
		{ID: CapReadDestinationAuthority, Version: 1},
		{ID: CapResolveJurisdiction, Version: 1},
		{ID: CapObserveConflict, Version: 1},
	}
}
