// Package learning is CONF-013's proof that a learning/skills/credential
// satisfaction reference workflow compiles under P1A and walks to completion
// through the real SIMULATE-mode interpreter (internal/workflow/simulate),
// in the same spirit CONF-001 established for the promote-into-management
// reference and CONF-003/CONF-005 established for the cross-company transfer
// and termination-and-offboarding references
// (internal/workflow/conformance/{transfer,termination}).
//
// It is a conformance fixture, not a domain implementation: the capability
// handlers behind every CAPABILITY/OBSERVE node are canned, in-memory
// projections this package owns, never a real LMS, credentialing or
// compliance integration. What is under test is the workflow's own
// composition: a duplicate-enrollment block that fires ahead of everything
// else, a waiver-authority exemption that requires a pinned policy context
// (never a silently-defaulted "no waiver applies"), a distinction between a
// verified credential and a bare skill claim that is never allowed to
// collapse (the REFACTOR clause), an evidence-expiry footprint computed once
// and consumed by the routing decision rather than re-derived, and a
// post-decision provider-reconciliation observation whose PASS/FAIL/UNKNOWN
// answer decides between two consistent pending-approval terminals (satisfied
// or expiring) and a bounded repair or unknown route.
package learning

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
)

// Workflow identity.
const (
	WorkflowID = "hcmnext.workflows.learning_credential_satisfaction"
	Version    = 1
)

// Node ids. Exported so the conformance tests can name the exact path a
// golden walk takes without re-deriving it from the definition.
const (
	NodeReadRequirement           = "read_requirement"
	NodeReadEnrollmentHistory     = "read_enrollment_history"
	NodeReadCredentialEvidence    = "read_credential_evidence"
	NodeReadWaiver                = "read_waiver"
	NodeComputeEvidenceFootprint  = "compute_evidence_footprint"
	NodeBuildSatisfactionProposal = "build_satisfaction_proposal"
	NodeSatisfactionDecision      = "satisfaction_decision"

	// NodeObserveProviderReconciliation and
	// NodeObserveProviderReconciliationExpiring are two distinct OBSERVE
	// nodes rather than one, because a single StepObserve node's PASS
	// outcome can only route to one successor: the satisfied path and the
	// expiring path each need their own terminal on PASS (a distinct
	// terminal code per GREEN's SATISFIED/EXPIRING split), while both still
	// share the same degraded-repair and unknown routes on FAIL/PARTIAL/
	// UNKNOWN. Both bind the same capability id and are driven by the same
	// injected read port, so this is a graph-shape choice, not a gap in
	// internal/workflow.
	NodeObserveProviderReconciliation         = "observe_provider_reconciliation"
	NodeObserveProviderReconciliationExpiring = "observe_provider_reconciliation_expiring"

	NodeEndSatisfied                = "end_satisfied"
	NodeEndExpiringSatisfaction     = "end_expiring_satisfaction"
	NodeEndExemptValidWaiver        = "end_exempt_valid_waiver"
	NodeEndUnsatisfiedNoEvidence    = "end_unsatisfied_no_evidence"
	NodeEndUnsatisfiedExpired       = "end_unsatisfied_expired"
	NodeEndUnsatisfiedUnverified    = "end_unsatisfied_unverified_claim"
	NodeEndUnsatisfiedInvalidWaiver = "end_unsatisfied_invalid_waiver"
	NodeEndDuplicateEnrollment      = "end_duplicate_enrollment_blocked"
	NodeEndDegradedRepair           = "end_degraded_repair"
	NodeEndUnknown                  = "end_unknown"
)

// Decision route keys the satisfaction DECISION declares, in the exact
// precedence order CONF-013's GREEN clause names.
const (
	RouteDuplicateEnrollmentBlocked     = "DUPLICATE_ENROLLMENT_BLOCKED"
	RouteExemptValidWaiver              = "EXEMPT_VALID_WAIVER"
	RouteUnsatisfiedInvalidWaiver       = "UNSATISFIED_INVALID_WAIVER"
	RouteUnsatisfiedUnverifiedClaim     = "UNSATISFIED_UNVERIFIED_CLAIM"
	RouteUnsatisfiedExpired             = "UNSATISFIED_EXPIRED"
	RouteExpiringSatisfaction           = "EXPIRING_SATISFACTION"
	RouteUnsatisfiedNoEvidence          = "UNSATISFIED_NO_EVIDENCE"
	RouteSatisfiedPendingReconciliation = "SATISFIED_PENDING_RECONCILIATION"
	RuleSatisfactionDecision            = "rules.learning.satisfaction_decision/v1"
)

