// Package hrcase is CONF-014's proof that the HR case investigation and
// disposition reference workflow (planning/data/models/talent-experience-cases.md,
// planning/specs/business-intent-catalog.md) compiles under P1A and walks to
// completion through the real SIMULATE-mode interpreter
// (internal/workflow/simulate), exactly as CONF-001 established for the
// promote-into-management reference and as CONF-003/CONF-005 established for
// the cross-company transfer and termination-and-offboarding references
// (internal/workflow/conformance/transfer, internal/workflow/conformance/termination).
//
// It is a conformance fixture, not a domain implementation: the capability
// handlers behind every CAPABILITY/OBSERVE node are canned, in-memory
// projections this package owns, never a real case-management, legal-hold or
// IAM integration. What is under test is the workflow's own composition: an
// assignment fence that is relationship-scoped, a participant read that is
// purpose-scoped, an investigator-equals-subject conflict, a matter-wall and
// legal-hold gate that blocks rather than races past a disposition, a
// retaliation signal that forces an Employee Relations escalation rather than
// being folded into routine disposition, an appeal against an already-disposed
// case that becomes a distinct reopen intent rather than mutating the
// original disposition's history, and a confidential-evidence-custody
// obligation that is independently tracked from every other obligation.
package hrcase

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
)

// Workflow identity.
const (
	WorkflowID = "hcmnext.workflows.hr_case_investigation_disposition"
	Version    = 1
)

// Node ids. Exported so the conformance tests can name the exact path a
// golden walk takes without re-deriving it from the definition.
const (
	NodeReadCaseAssignment          = "read_case_assignment"
	NodeReadParticipantAuthz        = "read_participant_authorization"
	NodeReadRetaliationSignal       = "read_retaliation_signal"
	NodeResolveMatterWallAndHold    = "resolve_matter_wall_and_hold"
	NodeComputeCaseFootprint        = "compute_case_footprint"
	NodeBuildDispositionProposal    = "build_disposition_proposal"
	NodeCaseDecision                = "case_decision"
	NodeObserveRecordCustody        = "observe_case_record_custody"
	NodeObserveRecordCustodyEscalat = "observe_case_record_custody_escalation"

	NodeEndPendingDisposition           = "end_pending_disposition"
	NodeEndRetaliationEscalationPending = "end_retaliation_escalation_pending"
	NodeEndUnauthorizedParticipant      = "end_unauthorized_participant_blocked"
	NodeEndInvestigatorConflictBlocked  = "end_investigator_conflict_blocked"
	NodeEndMatterWallBreachBlocked      = "end_matter_wall_breach_blocked"
	NodeEndLegalHoldRaceBlocked         = "end_legal_hold_race_blocked"
	NodeEndAlreadyDisposedInvalid       = "end_already_disposed_invalid"
	NodeEndAppealRequiresReopen         = "end_appeal_requires_reopen"
	NodeEndDegradedRepair               = "end_degraded_repair"
	NodeEndUnknown                      = "end_unknown"
)

// Decision route keys the case-disposition DECISION declares, in precedence
// order (lower precedence is evaluated first).
const (
	RouteAppealRequiresReopen        = "APPEAL_AFTER_DISPOSITION_REQUIRES_REOPEN"
	RouteCaseAlreadyDisposedInvalid  = "CASE_ALREADY_DISPOSED_INVALID"
	RouteUnauthorizedParticipant     = "UNAUTHORIZED_PARTICIPANT_BLOCKED"
	RouteInvestigatorConflictBlocked = "INVESTIGATOR_CONFLICT_BLOCKED"
	RouteMatterWallBreachBlocked     = "MATTER_WALL_BREACH_BLOCKED"
	RouteLegalHoldRaceBlocked        = "LEGAL_HOLD_RACE_BLOCKED"
	RouteRetaliationEscalation       = "RETALIATION_EXPOSURE_ESCALATION_REQUIRED"
	RouteRoutineDispositionRequired  = "ROUTINE_DISPOSITION_REQUIRED"
	RuleCaseDisposition              = "rules.hrcase.case_disposition/v1"
)

