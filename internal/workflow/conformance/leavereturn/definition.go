// Package leavereturn is CONF-004's proof that the leave-and-return
// reference workflow (planning/workflows/leave/leave-return-to-work.md)
// compiles under P1A and walks to completion through the real SIMULATE-mode
// interpreter (internal/workflow/simulate), exactly as CONF-005 established
// for termination.
//
// It is a conformance fixture, not a domain implementation: the capability
// handlers behind every CAPABILITY/OBSERVE node are canned, in-memory
// projections this package owns, never a real HRIS, benefits or payroll
// integration. What is under test is the workflow's own composition:
// intent-only ingress, independent program eligibility, a compartmented
// evidence review that refuses manager-determined eligibility and unsealed
// medical evidence, an immutable proposal, a benefits observation tracked
// independently of every other obligation, a readiness-gated return that
// refuses not-ready states, and multidimensional completion with every
// obligation outstanding rather than collapsed into a false success.
package leavereturn

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Workflow identity.
const (
	WorkflowID = "hcmnext.workflows.leave_and_return"
	Version    = 1
)

// Node ids.
const (
	NodeReadEmploymentAuthority = "read_employment_and_authority"
	NodeResolveLeavePrograms    = "resolve_leave_programs"
	NodeEvidenceReviewDecision  = "evidence_review_decision"
	NodeBuildLeaveProposal      = "build_leave_proposal"
	NodeObserveBenefits         = "observe_benefits_continuation"
	NodeResolveReadiness        = "resolve_return_readiness"
	NodeReadinessReturnDecision = "readiness_return_decision"

	NodeEndPendingObligations    = "end_pending_obligations"
	NodeEndDegradedRepair        = "end_degraded_repair"
	NodeEndReturnBlockedNotReady = "end_return_blocked_not_ready"
	NodeEndCompartmentBreach     = "end_compartment_breach_blocked"
	NodeEndManagerEligibility    = "end_manager_eligibility_blocked"
	NodeEndRejectedInvalid       = "end_rejected_invalid_request"
	NodeEndUnknown               = "end_unknown"
)

// Decision route keys.
const (
	RouteRoutineReviewRequired     = "ROUTINE_REVIEW_REQUIRED"
	RouteCompartmentBreachBlocked  = "COMPARTMENT_BREACH_BLOCKED"
	RouteManagerEligibilityBlocked = "MANAGER_ELIGIBILITY_BLOCKED"
	RouteProceedToReturn           = "PROCEED_TO_RETURN"
	RouteReturnBlockedNotReady     = "RETURN_BLOCKED_NOT_READY"
	RouteEmploymentInactiveInvalid = "EMPLOYMENT_INACTIVE_INVALID"
	RuleEvidenceReview             = "rules.leave.evidence_review/v1"
	RuleReadinessReturn            = "rules.leave.readiness_return/v1"
)

// Transform references.
const (
	TransformBuildProposal = "transforms.leave.build_proposal"
)

// Capability identities. This package's own fixture capabilities, never a
// bootstrap or domain capability.
const (
	CapReadEmploymentAuthority = "hcmnext.conformance.leavereturn.read_employment_and_authority"
	CapResolveLeavePrograms    = "hcmnext.conformance.leavereturn.resolve_leave_programs"
	CapObserveBenefits         = "hcmnext.conformance.leavereturn.observe_benefits_continuation"
	CapResolveReadiness        = "hcmnext.conformance.leavereturn.resolve_return_readiness"
)

// Approval requirement ids: the leave administrator owns the restricted
// review; the manager sees availability only, never eligibility.
const (
	ApprovalLeaveAdministrator = "approval.leave.leave_administrator"
	ApprovalManager            = "approval.leave.manager_availability_only"
)