// Transform references the two TRANSFORM nodes bind.
const (
	TransformEvidenceFootprint = "transforms.learning.compute_evidence_footprint"
	TransformBuildProposal     = "transforms.learning.build_satisfaction_proposal"
)

// Evidence kinds. Only VERIFIED_CREDENTIAL evidence may ever satisfy a
// requirement; SELF_CLAIM is recorded and reported but never alone
// sufficient (the REFACTOR clause: a skill claim and verified evidence
// remain distinct).
const (
	EvidenceKindVerifiedCredential = "VERIFIED_CREDENTIAL"
	EvidenceKindSelfClaim          = "SELF_CLAIM"
)

// Capability identities. This package's own fixture capabilities, never a
// bootstrap or domain capability: CONF-013 proves the workflow's
// composition, not a real LMS/credentialing integration.
const (
	CapReadRequirement               = "hcmnext.conformance.learning.read_requirement"
	CapReadEnrollmentHistory         = "hcmnext.conformance.learning.read_enrollment_history"
	CapReadCredentialEvidence        = "hcmnext.conformance.learning.read_credential_evidence"
	CapReadWaiver                    = "hcmnext.conformance.learning.read_waiver"
	CapObserveProviderReconciliation = "hcmnext.conformance.learning.observe_provider_reconciliation"
)

// Approval requirement ids, declared on the pending SATISFIED/EXPIRING/EXEMPT
// terminals.
const (
	ApprovalLnDPartner        = "approval.learning.lnd_partner"
	ApprovalComplianceOfficer = "approval.learning.compliance_officer"
)

// Obligation ids. ObligationEvidenceKindRecorded is the REFACTOR obligation:
// it requires the evidence kind (verified vs self-claim) be recorded
// distinctly from the satisfaction outcome itself, never folded into it.
const (
	ObligationEvidenceKindRecorded   = "obligation.learning.evidence_kind_recorded"
	ObligationProviderReconciliation = "obligation.learning.provider_reconciliation_retained"
	ObligationEvidenceRetained       = "obligation.learning.simulation_evidence_retained"
	ObligationRenewalReminder        = "obligation.learning.renewal_reminder"
)

const (
	organizationScope  = "acme/talent"
	purpose            = "SIMULATE_LEARNING_CREDENTIAL_SATISFACTION"
	classification     = "CONFIDENTIAL_HR"
	dataAccessManifest = "dam.learning.simulation/v1"
	mappingDigest      = "hcmnext.workflow.InputMappingSet/v1"
	// repairRef is the bounded repair artifact the degraded terminal links,
	// which is how a stalled provider-reconciliation observation creates
	// tracked repair evidence rather than a silently dropped observation.
	repairRef = "repair.learning.provider_reconciliation_drift/v1"
	// expiringWindowDays is the fixed window compute_evidence_footprint
	// uses to decide "expiring soon": evidence whose expiry falls within
	// this many days of the effective date, but has not yet expired.
	expiringWindowDays = 30
)

// ---- small typed-value helpers, mirrored from internal/workflow/promotion.go
// and internal/workflow/conformance/{transfer,termination} so this package
// never has to reach into another package's unexported helpers.

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