// Transform references the two TRANSFORM nodes bind.
const (
	TransformCaseFootprint = "transforms.hrcase.compute_case_footprint"
	TransformBuildProposal = "transforms.hrcase.build_disposition_proposal"
)

// Capability identities. They are this package's own fixture capabilities,
// never a bootstrap or domain capability: CONF-014 proves the workflow's own
// composition, not a real case-management, legal-hold or IAM integration.
const (
	CapReadCaseAssignment       = "hcmnext.conformance.hrcase.read_case_assignment"
	CapReadParticipantAuthz     = "hcmnext.conformance.hrcase.read_participant_authorization"
	CapReadRetaliationSignal    = "hcmnext.conformance.hrcase.read_retaliation_signal"
	CapResolveMatterWallAndHold = "hcmnext.conformance.hrcase.resolve_matter_wall_and_hold"
	CapObserveRecordCustody     = "hcmnext.conformance.hrcase.observe_case_record_custody"
)

// Approval requirement ids: the routine disposition terminal names the HR
// case manager, Employee Relations and legal (SoD); the retaliation
// escalation terminal adds a distinct Employee Relations escalation review.
const (
	ApprovalHRCaseManager            = "approval.hrcase.hr_case_manager"
	ApprovalEmployeeRelations        = "approval.hrcase.employee_relations"
	ApprovalLegal                    = "approval.hrcase.legal"
	ApprovalEmployeeRelationsEscalat = "approval.hrcase.employee_relations_escalation_review"
)

// Obligation ids. EvidenceCustody is independently tracked and mandatory: the
// GREEN clause's "confidential notes/evidence custody" requirement is what
// this obligation being distinct from every other one, and never widened at a
// degraded terminal, makes checkable.
const (
	ObligationEvidenceCustody    = "obligation.hrcase.confidential_evidence_custody"
	ObligationSLATracking        = "obligation.hrcase.sla_tracking"
	ObligationFindingRecord      = "obligation.hrcase.finding_record"
	ObligationRecordsRetention   = "obligation.hrcase.records_retention"
	ObligationRetaliationEscalat = "obligation.hrcase.retaliation_escalation_review"
)

const (
	organizationScope  = "acme/employee_relations"
	purpose            = "SIMULATE_HR_CASE_INVESTIGATION_DISPOSITION"
	classification     = "CONFIDENTIAL_HR_CASE"
	dataAccessManifest = "dam.hrcase.simulation/v1"
	mappingDigest      = "hcmnext.workflow.InputMappingSet/v1"
	// repairRef is the bounded repair artifact the degraded terminal links,
	// which is how a stalled record-custody observation creates tracked
	// repair evidence rather than a silently dropped observation.
	repairRef = "repair.hrcase.record_custody_drift/v1"
)

// ---- small typed-value helpers, mirrored from internal/workflow/promotion.go
// and internal/workflow/conformance/transfer and .../termination, so this
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

