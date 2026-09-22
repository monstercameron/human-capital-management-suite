// Package termination is CONF-005's proof that the termination-and-offboarding
// reference workflow (planning/workflows/lifecycle/termination-offboarding.md)
// compiles under P1A and walks to completion through the real SIMULATE-mode
// interpreter (internal/workflow/simulate), exactly as CONF-001 established
// for the promote-into-management reference and as CONF-003 established for
// the cross-company transfer reference
// (internal/workflow/conformance/transfer).
//
// It is a conformance fixture, not a domain implementation: the capability
// handlers behind every CAPABILITY/OBSERVE node are canned, in-memory
// projections this package owns, never a real HRIS, IAM or payroll
// integration. What is under test is the workflow's own composition:
// separation-of-duties (requester != approver), a legal-hold block, a P0
// access-revocation observation that is tracked independently of every other
// obligation, and a cancellation-after-employment-end path that becomes a
// distinct reinstatement/correction intent rather than mutating the original
// terminated request.
package termination

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
)

// Workflow identity.
const (
	WorkflowID = "hcmnext.workflows.termination_offboarding"
	Version    = 1
)

// Node ids.
const (
	NodeReadEmploymentAuthority = "read_employment_and_authority"
	NodeResolveLegalHolds       = "resolve_legal_and_holds"
	NodeComputeFinalPay         = "compute_final_pay_and_deadline"
	NodeBuildProposal           = "build_termination_proposal"
	NodeApprovalDecision        = "approval_and_sod_decision"
	NodeObserveAccessRevocation = "observe_access_revocation"

	NodeEndPendingObligations  = "end_pending_obligations"
	NodeEndLegalHoldBlocked    = "end_legal_hold_blocked"
	NodeEndSoDViolationBlocked = "end_sod_violation_blocked"
	NodeEndRequiresReinstate   = "end_requires_reinstatement_intent"
	NodeEndRejectedInvalid     = "end_rejected_invalid_request"
	NodeEndDegradedRepair      = "end_degraded_repair"
	NodeEndUnknown             = "end_unknown"
)

// Decision route keys the approval-and-SoD DECISION declares.
const (
	RouteRoutineApprovalRequired = "ROUTINE_APPROVAL_REQUIRED"
	RouteLegalHoldBlocked        = "LEGAL_HOLD_BLOCKED"
	RouteSoDViolationBlocked     = "SOD_VIOLATION_BLOCKED"
	RouteRequiresReinstatement   = "REQUIRES_REINSTATEMENT_INTENT"
	RouteAlreadyEndedInvalid     = "ALREADY_ENDED_INVALID"
	RuleApprovalAndSoD           = "rules.termination.approval_and_separation_of_duties/v1"
)

// Transform references.
const (
	TransformFinalPay      = "transforms.termination.compute_final_pay_and_deadline"
	TransformBuildProposal = "transforms.termination.build_proposal"
)

// Capability identities. This package's own fixture capabilities, never a
// bootstrap or domain capability.
const (
	CapReadEmploymentAuthority = "hcmnext.conformance.termination.read_employment_and_authority"
	CapResolveLegalHolds       = "hcmnext.conformance.termination.resolve_legal_and_holds"
	CapObserveAccessRevocation = "hcmnext.conformance.termination.observe_access_revocation"
)

// Approval requirement ids: step 9 of the reference workflow names HR, legal,
// manager, employee-relations and finance approvals, resolved conditionally.
const (
	ApprovalHR                = "approval.termination.hr_partner"
	ApprovalLegal             = "approval.termination.legal"
	ApprovalManager           = "approval.termination.manager"
	ApprovalEmployeeRelations = "approval.termination.employee_relations"
	ApprovalFinance           = "approval.termination.finance"
)

// Obligation ids. AccessRevocation is P0 and mandatory: GREEN requires it be
// "independently tracked", which this obligation id being distinct from
// every other one is what makes checkable.
const (
	ObligationFinalPay         = "obligation.termination.final_pay"
	ObligationAccessRevocation = "obligation.termination.p0_access_revocation"
	ObligationNoticeEvidence   = "obligation.termination.notice_and_evidence"
	ObligationRecordsRetention = "obligation.termination.records_retention_hold"
	ObligationEquipment        = "obligation.termination.equipment_recovery"
	ObligationBenefits         = "obligation.termination.benefits_continuation"
)