// ReferenceDefinition returns the P1A learning/skills/credential-satisfaction
// reference workflow as typed Go values.
//
// It composes exactly what CONF-013's GREEN clause names: the requirement
// and enrollment history are read, credential evidence and any waiver are
// read (the waiver behind a pinned PolicyContext requirement, never a live
// re-read), an evidence-expiry footprint is computed once, a proposal is
// bound, a satisfaction DECISION routes a duplicate enrollment, a waiver
// (valid or invalid), an unverified skill claim, expired or expiring
// verified-credential evidence, or absent evidence to its own distinct
// terminal, and a post-decision provider-reconciliation observation decides
// between a consistent pending-approval completion and a bounded repair or
// unknown route.
func ReferenceDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        WorkflowID,
		Version:           Version,
		Name:              "Learning credential satisfaction (simulation)",
		InputSchema:       workflowSchema("LearningCredentialSatisfactionInput"),
		OutputSchema:      workflowSchema("LearningCredentialSatisfactionResult"),
		VariablesSchema:   workflowSchema("LearningCredentialSatisfactionVariables"),
		TenantScope:       "acme",
		OrganizationScope: organizationScope,
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeSimulate},
		TerminalProfile:   workflow.TerminalProfileSimulateOnly,
		StartNodeID:       NodeReadRequirement,

		Inputs: []workflow.Field{
			{Path: "worker_id", Type: str("WorkerID")},
			{Path: "requirement_id", Type: str("RequirementID")},
			{Path: "effective_date", Type: localDate()},
		},
		Outputs: []workflow.Field{
			{Path: "worker_id", Type: str("WorkerID")},
			{Path: "terminal_code", Type: plainStr()},
		},

		Limits: workflow.Limits{MaxFanOut: 9, MaxDepth: 14, MaxNodes: 24},

		FailurePolicyRef:      "policy.workflow.failure.simulation/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.simulation/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.hr-simulation/v1",

		ApprovalRequirements: []workflow.ApprovalRequirement{
			{ID: ApprovalLnDPartner, ResolverExpression: "LnDPartnerFor(requirement)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalComplianceOfficer, ResolverExpression: "ComplianceOfficerFor(requirement)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
		},

		Obligations: []workflow.ObligationRequirement{
			{
				ID: ObligationEvidenceKindRecorded, Authority: "customer.policy.learning.evidence_integrity",
				InsertionPoint:   workflow.InsertSimulation,
				RequiredAction:   "Record whether satisfaction evidence is a verified credential or a self-reported claim, distinct from the satisfaction outcome itself",
				ResponsibleParty: "learning.compliance", SatisfactionCondition: "proposal digest cites the evidence kind separately from the route outcome",
				SourceVersion: RuleSatisfactionDecision, ReevaluationPolicy: workflow.ReevalReevaluate, Mandatory: true,
			},
			{
				ID: ObligationProviderReconciliation, Authority: "lms.completion.projection",
				InsertionPoint:   workflow.InsertSimulation,
				RequiredAction:   "Retain the provider-reconciliation observation record",
				ResponsibleParty: "learning.provider_reconciliation", SatisfactionCondition: "reconciliation watermark and outcome recorded against the proposal digest",
				SourceVersion: "lms.completion.projection/v1", ReevaluationPolicy: workflow.ReevalRequireReview,
			},
			{
				ID: ObligationEvidenceRetained, Authority: "records.retention",
				InsertionPoint:   workflow.InsertClosure,
				RequiredAction:   "Retain the simulation artifact and its evidence references",
				ResponsibleParty: "operations.records", SatisfactionCondition: "simulation artifact digest recorded with the terminal result",
				SourceVersion: "records.retention.hr-simulation/v1", ReevaluationPolicy: workflow.ReevalPin, Mandatory: true,
			},
			{
				ID: ObligationRenewalReminder, Authority: "customer.policy.learning.renewal",
				InsertionPoint:   workflow.InsertSimulation,
				RequiredAction:   "Schedule a renewal reminder before the credential's expiry date",
				ResponsibleParty: "learning.renewal_reminder", SatisfactionCondition: "renewal reminder intent recorded against the evidence expiry date",
				SourceVersion: "customer.policy.learning.renewal/v1", ReevaluationPolicy: workflow.ReevalReevaluate,
			},
		},

		Nodes: nodes(),
		Edges: edges(),
	}
}