// ReferenceDefinition returns the P1A HR case investigation-and-disposition
// reference workflow as typed Go values.
//
// It composes exactly what CONF-014's GREEN clause names: an assignment fence
// (case_active, a relationship-scoped subject scope and a separately
// authority-scoped investigator read), a purpose-scoped participant-access
// read, a retaliation signal read that is never silently ignored, a
// matter-wall-and-legal-hold resolution pinned to a LegalContext snapshot, a
// footprint that flags an investigator investigating themselves, a bound
// disposition proposal, a case-disposition DECISION whose precedence blocks
// an unauthorized participant, an investigator conflict, an active matter
// wall, a racing legal hold and an already-disposed case (unless the request
// is an appeal, which becomes a distinct reopen intent rather than mutating
// the original disposition), routes a detected retaliation signal to a
// mandatory Employee Relations escalation rather than routine disposition,
// and a post-decision record-custody observation whose PASS/FAIL/UNKNOWN
// answer decides between a consistent pending-disposition terminal and a
// bounded repair route.
func ReferenceDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        WorkflowID,
		Version:           Version,
		Name:              "HR case investigation and disposition (simulation)",
		InputSchema:       workflowSchema("HRCaseInvestigationDispositionInput"),
		OutputSchema:      workflowSchema("HRCaseInvestigationDispositionResult"),
		VariablesSchema:   workflowSchema("HRCaseInvestigationDispositionVariables"),
		TenantScope:       "acme",
		OrganizationScope: organizationScope,
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeSimulate},
		TerminalProfile:   workflow.TerminalProfileSimulateOnly,
		StartNodeID:       NodeReadCaseAssignment,

		Inputs: []workflow.Field{
			{Path: "case_id", Type: str("CaseID")},
			{Path: "subject_worker_id", Type: str("WorkerID")},
			{Path: "investigator_id", Type: str("WorkerID")},
			{Path: "requester_id", Type: str("WorkerID")},
			{Path: "effective_date", Type: localDate()},
			{Path: "finding_summary", Type: plainStr()},
			{Path: "appeal_requested", Type: boolean()},
			{Path: "case_already_disposed", Type: boolean()},
		},
		Outputs: []workflow.Field{
			{Path: "case_id", Type: str("CaseID")},
			{Path: "terminal_code", Type: plainStr()},
		},

		Limits: workflow.Limits{MaxFanOut: 10, MaxDepth: 14, MaxNodes: 24},

		FailurePolicyRef:      "policy.workflow.failure.simulation/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.simulation/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.hr-simulation/v1",

		ApprovalRequirements: []workflow.ApprovalRequirement{
			{ID: ApprovalHRCaseManager, ResolverExpression: "HRCaseManagerFor(case)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalEmployeeRelations, ResolverExpression: "EmployeeRelationsFor(case)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalLegal, ResolverExpression: "LegalReviewerFor(case)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalEmployeeRelationsEscalat, ResolverExpression: "EmployeeRelationsEscalationReviewerFor(case)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
		},

		Obligations: []workflow.ObligationRequirement{
			{
				ID: ObligationEvidenceCustody, Authority: "customer.policy.employee_relations.evidence_custody",
				InsertionPoint:   workflow.InsertSimulation,
				RequiredAction:   "Retain confidential investigation notes and evidence under a restricted custody chain, tracked independently of every other obligation",
				ResponsibleParty: "employee_relations.case_records", SatisfactionCondition: "evidence custody digest recorded against the proposal and case record",
				SourceVersion: RuleCaseDisposition, ReevaluationPolicy: workflow.ReevalRequireReview, Mandatory: true,
			},
			{
				ID: ObligationSLATracking, Authority: "customer.policy.employee_relations.sla",
				InsertionPoint:   workflow.InsertSimulation,
				RequiredAction:   "Track the case's SLA clock against the jurisdiction's investigation deadline",
				ResponsibleParty: "employee_relations.case_records", SatisfactionCondition: "SLA due date recorded against the proposal digest",
				SourceVersion: "policy.hrcase.sla/v1", ReevaluationPolicy: workflow.ReevalReevaluate,
			},
			{
				ID: ObligationFindingRecord, Authority: "customer.policy.employee_relations.finding",
				InsertionPoint:   workflow.InsertSimulation,
				RequiredAction:   "Record the investigation finding and disposition summary against the case",
				ResponsibleParty: "employee_relations.case_records", SatisfactionCondition: "finding record digest recorded against the proposal",
				SourceVersion: "policy.hrcase.finding/v1", ReevaluationPolicy: workflow.ReevalRequireReview,
			},
			{
				ID: ObligationRecordsRetention, Authority: "records.retention",
				InsertionPoint:   workflow.InsertClosure,
				RequiredAction:   "Preserve case records under retention and any applicable legal hold",
				ResponsibleParty: "operations.records", SatisfactionCondition: "simulation artifact digest recorded with the terminal result",
				SourceVersion: "records.retention.hr-simulation/v1", ReevaluationPolicy: workflow.ReevalPin, Mandatory: true,
			},
			{
				ID: ObligationRetaliationEscalat, Authority: "customer.policy.employee_relations.retaliation_escalation",
				InsertionPoint:   workflow.InsertApproval,
				RequiredAction:   "Route the case to Employee Relations escalation review before any disposition is finalized",
				ResponsibleParty: "employee_relations.escalation", SatisfactionCondition: "escalation review approval recorded against the proposal digest",
				SourceVersion: "policy.hrcase.retaliation_escalation/v1", ReevaluationPolicy: workflow.ReevalRequireReview, Mandatory: true,
			},
		},

		Nodes: nodes(),
		Edges: edges(),
	}
}