const (
	organizationScope  = "acme/lifecycle"
	purpose            = "SIMULATE_TERMINATION_OFFBOARDING"
	classification     = "CONFIDENTIAL_HR_SENSITIVE"
	dataAccessManifest = "dam.termination.simulation/v1"
	mappingDigest      = "hcmnext.workflow.InputMappingSet/v1"
	// repairRef is the bounded repair artifact the degraded terminal links,
	// which is how a stalled P0 access revocation creates tracked repair
	// evidence rather than a silently dropped observation.
	repairRef = "repair.termination.access_revocation_drift/v1"
)

// ---- small typed-value helpers, mirrored from internal/workflow/promotion.go
// and internal/workflow/conformance/transfer so this package never has to
// reach into another package's unexported helpers.

func str(brand string) workflow.ValueType {
	return workflow.ValueType{Kind: workflow.KindString, Brand: brand}
}
func plainStr() workflow.ValueType  { return workflow.ValueType{Kind: workflow.KindString} }
func boolean() workflow.ValueType   { return workflow.ValueType{Kind: workflow.KindBool} }
func localDate() workflow.ValueType { return workflow.ValueType{Kind: workflow.KindLocalDate} }
func money() workflow.ValueType     { return workflow.ValueType{Kind: workflow.KindMoney} }

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