func nodes() []workflow.Node {
	return []workflow.Node{
		{
			ID:           NodeReadRequirement,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapReadRequirement, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapReadRequirement, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "requirement_id", Type: str("RequirementID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "requirement_active", Type: boolean()},
				{Path: "requirement_authority_scope", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "requirement_id", Source: builders.FromInput("requirement_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapReadRequirement, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:learning.requirement.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeReadEnrollmentHistory,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapReadEnrollmentHistory, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapReadEnrollmentHistory, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "requirement_id", Type: str("RequirementID")},
			},
			Outputs: []workflow.Field{
				{Path: "duplicate_enrollment_detected", Type: boolean()},
				{Path: "enrollment_provider", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "requirement_id", Source: builders.FromInput("requirement_id")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapReadEnrollmentHistory, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:learning.enrollment.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeReadCredentialEvidence,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapReadCredentialEvidence, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapReadCredentialEvidence, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "requirement_id", Type: str("RequirementID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				// evidence_present is false both when no evidence was ever
				// submitted and when the evidence document exists but its
				// content is inaccessible: an inaccessible document must
				// resolve identically to no evidence, never a silent pass.
				{Path: "evidence_present", Type: boolean()},
				{Path: "evidence_expiry_date", Type: localDate()},
				// evidence_kind is VERIFIED_CREDENTIAL or SELF_CLAIM. Only
				// VERIFIED_CREDENTIAL evidence may ever satisfy the
				// requirement (the REFACTOR clause).
				{Path: "evidence_kind", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "requirement_id", Source: builders.FromInput("requirement_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapReadCredentialEvidence, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:learning.evidence.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeReadWaiver,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapReadWaiver, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapReadWaiver, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "requirement_id", Type: str("RequirementID")},
			},
			Outputs: []workflow.Field{
				{Path: "waiver_present", Type: boolean()},
				{Path: "waiver_valid", Type: boolean()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "requirement_id", Source: builders.FromInput("requirement_id")},
			},
			// A waiver's authority is never resolved from a live read: it is
			// pinned to a policy context snapshot, mirroring transfer's and
			// termination's LegalContext requirement. Omitting it means
			// UNKNOWN, never a silently-defaulted "no waiver applies".
			RequiredContext: []workflow.ContextRequirement{{
				Kind:                  "PolicyContext",
				FieldPaths:            []string{"waiver_authority_scope", "policy_version"},
				Purpose:               purpose,
				MaximumClassification: classification,
				MaxAgeSeconds:         3600,
				RequiredWatermarks:    []string{"legal.policy.version"},
				Pinned:                true,
				MissingBehavior:       workflow.MissingUnknown,
			}},
			Capability: &workflow.CapabilityRef{
				ID: CapReadWaiver, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:learning.waiver.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeComputeEvidenceFootprint,
			Type:         workflow.StepTransform,
			InputSchema:  workflowSchema("LearningEvidenceFootprintDraft"),
			OutputSchema: workflowSchema("LearningEvidenceFootprint"),
			Inputs: []workflow.Field{
				{Path: "evidence_present", Type: boolean()},
				{Path: "evidence_expiry_date", Type: localDate()},
				{Path: "effective_date", Type: localDate()},
				{Path: "evidence_kind", Type: plainStr()},
				{Path: "requirement_active", Type: boolean()},
			},
			Outputs: []workflow.Field{
				{Path: "evidence_expired", Type: boolean()},
				{Path: "evidence_expiring_soon", Type: boolean()},
				{Path: "footprint_digest", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "evidence_present", Source: builders.FromNode(NodeReadCredentialEvidence, "evidence_present")},
				{Target: "evidence_expiry_date", Source: builders.FromNode(NodeReadCredentialEvidence, "evidence_expiry_date")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
				{Target: "evidence_kind", Source: builders.FromNode(NodeReadCredentialEvidence, "evidence_kind")},
				{Target: "requirement_active", Source: builders.FromNode(NodeReadRequirement, "requirement_active")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformEvidenceFootprint, Version: 1,
				NormalizationProfile: "hcmnext.canonical.learning_evidence_footprint/v1",
				InputTaint: []workflow.TaintInput{
					{Source: "evidence_expiry_date", Level: workflow.TaintTrusted},
					{Source: "evidence_kind", Level: workflow.TaintTrusted},
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
			ID:           NodeBuildSatisfactionProposal,
			Type:         workflow.StepTransform,
			InputSchema:  workflowSchema("LearningSatisfactionProposalDraft"),
			OutputSchema: workflowSchema("LearningSatisfactionProposal"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "requirement_id", Type: str("RequirementID")},
				{Path: "footprint_digest", Type: plainStr()},
				{Path: "waiver_valid", Type: boolean()},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "proposal_digest", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "requirement_id", Source: builders.FromInput("requirement_id")},
				{Target: "footprint_digest", Source: builders.FromNode(NodeComputeEvidenceFootprint, "footprint_digest")},
				{Target: "waiver_valid", Source: builders.FromNode(NodeReadWaiver, "waiver_valid")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformBuildProposal, Version: 1,
				NormalizationProfile: "hcmnext.canonical.learning_satisfaction_proposal/v1",
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
			ID:           NodeSatisfactionDecision,
			Type:         workflow.StepDecision,
			InputSchema:  workflowSchema("LearningSatisfactionDecisionInput"),
			OutputSchema: workflowSchema("LearningSatisfactionDecisionResult"),
			Inputs: []workflow.Field{
				{Path: "requirement_active", Type: boolean()},
				{Path: "duplicate_enrollment_detected", Type: boolean()},
				{Path: "evidence_present", Type: boolean()},
				{Path: "evidence_expired", Type: boolean()},
				{Path: "evidence_expiring_soon", Type: boolean()},
				{Path: "evidence_kind", Type: plainStr()},
				{Path: "waiver_present", Type: boolean()},
				{Path: "waiver_valid", Type: boolean()},
			},
			Outputs: []workflow.Field{{Path: "route_key", Type: plainStr()}},
			InputMappings: []workflow.Mapping{
				{Target: "requirement_active", Source: builders.FromNode(NodeReadRequirement, "requirement_active")},
				{Target: "duplicate_enrollment_detected", Source: builders.FromNode(NodeReadEnrollmentHistory, "duplicate_enrollment_detected")},
				{Target: "evidence_present", Source: builders.FromNode(NodeReadCredentialEvidence, "evidence_present")},
				{Target: "evidence_expired", Source: builders.FromNode(NodeComputeEvidenceFootprint, "evidence_expired")},
				{Target: "evidence_expiring_soon", Source: builders.FromNode(NodeComputeEvidenceFootprint, "evidence_expiring_soon")},
				{Target: "evidence_kind", Source: builders.FromNode(NodeReadCredentialEvidence, "evidence_kind")},
				{Target: "waiver_present", Source: builders.FromNode(NodeReadWaiver, "waiver_present")},
				{Target: "waiver_valid", Source: builders.FromNode(NodeReadWaiver, "waiver_valid")},
			},
			Decision: &workflow.DecisionSpec{
				EvaluatorRef: "engines.rules.learning_satisfaction", EvaluatorVersion: 1,
				RuleRef: RuleSatisfactionDecision, InputDigestProfile: mappingDigest,
				Routes: []workflow.DecisionRoute{
					{Key: RouteDuplicateEnrollmentBlocked, Predicate: "duplicate_enrollment_detected", Precedence: 10},
					{Key: RouteExemptValidWaiver, Predicate: "waiver_present_and_valid", Precedence: 20},
					{Key: RouteUnsatisfiedInvalidWaiver, Predicate: "waiver_present_and_invalid", Precedence: 30},
					{Key: RouteUnsatisfiedUnverifiedClaim, Predicate: "evidence_present_and_self_claim", Precedence: 40},
					{Key: RouteUnsatisfiedExpired, Predicate: "verified_credential_evidence_expired", Precedence: 50},
					{Key: RouteExpiringSatisfaction, Predicate: "verified_credential_evidence_expiring_soon", Precedence: 60},
					{Key: RouteUnsatisfiedNoEvidence, Predicate: "evidence_absent", Precedence: 70},
					{Key: RouteSatisfiedPendingReconciliation, Predicate: "verified_credential_evidence_satisfied", Precedence: 80},
				},
				DefaultRoute: RouteSatisfiedPendingReconciliation,
			},
			Governance: workflow.NodeGovernance{
				Purpose: purpose, Classification: classification,
				RevalidationBoundary: workflow.RevalidateNone, DataAccessManifestRef: dataAccessManifest,
			},
		},
		{
			ID:           NodeObserveProviderReconciliation,
			Type:         workflow.StepObserve,
			InputSchema:  bootstrapCapabilitySchema(CapObserveProviderReconciliation, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapObserveProviderReconciliation, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "requirement_id", Type: str("RequirementID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "reconciliation_state", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "requirement_id", Source: builders.FromInput("requirement_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapObserveProviderReconciliation, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:learning.provider.read"},
			},
			Observe: &workflow.ObserveSpec{
				EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "lms.completion.projection",
				ExpectedStateFields: []string{"requirement_id"}, RequiredWatermarks: []string{"lms.completion.projection.stream_head"},
				MaxAgeSeconds: 300, ComparisonProfile: "comparison.learning.provider_reconciliation/v1",
				RetryExhaustionRoute: NodeEndDegradedRepair,
			},
			Retry:      &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.observation.bounded/v1"},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeObserveProviderReconciliationExpiring,
			Type:         workflow.StepObserve,
			InputSchema:  bootstrapCapabilitySchema(CapObserveProviderReconciliation, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapObserveProviderReconciliation, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "requirement_id", Type: str("RequirementID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "reconciliation_state", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "requirement_id", Source: builders.FromInput("requirement_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapObserveProviderReconciliation, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:learning.provider.read"},
			},
			Observe: &workflow.ObserveSpec{
				EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "lms.completion.projection",
				ExpectedStateFields: []string{"requirement_id"}, RequiredWatermarks: []string{"lms.completion.projection.stream_head"},
				MaxAgeSeconds: 300, ComparisonProfile: "comparison.learning.provider_reconciliation/v1",
				RetryExhaustionRoute: NodeEndDegradedRepair,
			},
			Retry:      &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.observation.bounded/v1"},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:            NodeEndSatisfied,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "LEARNING_SATISFIED_PENDING_RECONCILIATION"),
			End: &workflow.EndSpec{
				TerminalCode: "LEARNING_SATISFIED_PENDING_RECONCILIATION", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping:         builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "PENDING_OBSERVATION", "PENDING"),
				OutstandingObligationRefs: []string{ObligationEvidenceKindRecorded, ObligationProviderReconciliation, ObligationEvidenceRetained},
			},
			Governance: terminalGovernance(
				[]string{ObligationEvidenceKindRecorded, ObligationProviderReconciliation, ObligationEvidenceRetained},
				[]string{ApprovalLnDPartner, ApprovalComplianceOfficer},
			),
		},
		{
			ID:            NodeEndExpiringSatisfaction,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "LEARNING_EXPIRING_PENDING_RECONCILIATION"),
			End: &workflow.EndSpec{
				TerminalCode: "LEARNING_EXPIRING_PENDING_RECONCILIATION", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "PENDING_OBSERVATION", "PENDING"),
				OutstandingObligationRefs: []string{
					ObligationEvidenceKindRecorded, ObligationProviderReconciliation, ObligationEvidenceRetained, ObligationRenewalReminder,
				},
			},
			Governance: terminalGovernance(
				[]string{ObligationEvidenceKindRecorded, ObligationProviderReconciliation, ObligationEvidenceRetained, ObligationRenewalReminder},
				[]string{ApprovalLnDPartner, ApprovalComplianceOfficer},
			),
		},
		{
			ID:            NodeEndExemptValidWaiver,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "LEARNING_EXEMPT_VALID_WAIVER"),
			End: &workflow.EndSpec{
				TerminalCode: "LEARNING_EXEMPT_VALID_WAIVER", RuntimeStatus: workflow.RuntimeCompleted,
				// A valid waiver resolves immediately: no provider
				// observation is needed, but the requirement's own
				// obligations (retention) remain pending until closure.
				CompletionMapping:         builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "PENDING_OBSERVATION", "PENDING"),
				OutstandingObligationRefs: []string{ObligationEvidenceRetained},
			},
			Governance: terminalGovernance([]string{ObligationEvidenceRetained}, []string{ApprovalLnDPartner, ApprovalComplianceOfficer}),
		},
		{
			ID:            NodeEndUnsatisfiedNoEvidence,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "LEARNING_UNSATISFIED_NO_EVIDENCE"),
			End: &workflow.EndSpec{
				TerminalCode: "LEARNING_UNSATISFIED_NO_EVIDENCE", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationEvidenceRetained}, nil),
		},
		{
			ID:            NodeEndUnsatisfiedExpired,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "LEARNING_UNSATISFIED_EXPIRED"),
			End: &workflow.EndSpec{
				TerminalCode: "LEARNING_UNSATISFIED_EXPIRED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationEvidenceRetained}, nil),
		},
		{
			ID:            NodeEndUnsatisfiedUnverified,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "LEARNING_UNSATISFIED_UNVERIFIED_CLAIM"),
			End: &workflow.EndSpec{
				TerminalCode: "LEARNING_UNSATISFIED_UNVERIFIED_CLAIM", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationEvidenceRetained}, nil),
		},
		{
			ID:            NodeEndUnsatisfiedInvalidWaiver,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "LEARNING_UNSATISFIED_INVALID_WAIVER"),
			End: &workflow.EndSpec{
				TerminalCode: "LEARNING_UNSATISFIED_INVALID_WAIVER", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationEvidenceRetained}, nil),
		},
		{
			ID:            NodeEndDuplicateEnrollment,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "LEARNING_DUPLICATE_ENROLLMENT_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "LEARNING_DUPLICATE_ENROLLMENT_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping:         builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "PENDING"),
				OutstandingObligationRefs: []string{ObligationEvidenceRetained},
			},
			Governance: terminalGovernance([]string{ObligationEvidenceRetained}, nil),
		},
		{
			ID:            NodeEndDegradedRepair,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "LEARNING_SIMULATION_DEGRADED"),
			End: &workflow.EndSpec{
				TerminalCode: "LEARNING_SIMULATION_DEGRADED", RuntimeStatus: workflow.RuntimeBlocked,
				CompletionMapping:         builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{ObligationProviderReconciliation, ObligationEvidenceRetained},
				RepairRefs:                []string{repairRef},
			},
			Governance: terminalGovernance([]string{ObligationProviderReconciliation, ObligationEvidenceRetained}, nil),
		},
		{
			ID:            NodeEndUnknown,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "LEARNING_SIMULATION_UNKNOWN"),
			End: &workflow.EndSpec{
				TerminalCode: "LEARNING_SIMULATION_UNKNOWN", RuntimeStatus: workflow.RuntimeBlocked,
				CompletionMapping:         builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{ObligationEvidenceRetained},
			},
			Governance: terminalGovernance([]string{ObligationEvidenceRetained}, nil),
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
	out := capabilityRoutes(NodeReadRequirement, NodeReadEnrollmentHistory)
	out = append(out, capabilityRoutes(NodeReadEnrollmentHistory, NodeReadCredentialEvidence)...)
	out = append(out, capabilityRoutes(NodeReadCredentialEvidence, NodeReadWaiver)...)
	out = append(out, capabilityRoutes(NodeReadWaiver, NodeComputeEvidenceFootprint)...)
	out = append(out,
		workflow.Edge{From: NodeComputeEvidenceFootprint, To: NodeBuildSatisfactionProposal, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeComputeEvidenceFootprint, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)},

		workflow.Edge{From: NodeBuildSatisfactionProposal, To: NodeSatisfactionDecision, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeBuildSatisfactionProposal, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)},

		workflow.Edge{From: NodeSatisfactionDecision, To: NodeEndDuplicateEnrollment, RouteKey: RouteDuplicateEnrollmentBlocked},
		workflow.Edge{From: NodeSatisfactionDecision, To: NodeEndExemptValidWaiver, RouteKey: RouteExemptValidWaiver},
		workflow.Edge{From: NodeSatisfactionDecision, To: NodeEndUnsatisfiedInvalidWaiver, RouteKey: RouteUnsatisfiedInvalidWaiver},
		workflow.Edge{From: NodeSatisfactionDecision, To: NodeEndUnsatisfiedUnverified, RouteKey: RouteUnsatisfiedUnverifiedClaim},
		workflow.Edge{From: NodeSatisfactionDecision, To: NodeEndUnsatisfiedExpired, RouteKey: RouteUnsatisfiedExpired},
		workflow.Edge{From: NodeSatisfactionDecision, To: NodeObserveProviderReconciliationExpiring, RouteKey: RouteExpiringSatisfaction},
		workflow.Edge{From: NodeSatisfactionDecision, To: NodeEndUnsatisfiedNoEvidence, RouteKey: RouteUnsatisfiedNoEvidence},
		workflow.Edge{From: NodeSatisfactionDecision, To: NodeObserveProviderReconciliation, RouteKey: RouteSatisfiedPendingReconciliation},
		workflow.Edge{From: NodeSatisfactionDecision, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},

		workflow.Edge{From: NodeObserveProviderReconciliation, To: NodeEndSatisfied, RouteKey: string(workflow.OutcomePass)},
		workflow.Edge{From: NodeObserveProviderReconciliation, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomeFail)},
		workflow.Edge{From: NodeObserveProviderReconciliation, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomePartial)},
		workflow.Edge{From: NodeObserveProviderReconciliation, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},

		workflow.Edge{From: NodeObserveProviderReconciliationExpiring, To: NodeEndExpiringSatisfaction, RouteKey: string(workflow.OutcomePass)},
		workflow.Edge{From: NodeObserveProviderReconciliationExpiring, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomeFail)},
		workflow.Edge{From: NodeObserveProviderReconciliationExpiring, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomePartial)},
		workflow.Edge{From: NodeObserveProviderReconciliationExpiring, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},
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
		{ID: CapReadRequirement, Version: 1},
		{ID: CapReadEnrollmentHistory, Version: 1},
		{ID: CapReadCredentialEvidence, Version: 1},
		{ID: CapReadWaiver, Version: 1},
		{ID: CapObserveProviderReconciliation, Version: 1},
	}
}