func nodes() []workflow.Node {
	return []workflow.Node{
		{
			ID:           NodeReadCaseAssignment,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapReadCaseAssignment, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapReadCaseAssignment, "response"),
			Inputs: []workflow.Field{
				{Path: "case_id", Type: str("CaseID")},
				{Path: "subject_worker_id", Type: str("WorkerID")},
				{Path: "investigator_id", Type: str("WorkerID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "case_active", Type: boolean()},
				{Path: "investigator_authority_scope", Type: plainStr()},
				{Path: "subject_relationship_scope", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "case_id", Source: builders.FromInput("case_id")},
				{Target: "subject_worker_id", Source: builders.FromInput("subject_worker_id")},
				{Target: "investigator_id", Source: builders.FromInput("investigator_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapReadCaseAssignment, Version: 1, OperationMode: workflow.ModeSimulate,
				// The assignment fence's own scope: this is the "case is
				// relationship-scoped" half of the REFACTOR clause. It never
				// carries the participant capability's scope below.
				AuthorityScopes: []string{"scope:hrcase.assignment.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeReadParticipantAuthz,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapReadParticipantAuthz, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapReadParticipantAuthz, "response"),
			Inputs: []workflow.Field{
				{Path: "case_id", Type: str("CaseID")},
				{Path: "requester_id", Type: str("WorkerID")},
			},
			Outputs: []workflow.Field{
				{Path: "participant_authorized", Type: boolean()},
				{Path: "participant_purpose_scope", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "case_id", Source: builders.FromInput("case_id")},
				{Target: "requester_id", Source: builders.FromInput("requester_id")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapReadParticipantAuthz, Version: 1, OperationMode: workflow.ModeSimulate,
				// A distinct, purpose-scoped read: the "case access is
				// purpose-scoped" half of the REFACTOR clause. It never
				// shares a scope with the assignment fence above.
				AuthorityScopes: []string{"scope:hrcase.participant.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeReadRetaliationSignal,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapReadRetaliationSignal, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapReadRetaliationSignal, "response"),
			Inputs: []workflow.Field{
				{Path: "case_id", Type: str("CaseID")},
				{Path: "subject_worker_id", Type: str("WorkerID")},
			},
			Outputs: []workflow.Field{
				{Path: "retaliation_signal_detected", Type: boolean()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "case_id", Source: builders.FromInput("case_id")},
				{Target: "subject_worker_id", Source: builders.FromInput("subject_worker_id")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapReadRetaliationSignal, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:hrcase.retaliation.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeResolveMatterWallAndHold,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapResolveMatterWallAndHold, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapResolveMatterWallAndHold, "response"),
			Inputs: []workflow.Field{
				{Path: "case_id", Type: str("CaseID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "matter_wall_active", Type: boolean()},
				{Path: "legal_hold_active", Type: boolean()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "case_id", Source: builders.FromInput("case_id")},
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
				ID: CapResolveMatterWallAndHold, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:legal.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeComputeCaseFootprint,
			Type:         workflow.StepTransform,
			InputSchema:  workflowSchema("CaseFootprintDraft"),
			OutputSchema: workflowSchema("CaseFootprint"),
			Inputs: []workflow.Field{
				{Path: "case_active", Type: boolean()},
				{Path: "investigator_authority_scope", Type: plainStr()},
				{Path: "subject_relationship_scope", Type: plainStr()},
				{Path: "investigator_id", Type: str("WorkerID")},
				{Path: "subject_worker_id", Type: str("WorkerID")},
			},
			Outputs: []workflow.Field{
				{Path: "investigator_conflict", Type: boolean()},
				{Path: "footprint_digest", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "case_active", Source: builders.FromNode(NodeReadCaseAssignment, "case_active")},
				{Target: "investigator_authority_scope", Source: builders.FromNode(NodeReadCaseAssignment, "investigator_authority_scope")},
				{Target: "subject_relationship_scope", Source: builders.FromNode(NodeReadCaseAssignment, "subject_relationship_scope")},
				{Target: "investigator_id", Source: builders.FromInput("investigator_id")},
				{Target: "subject_worker_id", Source: builders.FromInput("subject_worker_id")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformCaseFootprint, Version: 1,
				NormalizationProfile: "hcmnext.canonical.hrcase_footprint/v1",
				InputTaint: []workflow.TaintInput{
					{Source: "investigator_authority_scope", Level: workflow.TaintTrusted},
					{Source: "subject_relationship_scope", Level: workflow.TaintTrusted},
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
			ID:           NodeBuildDispositionProposal,
			Type:         workflow.StepTransform,
			InputSchema:  workflowSchema("DispositionProposalDraft"),
			OutputSchema: workflowSchema("DispositionProposal"),
			Inputs: []workflow.Field{
				{Path: "case_id", Type: str("CaseID")},
				{Path: "footprint_digest", Type: plainStr()},
				{Path: "effective_date", Type: localDate()},
				{Path: "finding_summary", Type: plainStr()},
			},
			Outputs: []workflow.Field{
				{Path: "proposal_digest", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "case_id", Source: builders.FromInput("case_id")},
				{Target: "footprint_digest", Source: builders.FromNode(NodeComputeCaseFootprint, "footprint_digest")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
				{Target: "finding_summary", Source: builders.FromInput("finding_summary")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformBuildProposal, Version: 1,
				NormalizationProfile: "hcmnext.canonical.hrcase_disposition_proposal/v1",
				InputTaint: []workflow.TaintInput{
					{Source: "case_id", Level: workflow.TaintTrusted},
					{Source: "footprint_digest", Level: workflow.TaintTrusted},
					{Source: "finding_summary", Level: workflow.TaintTrusted},
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
			ID:           NodeCaseDecision,
			Type:         workflow.StepDecision,
			InputSchema:  workflowSchema("CaseDispositionDecisionInput"),
			OutputSchema: workflowSchema("CaseDispositionDecisionResult"),
			Inputs: []workflow.Field{
				{Path: "case_active", Type: boolean()},
				{Path: "participant_authorized", Type: boolean()},
				{Path: "investigator_conflict", Type: boolean()},
				{Path: "matter_wall_active", Type: boolean()},
				{Path: "legal_hold_active", Type: boolean()},
				{Path: "retaliation_signal_detected", Type: boolean()},
				{Path: "appeal_requested", Type: boolean()},
				{Path: "case_already_disposed", Type: boolean()},
			},
			Outputs: []workflow.Field{{Path: "route_key", Type: plainStr()}},
			InputMappings: []workflow.Mapping{
				{Target: "case_active", Source: builders.FromNode(NodeReadCaseAssignment, "case_active")},
				{Target: "participant_authorized", Source: builders.FromNode(NodeReadParticipantAuthz, "participant_authorized")},
				{Target: "investigator_conflict", Source: builders.FromNode(NodeComputeCaseFootprint, "investigator_conflict")},
				{Target: "matter_wall_active", Source: builders.FromNode(NodeResolveMatterWallAndHold, "matter_wall_active")},
				{Target: "legal_hold_active", Source: builders.FromNode(NodeResolveMatterWallAndHold, "legal_hold_active")},
				{Target: "retaliation_signal_detected", Source: builders.FromNode(NodeReadRetaliationSignal, "retaliation_signal_detected")},
				{Target: "appeal_requested", Source: builders.FromInput("appeal_requested")},
				{Target: "case_already_disposed", Source: builders.FromInput("case_already_disposed")},
			},
			Decision: &workflow.DecisionSpec{
				EvaluatorRef: "engines.rules.hrcase_case_disposition", EvaluatorVersion: 1,
				RuleRef: RuleCaseDisposition, InputDigestProfile: mappingDigest,
				Routes: []workflow.DecisionRoute{
					{Key: RouteAppealRequiresReopen, Predicate: "appeal_requested_after_case_already_closed", Precedence: 10},
					{Key: RouteCaseAlreadyDisposedInvalid, Predicate: "case_already_closed_without_appeal", Precedence: 20},
					{Key: RouteUnauthorizedParticipant, Predicate: "participant_not_authorized", Precedence: 30},
					{Key: RouteInvestigatorConflictBlocked, Predicate: "investigator_equals_subject", Precedence: 40},
					{Key: RouteMatterWallBreachBlocked, Predicate: "matter_wall_active", Precedence: 50},
					{Key: RouteLegalHoldRaceBlocked, Predicate: "legal_hold_active", Precedence: 60},
					{Key: RouteRetaliationEscalation, Predicate: "retaliation_signal_detected", Precedence: 70},
					{Key: RouteRoutineDispositionRequired, Predicate: "no_blocking_condition", Precedence: 80},
				},
				DefaultRoute: RouteRoutineDispositionRequired,
			},
			Governance: workflow.NodeGovernance{
				Purpose: purpose, Classification: classification,
				RevalidationBoundary: workflow.RevalidateNone, DataAccessManifestRef: dataAccessManifest,
			},
		},
		{
			ID:           NodeObserveRecordCustody,
			Type:         workflow.StepObserve,
			InputSchema:  bootstrapCapabilitySchema(CapObserveRecordCustody, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapObserveRecordCustody, "response"),
			Inputs: []workflow.Field{
				{Path: "case_id", Type: str("CaseID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "custody_state", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "case_id", Source: builders.FromInput("case_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapObserveRecordCustody, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:hrcase.records.read"},
			},
			Observe: &workflow.ObserveSpec{
				EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "hrcase.record.projection",
				ExpectedStateFields: []string{"case_id"}, RequiredWatermarks: []string{"hrcase.record.stream_head"},
				MaxAgeSeconds: 300, ComparisonProfile: "comparison.hrcase.record_custody/v1",
				RetryExhaustionRoute: NodeEndDegradedRepair,
			},
			Retry:      &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.observation.bounded/v1"},
			Governance: governedInvocation(nil, nil),
		},
		{
			// A distinct node id from NodeObserveRecordCustody: the graph's
			// edges are keyed by (from-node, outcome route), so a PASS from
			// the routine path and a PASS from the retaliation-escalation
			// path must be able to reach two different terminals. Both nodes
			// bind the same capability and the same injected read port
			// ([Reads]); only the terminal they lead to differs.
			ID:           NodeObserveRecordCustodyEscalat,
			Type:         workflow.StepObserve,
			InputSchema:  bootstrapCapabilitySchema(CapObserveRecordCustody, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapObserveRecordCustody, "response"),
			Inputs: []workflow.Field{
				{Path: "case_id", Type: str("CaseID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "custody_state", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "case_id", Source: builders.FromInput("case_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapObserveRecordCustody, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:hrcase.records.read"},
			},
			Observe: &workflow.ObserveSpec{
				EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "hrcase.record.projection",
				ExpectedStateFields: []string{"case_id"}, RequiredWatermarks: []string{"hrcase.record.stream_head"},
				MaxAgeSeconds: 300, ComparisonProfile: "comparison.hrcase.record_custody/v1",
				RetryExhaustionRoute: NodeEndDegradedRepair,
			},
			Retry:      &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.observation.bounded/v1"},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:            NodeEndPendingDisposition,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("case_id", "CaseID"),
			InputMappings: builders.TerminalMappings("case_id", "HRCASE_SIMULATION_PENDING_DISPOSITION"),
			End: &workflow.EndSpec{
				TerminalCode: "HRCASE_SIMULATION_PENDING_DISPOSITION", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "PENDING_OBSERVATION", "PENDING"),
				OutstandingObligationRefs: []string{
					ObligationEvidenceCustody, ObligationSLATracking, ObligationFindingRecord, ObligationRecordsRetention,
				},
			},
			Governance: terminalGovernance(
				[]string{ObligationEvidenceCustody, ObligationSLATracking, ObligationFindingRecord, ObligationRecordsRetention},
				[]string{ApprovalHRCaseManager, ApprovalEmployeeRelations, ApprovalLegal},
			),
		},
		{
			ID:            NodeEndRetaliationEscalationPending,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("case_id", "CaseID"),
			InputMappings: builders.TerminalMappings("case_id", "HRCASE_RETALIATION_ESCALATION_PENDING"),
			End: &workflow.EndSpec{
				TerminalCode: "HRCASE_RETALIATION_ESCALATION_PENDING", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "PENDING_OBSERVATION", "PENDING"),
				OutstandingObligationRefs: []string{
					ObligationEvidenceCustody, ObligationSLATracking, ObligationFindingRecord,
					ObligationRecordsRetention, ObligationRetaliationEscalat,
				},
			},
			Governance: terminalGovernance(
				[]string{ObligationEvidenceCustody, ObligationSLATracking, ObligationFindingRecord, ObligationRecordsRetention, ObligationRetaliationEscalat},
				[]string{ApprovalHRCaseManager, ApprovalEmployeeRelations, ApprovalLegal, ApprovalEmployeeRelationsEscalat},
			),
		},
		{
			ID:            NodeEndUnauthorizedParticipant,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("case_id", "CaseID"),
			InputMappings: builders.TerminalMappings("case_id", "HRCASE_UNAUTHORIZED_PARTICIPANT_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "HRCASE_UNAUTHORIZED_PARTICIPANT_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping:         builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "PENDING"),
				OutstandingObligationRefs: []string{ObligationRecordsRetention},
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndInvestigatorConflictBlocked,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("case_id", "CaseID"),
			InputMappings: builders.TerminalMappings("case_id", "HRCASE_INVESTIGATOR_CONFLICT_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "HRCASE_INVESTIGATOR_CONFLICT_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping:         builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "PENDING"),
				OutstandingObligationRefs: []string{ObligationRecordsRetention},
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndMatterWallBreachBlocked,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("case_id", "CaseID"),
			InputMappings: builders.TerminalMappings("case_id", "HRCASE_MATTER_WALL_BREACH_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "HRCASE_MATTER_WALL_BREACH_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping:         builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "PENDING"),
				OutstandingObligationRefs: []string{ObligationRecordsRetention},
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndLegalHoldRaceBlocked,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("case_id", "CaseID"),
			InputMappings: builders.TerminalMappings("case_id", "HRCASE_LEGAL_HOLD_RACE_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "HRCASE_LEGAL_HOLD_RACE_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping:         builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "PENDING"),
				OutstandingObligationRefs: []string{ObligationRecordsRetention},
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndAlreadyDisposedInvalid,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("case_id", "CaseID"),
			InputMappings: builders.TerminalMappings("case_id", "HRCASE_ALREADY_DISPOSED_INVALID"),
			End: &workflow.EndSpec{
				TerminalCode: "HRCASE_ALREADY_DISPOSED_INVALID", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping:         builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "PENDING"),
				OutstandingObligationRefs: []string{ObligationRecordsRetention},
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndAppealRequiresReopen,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("case_id", "CaseID"),
			InputMappings: builders.TerminalMappings("case_id", "HRCASE_APPEAL_REQUIRES_REOPEN"),
			End: &workflow.EndSpec{
				TerminalCode: "HRCASE_APPEAL_REQUIRES_REOPEN", RuntimeStatus: workflow.RuntimeCompleted,
				// An appeal reaching an already-disposed case never mutates
				// the original disposition's history: the original request is
				// superseded, and what must be handled is a distinct reopen
				// intent (the REFACTOR clause), never a resurrected
				// ChangeRequest against this one.
				CompletionMapping:         builders.Completion("SUPERSEDED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "PENDING"),
				OutstandingObligationRefs: []string{ObligationRecordsRetention},
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndDegradedRepair,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("case_id", "CaseID"),
			InputMappings: builders.TerminalMappings("case_id", "HRCASE_SIMULATION_DEGRADED"),
			End: &workflow.EndSpec{
				TerminalCode: "HRCASE_SIMULATION_DEGRADED", RuntimeStatus: workflow.RuntimeBlocked,
				CompletionMapping:         builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{ObligationEvidenceCustody, ObligationRecordsRetention},
				RepairRefs:                []string{repairRef},
			},
			Governance: terminalGovernance([]string{ObligationEvidenceCustody, ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndUnknown,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("case_id", "CaseID"),
			InputMappings: builders.TerminalMappings("case_id", "HRCASE_SIMULATION_UNKNOWN"),
			End: &workflow.EndSpec{
				TerminalCode: "HRCASE_SIMULATION_UNKNOWN", RuntimeStatus: workflow.RuntimeBlocked,
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
	out := capabilityRoutes(NodeReadCaseAssignment, NodeReadParticipantAuthz)
	out = append(out, capabilityRoutes(NodeReadParticipantAuthz, NodeReadRetaliationSignal)...)
	out = append(out, capabilityRoutes(NodeReadRetaliationSignal, NodeResolveMatterWallAndHold)...)
	out = append(out, capabilityRoutes(NodeResolveMatterWallAndHold, NodeComputeCaseFootprint)...)
	out = append(out,
		workflow.Edge{From: NodeComputeCaseFootprint, To: NodeBuildDispositionProposal, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeComputeCaseFootprint, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)},

		workflow.Edge{From: NodeBuildDispositionProposal, To: NodeCaseDecision, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeBuildDispositionProposal, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)},

		workflow.Edge{From: NodeCaseDecision, To: NodeEndAppealRequiresReopen, RouteKey: RouteAppealRequiresReopen},
		workflow.Edge{From: NodeCaseDecision, To: NodeEndAlreadyDisposedInvalid, RouteKey: RouteCaseAlreadyDisposedInvalid},
		workflow.Edge{From: NodeCaseDecision, To: NodeEndUnauthorizedParticipant, RouteKey: RouteUnauthorizedParticipant},
		workflow.Edge{From: NodeCaseDecision, To: NodeEndInvestigatorConflictBlocked, RouteKey: RouteInvestigatorConflictBlocked},
		workflow.Edge{From: NodeCaseDecision, To: NodeEndMatterWallBreachBlocked, RouteKey: RouteMatterWallBreachBlocked},
		workflow.Edge{From: NodeCaseDecision, To: NodeEndLegalHoldRaceBlocked, RouteKey: RouteLegalHoldRaceBlocked},
		workflow.Edge{From: NodeCaseDecision, To: NodeObserveRecordCustodyEscalat, RouteKey: RouteRetaliationEscalation},
		workflow.Edge{From: NodeCaseDecision, To: NodeObserveRecordCustody, RouteKey: RouteRoutineDispositionRequired},
		workflow.Edge{From: NodeCaseDecision, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},

		workflow.Edge{From: NodeObserveRecordCustody, To: NodeEndPendingDisposition, RouteKey: string(workflow.OutcomePass)},
		workflow.Edge{From: NodeObserveRecordCustody, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomeFail)},
		workflow.Edge{From: NodeObserveRecordCustody, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomePartial)},
		workflow.Edge{From: NodeObserveRecordCustody, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},

		workflow.Edge{From: NodeObserveRecordCustodyEscalat, To: NodeEndRetaliationEscalationPending, RouteKey: string(workflow.OutcomePass)},
		workflow.Edge{From: NodeObserveRecordCustodyEscalat, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomeFail)},
		workflow.Edge{From: NodeObserveRecordCustodyEscalat, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomePartial)},
		workflow.Edge{From: NodeObserveRecordCustodyEscalat, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},
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
		{ID: CapReadCaseAssignment, Version: 1},
		{ID: CapReadParticipantAuthz, Version: 1},
		{ID: CapReadRetaliationSignal, Version: 1},
		{ID: CapResolveMatterWallAndHold, Version: 1},
		{ID: CapObserveRecordCustody, Version: 1},
	}
}