// ReferenceDefinition returns the P1A termination-and-offboarding reference
// workflow as typed Go values.
//
// It composes exactly what CONF-005's GREEN clause names: employment and
// authority are read once, legal holds and jurisdiction are resolved,
// final-pay and its deadline are computed only once a notice/legal basis
// exists, a proposal is bound, an approval-and-separation-of-duties decision
// blocks a legal hold, a requester-equals-approver conflict or a
// cancellation attempt against an already-ended employment (which becomes a
// distinct reinstatement/correction intent per the REFACTOR clause), and a
// P0 access-revocation observation is tracked independently of every other
// obligation and routes to a bounded repair terminal when it degrades.
func ReferenceDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        WorkflowID,
		Version:           Version,
		Name:              "Termination and offboarding (simulation)",
		InputSchema:       workflowSchema("TerminationOffboardingInput"),
		OutputSchema:      workflowSchema("TerminationOffboardingResult"),
		VariablesSchema:   workflowSchema("TerminationOffboardingVariables"),
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
			{Path: "termination_type", Type: plainStr()},
			{Path: "effective_date", Type: localDate()},
			{Path: "cancel_requested", Type: boolean()},
		},
		Outputs: []workflow.Field{
			{Path: "worker_id", Type: str("WorkerID")},
			{Path: "terminal_code", Type: plainStr()},
		},

		Limits: workflow.Limits{MaxFanOut: 6, MaxDepth: 12, MaxNodes: 24},

		FailurePolicyRef:      "policy.workflow.failure.simulation/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.simulation/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.hr-simulation/v1",

		ApprovalRequirements: []workflow.ApprovalRequirement{
			{ID: ApprovalHR, ResolverExpression: "HRPartnerFor(employment)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalLegal, ResolverExpression: "LegalReviewerFor(jurisdiction)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalManager, ResolverExpression: "CurrentManagerOf(worker)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalEmployeeRelations, ResolverExpression: "EmployeeRelationsFor(employment)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalFinance, ResolverExpression: "FinancePartnerFor(employment)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
		},

		Obligations: []workflow.ObligationRequirement{
			{
				ID: ObligationFinalPay, Authority: "customer.policy.payroll.final_pay",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Compute and schedule final pay by the jurisdiction's deadline",
				ResponsibleParty: "payroll.final_pay", SatisfactionCondition: "final pay amount and deadline are recorded against the proposal digest",
				SourceVersion: RuleApprovalAndSoD, ReevaluationPolicy: workflow.ReevalReevaluate, Mandatory: true,
			},
			{
				ID: ObligationAccessRevocation, Authority: "iam.access.revocation",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Revoke privileged and standard IAM access at the policy-defined instant, tracked independently of every other obligation",
				ResponsibleParty: "iam.access", SatisfactionCondition: "access revocation is observed and reconciled independently of final pay, benefits, equipment and notices",
				SourceVersion: "iam.access.revocation.policy/v2", ReevaluationPolicy: workflow.ReevalRequireReview, Mandatory: true,
			},
			{
				ID: ObligationNoticeEvidence, Authority: "customer.policy.employee_relations",
				InsertionPoint: workflow.InsertApproval, RequiredAction: "Generate the required notice and retain evidence of delivery",
				ResponsibleParty: "employee_relations.notices", SatisfactionCondition: "notice artifact digest recorded against the proposal",
				SourceVersion: "policy.notice.hr-termination/v1", ReevaluationPolicy: workflow.ReevalRequireReview,
			},
			{
				ID: ObligationRecordsRetention, Authority: "records.retention",
				InsertionPoint: workflow.InsertClosure, RequiredAction: "Preserve records and apply any legal hold or retention schedule",
				ResponsibleParty: "operations.records", SatisfactionCondition: "simulation artifact digest recorded with the terminal result",
				SourceVersion: "records.retention.hr-simulation/v1", ReevaluationPolicy: workflow.ReevalPin, Mandatory: true,
			},
			{
				ID: ObligationEquipment, Authority: "operations.equipment",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Schedule equipment recovery",
				ResponsibleParty: "operations.equipment", SatisfactionCondition: "equipment recovery intent recorded against the effective date",
				SourceVersion: "operations.equipment.recovery/v1", ReevaluationPolicy: workflow.ReevalReevaluate,
			},
			{
				ID: ObligationBenefits, Authority: "benefits.continuation",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Schedule benefits continuation or termination notice",
				ResponsibleParty: "benefits.administration", SatisfactionCondition: "benefits disposition recorded against the effective date",
				SourceVersion: "benefits.continuation.policy/v1", ReevaluationPolicy: workflow.ReevalRequireReview,
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
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "employment_id", Source: builders.FromInput("employment_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapReadEmploymentAuthority, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:people.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeResolveLegalHolds,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapResolveLegalHolds, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapResolveLegalHolds, "response"),
			Inputs: []workflow.Field{
				{Path: "employment_id", Type: str("EmploymentID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "legal_hold_active", Type: boolean()},
				{Path: "jurisdiction", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "employment_id", Source: builders.FromInput("employment_id")},
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
				ID: CapResolveLegalHolds, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:legal.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeComputeFinalPay,
			Type:         workflow.StepTransform,
			InputSchema:  workflowSchema("FinalPayDraft"),
			OutputSchema: workflowSchema("FinalPay"),
			Inputs: []workflow.Field{
				{Path: "employment_id", Type: str("EmploymentID")},
				{Path: "effective_date", Type: localDate()},
				{Path: "jurisdiction", Type: plainStr()},
			},
			Outputs: []workflow.Field{
				{Path: "final_pay_deadline", Type: localDate()},
				{Path: "final_pay_amount", Type: money()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "employment_id", Source: builders.FromInput("employment_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
				{Target: "jurisdiction", Source: builders.FromNode(NodeResolveLegalHolds, "jurisdiction")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformFinalPay, Version: 1,
				NormalizationProfile: "hcmnext.canonical.termination_final_pay/v1",
				InputTaint: []workflow.TaintInput{
					{Source: "employment_id", Level: workflow.TaintTrusted},
					{Source: "jurisdiction", Level: workflow.TaintTrusted},
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
			InputSchema:  workflowSchema("TerminationProposalDraft"),
			OutputSchema: workflowSchema("TerminationProposal"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "employment_id", Type: str("EmploymentID")},
				{Path: "final_pay_deadline", Type: localDate()},
				{Path: "jurisdiction", Type: plainStr()},
			},
			Outputs: []workflow.Field{
				{Path: "proposal_digest", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "employment_id", Source: builders.FromInput("employment_id")},
				{Target: "final_pay_deadline", Source: builders.FromNode(NodeComputeFinalPay, "final_pay_deadline")},
				{Target: "jurisdiction", Source: builders.FromNode(NodeResolveLegalHolds, "jurisdiction")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformBuildProposal, Version: 1,
				NormalizationProfile: "hcmnext.canonical.termination_proposal/v1",
				InputTaint: []workflow.TaintInput{
					{Source: "worker_id", Level: workflow.TaintTrusted},
					{Source: "final_pay_deadline", Level: workflow.TaintTrusted},
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
			ID:           NodeApprovalDecision,
			Type:         workflow.StepDecision,
			InputSchema:  workflowSchema("TerminationApprovalDecisionInput"),
			OutputSchema: workflowSchema("TerminationApprovalDecisionResult"),
			Inputs: []workflow.Field{
				{Path: "legal_hold_active", Type: boolean()},
				{Path: "requester_id", Type: str("WorkerID")},
				{Path: "current_manager_id", Type: str("WorkerID")},
				{Path: "cancel_requested", Type: boolean()},
				{Path: "employment_active", Type: boolean()},
			},
			Outputs: []workflow.Field{{Path: "route_key", Type: plainStr()}},
			InputMappings: []workflow.Mapping{
				{Target: "legal_hold_active", Source: builders.FromNode(NodeResolveLegalHolds, "legal_hold_active")},
				{Target: "requester_id", Source: builders.FromInput("requester_id")},
				{Target: "current_manager_id", Source: builders.FromNode(NodeReadEmploymentAuthority, "current_manager_id")},
				{Target: "cancel_requested", Source: builders.FromInput("cancel_requested")},
				{Target: "employment_active", Source: builders.FromNode(NodeReadEmploymentAuthority, "employment_active")},
			},
			Decision: &workflow.DecisionSpec{
				EvaluatorRef: "engines.rules.termination_approval_and_sod", EvaluatorVersion: 1,
				RuleRef: RuleApprovalAndSoD, InputDigestProfile: mappingDigest,
				Routes: []workflow.DecisionRoute{
					{Key: RouteRequiresReinstatement, Predicate: "cancel_requested_after_employment_already_ended", Precedence: 10},
					{Key: RouteAlreadyEndedInvalid, Predicate: "employment_already_ended_without_cancellation", Precedence: 20},
					{Key: RouteLegalHoldBlocked, Predicate: "legal_hold_active", Precedence: 30},
					{Key: RouteSoDViolationBlocked, Predicate: "requester_equals_current_manager", Precedence: 40},
					{Key: RouteRoutineApprovalRequired, Predicate: "no_blocking_condition", Precedence: 50},
				},
				DefaultRoute: RouteRoutineApprovalRequired,
			},
			Governance: workflow.NodeGovernance{
				Purpose: purpose, Classification: classification,
				RevalidationBoundary: workflow.RevalidateNone, DataAccessManifestRef: dataAccessManifest,
			},
		},
		{
			ID:           NodeObserveAccessRevocation,
			Type:         workflow.StepObserve,
			InputSchema:  bootstrapCapabilitySchema(CapObserveAccessRevocation, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapObserveAccessRevocation, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "employment_id", Type: str("EmploymentID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "access_state", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "employment_id", Source: builders.FromInput("employment_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapObserveAccessRevocation, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:iam.read"},
			},
			Observe: &workflow.ObserveSpec{
				EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "iam.access.projection",
				ExpectedStateFields: []string{"employment_id"}, RequiredWatermarks: []string{"iam.access.stream_head"},
				MaxAgeSeconds: 300, ComparisonProfile: "comparison.iam.access_revocation/v1",
				RetryExhaustionRoute: NodeEndDegradedRepair,
			},
			Retry:      &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.observation.bounded/v1"},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:            NodeEndPendingObligations,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TERMINATION_SIMULATION_PENDING_OBLIGATIONS"),
			End: &workflow.EndSpec{
				TerminalCode: "TERMINATION_SIMULATION_PENDING_OBLIGATIONS", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "PENDING_OBSERVATION", "PENDING"),
				OutstandingObligationRefs: []string{
					ObligationFinalPay, ObligationAccessRevocation, ObligationNoticeEvidence,
					ObligationRecordsRetention, ObligationEquipment, ObligationBenefits,
				},
			},
			Governance: terminalGovernance(
				[]string{ObligationFinalPay, ObligationAccessRevocation, ObligationNoticeEvidence, ObligationRecordsRetention, ObligationEquipment, ObligationBenefits},
				[]string{ApprovalHR, ApprovalLegal, ApprovalManager, ApprovalEmployeeRelations, ApprovalFinance},
			),
		},
		{
			ID:            NodeEndLegalHoldBlocked,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TERMINATION_LEGAL_HOLD_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "TERMINATION_LEGAL_HOLD_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndSoDViolationBlocked,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TERMINATION_SOD_VIOLATION_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "TERMINATION_SOD_VIOLATION_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndRequiresReinstate,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TERMINATION_REQUIRES_REINSTATEMENT_INTENT"),
			End: &workflow.EndSpec{
				TerminalCode: "TERMINATION_REQUIRES_REINSTATEMENT_INTENT", RuntimeStatus: workflow.RuntimeCompleted,
				// A cancellation reaching an already-ended employment never
				// mutates the original terminated request: the original
				// request is superseded, and the fact that must be handled is
				// a distinct reinstatement/correction intent (the REFACTOR
				// clause), not a resurrected ChangeRequest against this one.
				CompletionMapping: builders.Completion("SUPERSEDED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndRejectedInvalid,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TERMINATION_REJECTED_INVALID_REQUEST"),
			End: &workflow.EndSpec{
				TerminalCode: "TERMINATION_REJECTED_INVALID_REQUEST", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndDegradedRepair,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TERMINATION_SIMULATION_DEGRADED"),
			End: &workflow.EndSpec{
				TerminalCode: "TERMINATION_SIMULATION_DEGRADED", RuntimeStatus: workflow.RuntimeBlocked,
				CompletionMapping:         builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{ObligationAccessRevocation, ObligationRecordsRetention},
				RepairRefs:                []string{repairRef},
			},
			Governance: terminalGovernance([]string{ObligationAccessRevocation, ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndUnknown,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TERMINATION_SIMULATION_UNKNOWN"),
			End: &workflow.EndSpec{
				TerminalCode: "TERMINATION_SIMULATION_UNKNOWN", RuntimeStatus: workflow.RuntimeBlocked,
				CompletionMapping:         builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
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
	out := capabilityRoutes(NodeReadEmploymentAuthority, NodeResolveLegalHolds)
	out = append(out, capabilityRoutes(NodeResolveLegalHolds, NodeComputeFinalPay)...)
	out = append(out,
		workflow.Edge{From: NodeComputeFinalPay, To: NodeBuildProposal, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeComputeFinalPay, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)},

		workflow.Edge{From: NodeBuildProposal, To: NodeApprovalDecision, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeBuildProposal, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)},

		workflow.Edge{From: NodeApprovalDecision, To: NodeObserveAccessRevocation, RouteKey: RouteRoutineApprovalRequired},
		workflow.Edge{From: NodeApprovalDecision, To: NodeEndLegalHoldBlocked, RouteKey: RouteLegalHoldBlocked},
		workflow.Edge{From: NodeApprovalDecision, To: NodeEndSoDViolationBlocked, RouteKey: RouteSoDViolationBlocked},
		workflow.Edge{From: NodeApprovalDecision, To: NodeEndRequiresReinstate, RouteKey: RouteRequiresReinstatement},
		workflow.Edge{From: NodeApprovalDecision, To: NodeEndRejectedInvalid, RouteKey: RouteAlreadyEndedInvalid},
		workflow.Edge{From: NodeApprovalDecision, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},

		workflow.Edge{From: NodeObserveAccessRevocation, To: NodeEndPendingObligations, RouteKey: string(workflow.OutcomePass)},
		workflow.Edge{From: NodeObserveAccessRevocation, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomeFail)},
		workflow.Edge{From: NodeObserveAccessRevocation, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomePartial)},
		workflow.Edge{From: NodeObserveAccessRevocation, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},
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
		{ID: CapResolveLegalHolds, Version: 1},
		{ID: CapObserveAccessRevocation, Version: 1},
	}
}
