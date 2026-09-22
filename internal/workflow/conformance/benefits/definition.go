// Package benefits is CONF-010's proof that a benefits eligibility/
// election/carrier-reconciliation reference workflow compiles under P1A and
// walks to completion through the real SIMULATE-mode interpreter
// (internal/workflow/simulate), exactly as CONF-001 established for the
// promote-into-management reference and as CONF-003/CONF-005/CONF-009
// established for the cross-company transfer, termination and payroll
// references (internal/workflow/conformance/{transfer,termination,payroll}).
//
// It is a conformance fixture, not a benefits administration system: the
// capability handlers behind every CAPABILITY/OBSERVE node are canned,
// in-memory projections this package owns, never a real carrier or payroll
// integration. What is under test is the workflow's own composition: a
// disputed eligibility fact, an expired enrollment window, a duplicate life
// event or an overlapping election are refused rather than approved, an
// election is built as an immutable revision that supersedes rather than
// overwrites, and carrier and payroll-deduction reconciliation are observed
// independently of each other with PASS/FAIL/PARTIAL/UNKNOWN routed to
// distinct, separately-tracked repair terminals rather than a single
// collapsed "success".
package benefits

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
)

// Workflow identity.
const (
	WorkflowID = "hcmnext.workflows.benefits_eligibility_election_reconciliation"
	Version    = 1
)

// Node ids.
const (
	NodeReadEligibilityFacts           = "read_eligibility_facts"
	NodeBuildEligibilityTrace          = "build_eligibility_trace"
	NodeBuildElectionProposal          = "build_election_proposal"
	NodeEligibilityDecision            = "eligibility_decision"
	NodeObserveCarrierReconciliation   = "observe_carrier_reconciliation"
	NodeObserveDeductionReconciliation = "observe_deduction_reconciliation"

	NodeEndPendingObligations         = "end_pending_obligations"
	NodeEndDisputedFactBlocked        = "end_disputed_fact_blocked"
	NodeEndExpiredWindowBlocked       = "end_expired_window_blocked"
	NodeEndDuplicateLifeEventBlocked  = "end_duplicate_life_event_blocked"
	NodeEndOverlappingElectionBlocked = "end_overlapping_election_blocked"
	NodeEndCarrierDegradedRepair      = "end_carrier_degraded_repair"
	NodeEndDeductionDegradedRepair    = "end_deduction_degraded_repair"
	NodeEndUnknown                    = "end_unknown"
)

// Decision route keys the eligibility DECISION declares.
const (
	RouteDisputedFactBlocked        = "DISPUTED_FACT_BLOCKED"
	RouteExpiredWindowBlocked       = "EXPIRED_WINDOW_BLOCKED"
	RouteDuplicateLifeEventBlocked  = "DUPLICATE_LIFE_EVENT_BLOCKED"
	RouteOverlappingElectionBlocked = "OVERLAPPING_ELECTION_BLOCKED"
	RouteRoutineApprovalRequired    = "ROUTINE_APPROVAL_REQUIRED"
	RuleEligibilityAndElection      = "rules.benefits.eligibility_and_election/v1"
)

// Transform references.
const (
	TransformBuildEligibilityTrace = "transforms.benefits.build_eligibility_trace"
	TransformBuildElectionProposal = "transforms.benefits.build_election_proposal"
)

// Capability identities. This package's own fixture capabilities, never a
// bootstrap or domain capability.
const (
	CapReadEligibilityFacts           = "hcmnext.conformance.benefits.read_eligibility_facts"
	CapObserveCarrierReconciliation   = "hcmnext.conformance.benefits.observe_carrier_reconciliation"
	CapObserveDeductionReconciliation = "hcmnext.conformance.benefits.observe_deduction_reconciliation"
)

// Approval requirement ids.
const (
	ApprovalBenefitsAdmin    = "approval.benefits.admin"
	ApprovalHRPartner        = "approval.benefits.hr_partner"
	ApprovalPayrollDeduction = "approval.benefits.payroll_deduction_setup"
)

// Obligation ids. EligibilityTrace and ElectionRevision are mandatory,
// which is what makes CONF-010's "returns eligibility trace and immutable
// election revisions" checkable; CarrierReconciliation and
// DeductionReconciliation are mandatory and independently tracked, which is
// what makes "partial carrier/deduction outcome becomes full success"
// checkable as a RED case rather than merely asserted.
const (
	ObligationEligibilityTrace        = "obligation.benefits.eligibility_trace"
	ObligationElectionRevision        = "obligation.benefits.election_revision_immutability"
	ObligationCarrierReconciliation   = "obligation.benefits.carrier_reconciliation"
	ObligationDeductionReconciliation = "obligation.benefits.payroll_deduction_reconciliation"
	ObligationRecordsRetention        = "obligation.benefits.records_retention"
)