// Obligation ids. BenefitsRecovery is independently tracked: GREEN
// requires degraded benefits to recover without rolling back valid leave,
// which this obligation id being distinct from every other one is what
// makes checkable.
const (
	ObligationPayrollEffect    = "obligation.leave.payroll_effect"
	ObligationBenefitsRecovery = "obligation.leave.benefits_recovery"
	ObligationNoticeEvidence   = "obligation.leave.notice_and_evidence"
	ObligationRecordsRetention = "obligation.leave.records_retention_hold"
)

const (
	organizationScope  = "acme/workforce"
	purpose            = "SIMULATE_LEAVE_AND_RETURN"
	classification     = "CONFIDENTIAL_HR_SENSITIVE"
	dataAccessManifest = "dam.leave.simulation/v1"
	mappingDigest      = "hcmnext.workflow.InputMappingSet/v1"
	// repairRef is the bounded repair artifact the degraded terminal links,
	// which is how stalled benefits recovery creates tracked repair
	// evidence rather than a silently dropped observation.
	repairRef = "repair.leave.benefits_recovery_drift/v1"
)

// ---- small typed-value helpers, mirrored from the termination fixture so
// this package never reaches into another package's unexported helpers.

func str(brand string) workflow.ValueType {
	return workflow.ValueType{Kind: workflow.KindString, Brand: brand}
}
func plainStr() workflow.ValueType  { return workflow.ValueType{Kind: workflow.KindString} }
func boolean() workflow.ValueType   { return workflow.ValueType{Kind: workflow.KindBool} }
func localDate() workflow.ValueType { return workflow.ValueType{Kind: workflow.KindLocalDate} }

func fromInput(path string) workflow.Source {
	return workflow.Source{Kind: workflow.SourceWorkflowInput, Path: path}
}
func fromNode(nodeID, path string) workflow.Source {
	return workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: nodeID, Path: path}
}
func constant(value string, t workflow.ValueType) workflow.Source {
	return workflow.Source{Kind: workflow.SourceConstant, Constant: value, Type: t}
}

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