const (
	organizationScope  = "acme/benefits"
	purpose            = "SIMULATE_BENEFITS_ELIGIBILITY_ELECTION_RECONCILIATION"
	classification     = "CONFIDENTIAL_BENEFITS"
	dataAccessManifest = "dam.benefits.simulation/v1"
	mappingDigest      = "hcmnext.workflow.InputMappingSet/v1"
	// repairRef is the bounded repair artifact a degraded reconciliation
	// terminal links, which is how a failed or partial carrier/deduction
	// outcome creates tracked repair evidence rather than a silently dropped
	// observation.
	repairRef = "repair.benefits.reconciliation_drift/v1"
)

// ---- small typed-value helpers, mirrored from
// internal/workflow/conformance/{transfer,termination,payroll} so this
// package never has to reach into another package's unexported helpers.

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

// ReferenceDefinition returns the P1A benefits eligibility/election/
// carrier-reconciliation reference workflow as typed Go values.
//
// It composes exactly what CONF-010's GREEN clause names: eligibility facts
// are read once and a disputed fact, an expired enrollment window, a
// duplicate life event or an overlapping election are each refused with a
// distinct route; a routine case builds an eligibility trace and an
// immutable election revision (never an overwrite of a prior election); and
// carrier reconciliation and payroll-deduction reconciliation are observed
// independently, each with its own PASS/FAIL/PARTIAL/UNKNOWN and its own
// repair terminal.
func ReferenceDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        WorkflowID,
		Version:           Version,
		Name:              "Benefits eligibility, election and carrier reconciliation (simulation)",
		InputSchema:       workflowSchema("BenefitsEligibilityElectionInput"),
		OutputSchema:      workflowSchema("BenefitsEligibilityElectionResult"),
		VariablesSchema:   workflowSchema("BenefitsEligibilityElectionVariables"),
		TenantScope:       "acme",
		OrganizationScope: organizationScope,
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeSimulate},
		TerminalProfile:   workflow.TerminalProfileSimulateOnly,
		StartNodeID:       NodeReadEligibilityFacts,

		Inputs: []workflow.Field{
			{Path: "worker_id", Type: str("WorkerID")},
			{Path: "life_event_id", Type: str("LifeEventID")},
			{Path: "plan_id", Type: str("BenefitPlanID")},
			{Path: "prior_election_digest", Type: plainStr()},
			{Path: "effective_date", Type: localDate()},
		},
		Outputs: []workflow.Field{
			{Path: "worker_id", Type: str("WorkerID")},
			{Path: "terminal_code", Type: plainStr()},
		},

		Limits: workflow.Limits{MaxFanOut: 6, MaxDepth: 14, MaxNodes: 28},

		FailurePolicyRef:      "policy.workflow.failure.simulation/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.simulation/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.hr-simulation/v1",

		ApprovalRequirements: []workflow.ApprovalRequirement{
			{ID: ApprovalBenefitsAdmin, ResolverExpression: "BenefitsAdminFor(plan)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalHRPartner, ResolverExpression: "HRPartnerFor(worker)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalPayrollDeduction, ResolverExpression: "PayrollDeductionSetupFor(worker)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
		},

		Obligations: []workflow.ObligationRequirement{
			{
				ID: ObligationEligibilityTrace, Authority: "customer.policy.benefits.eligibility",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Retain the eligibility evaluation trace bound to the election proposal digest",
				ResponsibleParty: "benefits.eligibility", SatisfactionCondition: "eligibility trace digest is recorded against the election proposal digest",
				SourceVersion: RuleEligibilityAndElection, ReevaluationPolicy: workflow.ReevalReevaluate, Mandatory: true,
			},
			{
				ID: ObligationElectionRevision, Authority: "customer.policy.benefits.election",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Record the election as a new immutable revision that supersedes, never overwrites, any prior election",
				ResponsibleParty: "benefits.election", SatisfactionCondition: "the election revision digest names its prior election digest rather than replacing it in place",
				SourceVersion: "benefits.election.immutability/v1", ReevaluationPolicy: workflow.ReevalPin, Mandatory: true,
			},
			{
				ID: ObligationCarrierReconciliation, Authority: "benefits.carrier.reconciliation",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Observe and reconcile the carrier's enrollment confirmation independently of the payroll deduction outcome",
				ResponsibleParty: "benefits.carrier", SatisfactionCondition: "carrier reconciliation outcome is observed and reconciled independently of the deduction outcome",
				SourceVersion: "benefits.carrier.reconciliation.policy/v2", ReevaluationPolicy: workflow.ReevalRequireReview, Mandatory: true,
			},
			{
				ID: ObligationDeductionReconciliation, Authority: "payroll.deduction.reconciliation",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Observe and reconcile the payroll deduction setup independently of the carrier outcome",
				ResponsibleParty: "payroll.deduction", SatisfactionCondition: "deduction reconciliation outcome is observed and reconciled independently of the carrier outcome",
				SourceVersion: "payroll.deduction.reconciliation.policy/v2", ReevaluationPolicy: workflow.ReevalRequireReview, Mandatory: true,
			},
			{
				ID: ObligationRecordsRetention, Authority: "records.retention",
				InsertionPoint: workflow.InsertClosure, RequiredAction: "Preserve the simulation artifact and its eligibility/election evidence",
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
			ID:           NodeReadEligibilityFacts,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapReadEligibilityFacts, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapReadEligibilityFacts, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "life_event_id", Type: str("LifeEventID")},
				{Path: "plan_id", Type: str("BenefitPlanID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "fact_disputed", Type: boolean()},
				{Path: "enrollment_window_expired", Type: boolean()},
				{Path: "life_event_duplicate", Type: boolean()},
				{Path: "election_overlap_detected", Type: boolean()},
				{Path: "eligibility_watermark", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "life_event_id", Source: builders.FromInput("life_event_id")},
				{Target: "plan_id", Source: builders.FromInput("plan_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapReadEligibilityFacts, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:benefits.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeBuildEligibilityTrace,
			Type:         workflow.StepTransform,
			InputSchema:  workflowSchema("BenefitsEligibilityTraceDraft"),
			OutputSchema: workflowSchema("BenefitsEligibilityTrace"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "plan_id", Type: str("BenefitPlanID")},
				{Path: "eligibility_watermark", Type: plainStr()},
			},
			Outputs: []workflow.Field{
				{Path: "eligibility_trace_digest", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "plan_id", Source: builders.FromInput("plan_id")},
				{Target: "eligibility_watermark", Source: builders.FromNode(NodeReadEligibilityFacts, "eligibility_watermark")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformBuildEligibilityTrace, Version: 1,
				NormalizationProfile: "hcmnext.canonical.benefits_eligibility_trace/v1",
				InputTaint: []workflow.TaintInput{
					{Source: "worker_id", Level: workflow.TaintTrusted},
					{Source: "eligibility_watermark", Level: workflow.TaintTrusted},
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
			ID:           NodeBuildElectionProposal,
			Type:         workflow.StepTransform,
			InputSchema:  workflowSchema("BenefitsElectionProposalDraft"),
			OutputSchema: workflowSchema("BenefitsElectionProposal"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "plan_id", Type: str("BenefitPlanID")},
				{Path: "prior_election_digest", Type: plainStr()},
				{Path: "eligibility_trace_digest", Type: plainStr()},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "election_digest", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "plan_id", Source: builders.FromInput("plan_id")},
				{Target: "prior_election_digest", Source: builders.FromInput("prior_election_digest")},
				{Target: "eligibility_trace_digest", Source: builders.FromNode(NodeBuildEligibilityTrace, "eligibility_trace_digest")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformBuildElectionProposal, Version: 1,
				NormalizationProfile: "hcmnext.canonical.benefits_election_proposal/v1",
				InputTaint: []workflow.TaintInput{
					{Source: "worker_id", Level: workflow.TaintTrusted},
					{Source: "eligibility_trace_digest", Level: workflow.TaintTrusted},
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
			ID:           NodeEligibilityDecision,
			Type:         workflow.StepDecision,
			InputSchema:  workflowSchema("BenefitsEligibilityDecisionInput"),
			OutputSchema: workflowSchema("BenefitsEligibilityDecisionResult"),
			Inputs: []workflow.Field{
				{Path: "fact_disputed", Type: boolean()},
				{Path: "enrollment_window_expired", Type: boolean()},
				{Path: "life_event_duplicate", Type: boolean()},
				{Path: "election_overlap_detected", Type: boolean()},
			},
			Outputs: []workflow.Field{{Path: "route_key", Type: plainStr()}},
			InputMappings: []workflow.Mapping{
				{Target: "fact_disputed", Source: builders.FromNode(NodeReadEligibilityFacts, "fact_disputed")},
				{Target: "enrollment_window_expired", Source: builders.FromNode(NodeReadEligibilityFacts, "enrollment_window_expired")},
				{Target: "life_event_duplicate", Source: builders.FromNode(NodeReadEligibilityFacts, "life_event_duplicate")},
				{Target: "election_overlap_detected", Source: builders.FromNode(NodeReadEligibilityFacts, "election_overlap_detected")},
			},
			Decision: &workflow.DecisionSpec{
				EvaluatorRef: "engines.rules.benefits_eligibility_and_election", EvaluatorVersion: 1,
				RuleRef: RuleEligibilityAndElection, InputDigestProfile: mappingDigest,
				Routes: []workflow.DecisionRoute{
					{Key: RouteDisputedFactBlocked, Predicate: "eligibility_fact_disputed", Precedence: 10},
					{Key: RouteExpiredWindowBlocked, Predicate: "enrollment_window_expired", Precedence: 20},
					{Key: RouteDuplicateLifeEventBlocked, Predicate: "life_event_duplicate", Precedence: 30},
					{Key: RouteOverlappingElectionBlocked, Predicate: "election_overlap_detected", Precedence: 40},
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
			ID:           NodeObserveCarrierReconciliation,
			Type:         workflow.StepObserve,
			InputSchema:  bootstrapCapabilitySchema(CapObserveCarrierReconciliation, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapObserveCarrierReconciliation, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "election_digest", Type: plainStr()},
			},
			Outputs: []workflow.Field{
				{Path: "carrier_state", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "election_digest", Source: builders.FromNode(NodeBuildElectionProposal, "election_digest")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapObserveCarrierReconciliation, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:benefits.carrier.read"},
			},
			Observe: &workflow.ObserveSpec{
				EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "benefits.carrier.enrollment_projection",
				ExpectedStateFields: []string{"worker_id"}, RequiredWatermarks: []string{"benefits.carrier.stream_head"},
				MaxAgeSeconds: 300, ComparisonProfile: "comparison.benefits.carrier_reconciliation/v1",
				RetryExhaustionRoute: NodeEndCarrierDegradedRepair,
			},
			Retry:      &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.observation.bounded/v1"},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeObserveDeductionReconciliation,
			Type:         workflow.StepObserve,
			InputSchema:  bootstrapCapabilitySchema(CapObserveDeductionReconciliation, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapObserveDeductionReconciliation, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "election_digest", Type: plainStr()},
			},
			Outputs: []workflow.Field{
				{Path: "deduction_state", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "election_digest", Source: builders.FromNode(NodeBuildElectionProposal, "election_digest")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapObserveDeductionReconciliation, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:payroll.deduction.read"},
			},
			Observe: &workflow.ObserveSpec{
				EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "payroll.deduction.setup_projection",
				ExpectedStateFields: []string{"worker_id"}, RequiredWatermarks: []string{"payroll.deduction.stream_head"},
				MaxAgeSeconds: 300, ComparisonProfile: "comparison.benefits.deduction_reconciliation/v1",
				RetryExhaustionRoute: NodeEndDeductionDegradedRepair,
			},
			Retry:      &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.observation.bounded/v1"},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:            NodeEndPendingObligations,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "BENEFITS_SIMULATION_PENDING_OBLIGATIONS"),
			End: &workflow.EndSpec{
				TerminalCode: "BENEFITS_SIMULATION_PENDING_OBLIGATIONS", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "PENDING_OBSERVATION", "PENDING"),
				OutstandingObligationRefs: []string{
					ObligationEligibilityTrace, ObligationElectionRevision, ObligationCarrierReconciliation,
					ObligationDeductionReconciliation, ObligationRecordsRetention,
				},
			},
			Governance: terminalGovernance(
				[]string{ObligationEligibilityTrace, ObligationElectionRevision, ObligationCarrierReconciliation, ObligationDeductionReconciliation, ObligationRecordsRetention},
				[]string{ApprovalBenefitsAdmin, ApprovalHRPartner, ApprovalPayrollDeduction},
			),
		},
		{
			ID:            NodeEndDisputedFactBlocked,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "BENEFITS_DISPUTED_FACT_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "BENEFITS_DISPUTED_FACT_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndExpiredWindowBlocked,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "BENEFITS_EXPIRED_WINDOW_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "BENEFITS_EXPIRED_WINDOW_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndDuplicateLifeEventBlocked,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "BENEFITS_DUPLICATE_LIFE_EVENT_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "BENEFITS_DUPLICATE_LIFE_EVENT_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndOverlappingElectionBlocked,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "BENEFITS_OVERLAPPING_ELECTION_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "BENEFITS_OVERLAPPING_ELECTION_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndCarrierDegradedRepair,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "BENEFITS_CARRIER_RECONCILIATION_DEGRADED"),
			End: &workflow.EndSpec{
				TerminalCode: "BENEFITS_CARRIER_RECONCILIATION_DEGRADED", RuntimeStatus: workflow.RuntimeBlocked,
				CompletionMapping:         builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{ObligationCarrierReconciliation, ObligationRecordsRetention},
				RepairRefs:                []string{repairRef},
			},
			Governance: terminalGovernance([]string{ObligationCarrierReconciliation, ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndDeductionDegradedRepair,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "BENEFITS_DEDUCTION_RECONCILIATION_DEGRADED"),
			End: &workflow.EndSpec{
				TerminalCode: "BENEFITS_DEDUCTION_RECONCILIATION_DEGRADED", RuntimeStatus: workflow.RuntimeBlocked,
				CompletionMapping:         builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{ObligationDeductionReconciliation, ObligationRecordsRetention},
				RepairRefs:                []string{repairRef},
			},
			Governance: terminalGovernance([]string{ObligationDeductionReconciliation, ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndUnknown,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "BENEFITS_SIMULATION_UNKNOWN"),
			End: &workflow.EndSpec{
				TerminalCode: "BENEFITS_SIMULATION_UNKNOWN", RuntimeStatus: workflow.RuntimeBlocked,
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
			{From: from, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeRejected)},
			{From: from, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},
			{From: from, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeAmbiguous)},
		}
	}
	out := capabilityRoutes(NodeReadEligibilityFacts, NodeBuildEligibilityTrace)
	out = append(out,
		workflow.Edge{From: NodeBuildEligibilityTrace, To: NodeBuildElectionProposal, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeBuildEligibilityTrace, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)},

		workflow.Edge{From: NodeBuildElectionProposal, To: NodeEligibilityDecision, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeBuildElectionProposal, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)},

		workflow.Edge{From: NodeEligibilityDecision, To: NodeObserveCarrierReconciliation, RouteKey: RouteRoutineApprovalRequired},
		workflow.Edge{From: NodeEligibilityDecision, To: NodeEndDisputedFactBlocked, RouteKey: RouteDisputedFactBlocked},
		workflow.Edge{From: NodeEligibilityDecision, To: NodeEndExpiredWindowBlocked, RouteKey: RouteExpiredWindowBlocked},
		workflow.Edge{From: NodeEligibilityDecision, To: NodeEndDuplicateLifeEventBlocked, RouteKey: RouteDuplicateLifeEventBlocked},
		workflow.Edge{From: NodeEligibilityDecision, To: NodeEndOverlappingElectionBlocked, RouteKey: RouteOverlappingElectionBlocked},
		workflow.Edge{From: NodeEligibilityDecision, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},

		workflow.Edge{From: NodeObserveCarrierReconciliation, To: NodeObserveDeductionReconciliation, RouteKey: string(workflow.OutcomePass)},
		workflow.Edge{From: NodeObserveCarrierReconciliation, To: NodeEndCarrierDegradedRepair, RouteKey: string(workflow.OutcomeFail)},
		workflow.Edge{From: NodeObserveCarrierReconciliation, To: NodeEndCarrierDegradedRepair, RouteKey: string(workflow.OutcomePartial)},
		workflow.Edge{From: NodeObserveCarrierReconciliation, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},

		workflow.Edge{From: NodeObserveDeductionReconciliation, To: NodeEndPendingObligations, RouteKey: string(workflow.OutcomePass)},
		workflow.Edge{From: NodeObserveDeductionReconciliation, To: NodeEndDeductionDegradedRepair, RouteKey: string(workflow.OutcomeFail)},
		workflow.Edge{From: NodeObserveDeductionReconciliation, To: NodeEndDeductionDegradedRepair, RouteKey: string(workflow.OutcomePartial)},
		workflow.Edge{From: NodeObserveDeductionReconciliation, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},
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
		{ID: CapReadEligibilityFacts, Version: 1},
		{ID: CapObserveCarrierReconciliation, Version: 1},
		{ID: CapObserveDeductionReconciliation, Version: 1},
	}
}