func governedInvocation(obligations, approvals []string) workflow.NodeGovernance {
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

func terminalInputs(extra ...workflow.Field) []workflow.Field {
	base := []workflow.Field{
		{Path: "worker_id", Type: str("WorkerID")},
		{Path: "terminal_code", Type: plainStr()},
	}
	return append(base, extra...)
}

func terminalMappings(code string, extra ...workflow.Mapping) []workflow.Mapping {
	base := []workflow.Mapping{
		{Target: "worker_id", Source: fromInput("worker_id")},
		{Target: "terminal_code", Source: constant(code, plainStr())},
	}
	return append(base, extra...)
}

func completion(request, execution, business, consistency, obligation string) map[string]string {
	return map[string]string{
		"RequestState":     request,
		"ExecutionState":   execution,
		"BusinessState":    business,
		"ConsistencyState": consistency,
		"ObligationState":  obligation,
	}
}

// ReferenceDefinition returns the P1A leave-and-return reference workflow
// as typed Go values: employment and authority are read once, leave
// programs resolve independently of approval, the restricted review
// refuses unsealed medical evidence and manager-determined eligibility, a
// proposal is bound, a benefits observation is tracked independently and
// routes to a bounded repair terminal when it degrades, and a
// readiness-gated return refuses not-ready states and inactive employment.
func ReferenceDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        WorkflowID,
		Version:           Version,
		Name:              "Leave of absence and return to work (simulation)",
		InputSchema:       workflowSchema("LeaveAndReturnInput"),
		OutputSchema:      workflowSchema("LeaveAndReturnResult"),
		VariablesSchema:   workflowSchema("LeaveAndReturnVariables"),
		TenantScope:       "acme",
		OrganizationScope: organizationScope,
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeSimulate},
		TerminalProfile:   workflow.TerminalProfileSimulateOnly,
		StartNodeID:       NodeReadEmploymentAuthority,

		Inputs: []workflow.Field{
			{Path: "worker_id", Type: str("WorkerID")},
			{Path: "employment_id", Type: str("EmploymentID")},
			{Path: "requester_id", Type: str("WorkerID")},
			{Path: "leave_type", Type: plainStr()},
			{Path: "effective_date", Type: localDate()},
			{Path: "return_requested", Type: boolean()},
			{Path: "reviewer_role", Type: plainStr()},
			{Path: "medical_sealed", Type: boolean()},
			{Path: "manager_determined_eligibility", Type: boolean()},
		},
		Outputs: []workflow.Field{
			{Path: "worker_id", Type: str("WorkerID")},
			{Path: "terminal_code", Type: plainStr()},
		},

		Limits: workflow.Limits{MaxFanOut: 6, MaxDepth: 14, MaxNodes: 24},

		FailurePolicyRef:      "policy.workflow.failure.simulation/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.simulation/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.hr-simulation/v1",

		ApprovalRequirements: []workflow.ApprovalRequirement{
			{ID: ApprovalLeaveAdministrator, ResolverExpression: "LeaveAdministratorFor(case)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalManager, ResolverExpression: "CurrentManagerOf(worker)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
		},

		Obligations: []workflow.ObligationRequirement{
			{
				ID: ObligationPayrollEffect, Authority: "customer.policy.payroll.leave_effects",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Project payroll effects for the leave interval",
				ResponsibleParty: "payroll.effects", SatisfactionCondition: "payroll effect projection recorded against the proposal digest",
				SourceVersion: "payroll.effects.policy/v1", ReevaluationPolicy: workflow.ReevalReevaluate, Mandatory: true,
			},
			{
				ID: ObligationBenefitsRecovery, Authority: "benefits.continuation",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Observe benefits continuation and recover degradation",
				ResponsibleParty: "benefits.administration", SatisfactionCondition: "benefits disposition observed and reconciled independently of payroll, schedule and access",
				SourceVersion: "benefits.continuation.policy/v1", ReevaluationPolicy: workflow.ReevalRequireReview, Mandatory: true,
			},
			{
				ID: ObligationNoticeEvidence, Authority: "customer.policy.employee_relations",
				InsertionPoint: workflow.InsertApproval, RequiredAction: "Generate the required leave notice and retain evidence of delivery",
				ResponsibleParty: "employee_relations.notices", SatisfactionCondition: "notice artifact digest recorded against the proposal",
				SourceVersion: "policy.notice.leave/v1", ReevaluationPolicy: workflow.ReevalRequireReview,
			},
			{
				ID: ObligationRecordsRetention, Authority: "records.retention",
				InsertionPoint: workflow.InsertClosure, RequiredAction: "Preserve leave records and apply any hold or retention schedule",
				ResponsibleParty: "operations.records", SatisfactionCondition: "simulation artifact digest recorded with the terminal result",
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
			ID:           NodeReadEmploymentAuthority,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapReadEmploymentAuthority, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapReadEmploymentAuthority, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "employment_id", Type: str("EmploymentID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "employment_active", Type: boolean()},
				{Path: "source_authority_scope", Type: plainStr()},
				{Path: "current_manager_id", Type: str("WorkerID")},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: fromInput("worker_id")},
				{Target: "employment_id", Source: fromInput("employment_id")},
				{Target: "effective_date", Source: fromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapReadEmploymentAuthority, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:people.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeResolveLeavePrograms,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapResolveLeavePrograms, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapResolveLeavePrograms, "response"),
			Inputs: []workflow.Field{
				{Path: "employment_id", Type: str("EmploymentID")},
				{Path: "leave_type", Type: plainStr()},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "program_ids", Type: plainStr()},
				{Path: "all_eligible", Type: boolean()},
				{Path: "jurisdiction", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "employment_id", Source: fromInput("employment_id")},
				{Target: "leave_type", Source: fromInput("leave_type")},
				{Target: "effective_date", Source: fromInput("effective_date")},
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
				ID: CapResolveLeavePrograms, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:leave.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeEvidenceReviewDecision,
			Type:         workflow.StepDecision,
			InputSchema:  workflowSchema("LeaveEvidenceReviewInput"),
			OutputSchema: workflowSchema("LeaveEvidenceReviewResult"),
			Inputs: []workflow.Field{
				{Path: "reviewer_role", Type: plainStr()},
				{Path: "medical_sealed", Type: boolean()},
				{Path: "manager_determined_eligibility", Type: boolean()},
			},
			Outputs: []workflow.Field{{Path: "route_key", Type: plainStr()}},
			InputMappings: []workflow.Mapping{
				{Target: "reviewer_role", Source: fromInput("reviewer_role")},
				{Target: "medical_sealed", Source: fromInput("medical_sealed")},
				{Target: "manager_determined_eligibility", Source: fromInput("manager_determined_eligibility")},
			},
			Decision: &workflow.DecisionSpec{
				EvaluatorRef: "engines.rules.leave_evidence_review", EvaluatorVersion: 1,
				RuleRef: RuleEvidenceReview, InputDigestProfile: mappingDigest,
				Routes: []workflow.DecisionRoute{
					{Key: RouteCompartmentBreachBlocked, Predicate: "medical_evidence_unsealed", Precedence: 10},
					{Key: RouteManagerEligibilityBlocked, Predicate: "manager_determined_eligibility", Precedence: 20},
					{Key: RouteRoutineReviewRequired, Predicate: "sealed_restricted_review", Precedence: 30},
				},
				DefaultRoute: RouteRoutineReviewRequired,
			},
			Governance: workflow.NodeGovernance{
				Purpose: purpose, Classification: classification,
				RevalidationBoundary: workflow.RevalidateNone, DataAccessManifestRef: dataAccessManifest,
			},
		},
		{
			ID:           NodeBuildLeaveProposal,
			Type:         workflow.StepTransform,
			InputSchema:  workflowSchema("LeaveProposalDraft"),
			OutputSchema: workflowSchema("LeaveProposal"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "employment_id", Type: str("EmploymentID")},
				{Path: "program_ids", Type: plainStr()},
				{Path: "jurisdiction", Type: plainStr()},
			},
			Outputs: []workflow.Field{
				{Path: "proposal_digest", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: fromInput("worker_id")},
				{Target: "employment_id", Source: fromInput("employment_id")},
				{Target: "program_ids", Source: fromNode(NodeResolveLeavePrograms, "program_ids")},
				{Target: "jurisdiction", Source: fromNode(NodeResolveLeavePrograms, "jurisdiction")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformBuildProposal, Version: 1,
				NormalizationProfile: "hcmnext.canonical.leave_proposal/v1",
				InputTaint: []workflow.TaintInput{
					{Source: "worker_id", Level: workflow.TaintTrusted},
					{Source: "program_ids", Level: workflow.TaintTrusted},
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
			ID:           NodeObserveBenefits,
			Type:         workflow.StepObserve,
			InputSchema:  bootstrapCapabilitySchema(CapObserveBenefits, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapObserveBenefits, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "employment_id", Type: str("EmploymentID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "benefits_state", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: fromInput("worker_id")},
				{Target: "employment_id", Source: fromInput("employment_id")},
				{Target: "effective_date", Source: fromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapObserveBenefits, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:benefits.read"},
			},
			Observe: &workflow.ObserveSpec{
				EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "benefits.continuation.projection",
				ExpectedStateFields: []string{"employment_id"}, RequiredWatermarks: []string{"benefits.continuation.stream_head"},
				MaxAgeSeconds: 300, ComparisonProfile: "comparison.benefits.continuation/v1",
				RetryExhaustionRoute: NodeEndDegradedRepair,
			},
			Retry:      &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.observation.bounded/v1"},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeResolveReadiness,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapResolveReadiness, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapResolveReadiness, "response"),
			Inputs: []workflow.Field{
				{Path: "employment_id", Type: str("EmploymentID")},
				{Path: "proposal_digest", Type: plainStr()},
			},
			Outputs: []workflow.Field{
				{Path: "readiness_state", Type: plainStr()},
				{Path: "employment_active", Type: boolean()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "employment_id", Source: fromInput("employment_id")},
				{Target: "proposal_digest", Source: fromNode(NodeBuildLeaveProposal, "proposal_digest")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapResolveReadiness, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:leave.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeReadinessReturnDecision,
			Type:         workflow.StepDecision,
			InputSchema:  workflowSchema("LeaveReadinessReturnInput"),
			OutputSchema: workflowSchema("LeaveReadinessReturnResult"),
			Inputs: []workflow.Field{
				{Path: "readiness_state", Type: plainStr()},
				{Path: "employment_active", Type: boolean()},
				{Path: "return_requested", Type: boolean()},
			},
			Outputs: []workflow.Field{{Path: "route_key", Type: plainStr()}},
			InputMappings: []workflow.Mapping{
				{Target: "readiness_state", Source: fromNode(NodeResolveReadiness, "readiness_state")},
				{Target: "employment_active", Source: fromNode(NodeResolveReadiness, "employment_active")},
				{Target: "return_requested", Source: fromInput("return_requested")},
			},
			Decision: &workflow.DecisionSpec{
				EvaluatorRef: "engines.rules.leave_readiness_return", EvaluatorVersion: 1,
				RuleRef: RuleReadinessReturn, InputDigestProfile: mappingDigest,
				Routes: []workflow.DecisionRoute{
					{Key: RouteEmploymentInactiveInvalid, Predicate: "employment_inactive", Precedence: 10},
					{Key: RouteReturnBlockedNotReady, Predicate: "readiness_not_ready", Precedence: 20},
					{Key: RouteProceedToReturn, Predicate: "ready_and_return_requested", Precedence: 30},
				},
				DefaultRoute: RouteProceedToReturn,
			},
			Governance: workflow.NodeGovernance{
				Purpose: purpose, Classification: classification,
				RevalidationBoundary: workflow.RevalidateNone, DataAccessManifestRef: dataAccessManifest,
			},
		},
		{
			ID:            NodeEndPendingObligations,
			Type:          workflow.StepEnd,
			Inputs:        terminalInputs(),
			InputMappings: terminalMappings("LEAVE_SIMULATION_PENDING_OBLIGATIONS"),
			End: &workflow.EndSpec{
				TerminalCode: "LEAVE_SIMULATION_PENDING_OBLIGATIONS", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "PENDING_OBSERVATION", "PENDING"),
				OutstandingObligationRefs: []string{
					ObligationPayrollEffect, ObligationBenefitsRecovery, ObligationNoticeEvidence, ObligationRecordsRetention,
				},
			},
			Governance: terminalGovernance(
				[]string{ObligationPayrollEffect, ObligationBenefitsRecovery, ObligationNoticeEvidence, ObligationRecordsRetention},
				[]string{ApprovalLeaveAdministrator, ApprovalManager},
			),
		},
		{
			ID:            NodeEndDegradedRepair,
			Type:          workflow.StepEnd,
			Inputs:        terminalInputs(),
			InputMappings: terminalMappings("LEAVE_SIMULATION_DEGRADED"),
			End: &workflow.EndSpec{
				TerminalCode: "LEAVE_SIMULATION_DEGRADED", RuntimeStatus: workflow.RuntimeBlocked,
				CompletionMapping:         completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{ObligationBenefitsRecovery, ObligationRecordsRetention},
				RepairRefs:                []string{repairRef},
			},
			Governance: terminalGovernance([]string{ObligationBenefitsRecovery, ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndReturnBlockedNotReady,
			Type:          workflow.StepEnd,
			Inputs:        terminalInputs(),
			InputMappings: terminalMappings("LEAVE_RETURN_BLOCKED_NOT_READY"),
			End: &workflow.EndSpec{
				TerminalCode: "LEAVE_RETURN_BLOCKED_NOT_READY", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndCompartmentBreach,
			Type:          workflow.StepEnd,
			Inputs:        terminalInputs(),
			InputMappings: terminalMappings("LEAVE_COMPARTMENT_BREACH_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "LEAVE_COMPARTMENT_BREACH_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndManagerEligibility,
			Type:          workflow.StepEnd,
			Inputs:        terminalInputs(),
			InputMappings: terminalMappings("LEAVE_MANAGER_ELIGIBILITY_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "LEAVE_MANAGER_ELIGIBILITY_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndRejectedInvalid,
			Type:          workflow.StepEnd,
			Inputs:        terminalInputs(),
			InputMappings: terminalMappings("LEAVE_REJECTED_INVALID_REQUEST"),
			End: &workflow.EndSpec{
				TerminalCode: "LEAVE_REJECTED_INVALID_REQUEST", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndUnknown,
			Type:          workflow.StepEnd,
			Inputs:        terminalInputs(),
			InputMappings: terminalMappings("LEAVE_SIMULATION_UNKNOWN"),
			End: &workflow.EndSpec{
				TerminalCode: "LEAVE_SIMULATION_UNKNOWN", RuntimeStatus: workflow.RuntimeBlocked,
				CompletionMapping:         completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{ObligationRecordsRetention},
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
	}
}

func edges() []workflow.Edge {
	capabilityRoutes := func(from, success string) []workflow.Edge {
		return []workflow.Edge{
			{From: from, To: success, RouteKey: string(workflow.OutcomeSucceeded)},
			{From: from, To: NodeEndRejectedInvalid, RouteKey: string(workflow.OutcomeRejected)},
			{From: from, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},
			{From: from, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeAmbiguous)},
		}
	}
	out := capabilityRoutes(NodeReadEmploymentAuthority, NodeResolveLeavePrograms)
	out = append(out, capabilityRoutes(NodeResolveLeavePrograms, NodeEvidenceReviewDecision)...)
	out = append(out, capabilityRoutes(NodeResolveReadiness, NodeReadinessReturnDecision)...)
	out = append(out,
		workflow.Edge{From: NodeEvidenceReviewDecision, To: NodeBuildLeaveProposal, RouteKey: RouteRoutineReviewRequired},
		workflow.Edge{From: NodeEvidenceReviewDecision, To: NodeEndCompartmentBreach, RouteKey: RouteCompartmentBreachBlocked},
		workflow.Edge{From: NodeEvidenceReviewDecision, To: NodeEndManagerEligibility, RouteKey: RouteManagerEligibilityBlocked},
		workflow.Edge{From: NodeEvidenceReviewDecision, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},

		workflow.Edge{From: NodeBuildLeaveProposal, To: NodeObserveBenefits, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeBuildLeaveProposal, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)},

		workflow.Edge{From: NodeObserveBenefits, To: NodeResolveReadiness, RouteKey: string(workflow.OutcomePass)},
		workflow.Edge{From: NodeObserveBenefits, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomeFail)},
		workflow.Edge{From: NodeObserveBenefits, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomePartial)},
		workflow.Edge{From: NodeObserveBenefits, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},

		workflow.Edge{From: NodeReadinessReturnDecision, To: NodeEndPendingObligations, RouteKey: RouteProceedToReturn},
		workflow.Edge{From: NodeReadinessReturnDecision, To: NodeEndReturnBlockedNotReady, RouteKey: RouteReturnBlockedNotReady},
		workflow.Edge{From: NodeReadinessReturnDecision, To: NodeEndRejectedInvalid, RouteKey: RouteEmploymentInactiveInvalid},
		workflow.Edge{From: NodeReadinessReturnDecision, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},
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
		{ID: CapReadEmploymentAuthority, Version: 1},
		{ID: CapResolveLeavePrograms, Version: 1},
		{ID: CapObserveBenefits, Version: 1},
		{ID: CapResolveReadiness, Version: 1},
	}
}
