// Package talent is CONF-012's proof that a talent/performance calibration
// reference workflow compiles under P1A and walks to completion through the
// real SIMULATE-mode interpreter (internal/workflow/simulate), exactly as
// CONF-001 established for the promote-into-management reference, CONF-003
// established for the cross-company transfer reference
// (internal/workflow/conformance/transfer) and CONF-005 established for the
// termination-and-offboarding reference
// (internal/workflow/conformance/termination).
//
// It is a conformance fixture, not a domain implementation: the capability
// handlers behind every CAPABILITY/OBSERVE node are canned, in-memory
// projections this package owns, never a real HRIS, talent-management or
// calibration-engine integration. What is under test is the workflow's own
// composition: a rating claim (HUMAN_OPINION), a model-generated
// recommendation (MODEL_INFERENCE) and a population/authority read (FACT) are
// kept as distinct, separately-tagged provenance; the model recommendation
// never reaches the authoritative DECISION node, only the proposal it feeds
// (the REFACTOR clause: a recommendation is not an authoritative decision); a
// stale population, an unauthorized rating author and a contested calibration
// each block or degrade rather than silently proceeding; and a correction
// never mutates the prior assessment - it always creates a new, distinct
// revision that references the prior digest instead of discarding it.
package talent

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
)

// Workflow identity.
const (
	WorkflowID = "hcmnext.workflows.talent_performance_calibration"
	Version    = 1
)

// Node ids. Exported so the conformance tests can name the exact path a
// golden walk takes without re-deriving it from the definition.
const (
	NodeReadPopulation              = "read_population_snapshot"
	NodeReadRatingClaim             = "read_rating_claim"
	NodeReadModelInference          = "read_model_inference"
	NodeResolveCalibrationCommittee = "resolve_calibration_committee"
	NodeComputeCalibrationFootprint = "compute_calibration_footprint"
	NodeBuildAssessmentProposal     = "build_assessment_proposal"
	NodeCalibrationDecision         = "calibration_and_correction_decision"
	NodeObserveCalibrationRecord    = "observe_calibration_record"

	NodeEndPendingApprovals    = "end_pending_approvals"
	NodeEndStalePopulation     = "end_stale_population_unknown"
	NodeEndUnauthorizedRating  = "end_unauthorized_rating_blocked"
	NodeEndCalibrationConflict = "end_calibration_conflict_blocked"
	NodeEndRevisionCreated     = "end_revision_created"
	NodeEndDegradedRepair      = "end_degraded_repair"
	NodeEndUnknown             = "end_unknown"
)

// Decision route keys the calibration-and-correction DECISION declares.
// Precedence follows the todo's exact ordering: a correction against a prior
// assessment is recognized before any other condition is even meaningful,
// because a correction's own identity (new revision, never a mutation) does
// not depend on whether this particular revision's population, authority or
// calibration checks pass.
const (
	RouteCorrectionRequiresNewRevision = "CORRECTION_REQUIRES_NEW_REVISION"
	RouteStalePopulationUnknown        = "STALE_POPULATION_UNKNOWN"
	RouteUnauthorizedRatingBlocked     = "UNAUTHORIZED_RATING_BLOCKED"
	RouteCalibrationConflictBlocked    = "CALIBRATION_CONFLICT_BLOCKED"
	RouteCalibrationOK                 = "CALIBRATION_OK"
	RuleCalibrationAndCorrection       = "rules.talent.calibration_and_correction/v1"
)

// Transform references the two TRANSFORM nodes bind.
const (
	TransformCalibrationFootprint = "transforms.talent.compute_calibration_footprint"
	TransformBuildProposal        = "transforms.talent.build_assessment_proposal"
)

// Capability identities. This package's own fixture capabilities, never a
// bootstrap or domain capability: CONF-012 proves the workflow's composition,
// not a real Talent/Performance/HRIS integration.
const (
	CapReadPopulation              = "hcmnext.conformance.talent.read_population_snapshot"
	CapReadRatingClaim             = "hcmnext.conformance.talent.read_rating_claim"
	CapReadModelInference          = "hcmnext.conformance.talent.read_model_inference"
	CapResolveCalibrationCommittee = "hcmnext.conformance.talent.resolve_calibration_committee"
	CapObserveCalibrationRecord    = "hcmnext.conformance.talent.observe_calibration_record"
)

// Approval requirement ids.
const (
	ApprovalCalibrationCommittee = "approval.talent.calibration_committee"
	ApprovalHRBP                 = "approval.talent.hrbp"
	ApprovalRewardsPartner       = "approval.talent.rewards_partner"
)

// Obligation ids. ProvenanceRetention is what makes the GREEN clause's
// "distinguishes FACT/HUMAN_OPINION/MODEL_INFERENCE" checkable: it is a
// distinct, mandatory obligation naming exactly that requirement.
const (
	ObligationProvenanceRetention      = "obligation.talent.provenance_retention"
	ObligationCalibrationRecord        = "obligation.talent.calibration_record"
	ObligationEvidenceRetention        = "obligation.talent.simulation_evidence_retained"
	ObligationPriorAssessmentPreserved = "obligation.talent.prior_assessment_preserved"
)

const (
	organizationScope  = "acme/talent"
	purpose            = "SIMULATE_TALENT_PERFORMANCE_CALIBRATION"
	classification     = "CONFIDENTIAL_HR_PERFORMANCE"
	dataAccessManifest = "dam.talent.simulation/v1"
	mappingDigest      = "hcmnext.workflow.InputMappingSet/v1"
	// repairRef is the bounded repair artifact the degraded terminal links,
	// which is how a stalled calibration-record observation creates tracked
	// repair evidence rather than a silently dropped observation.
	repairRef = "repair.talent.calibration_record_drift/v1"
)

// ---- small typed-value helpers, mirrored verbatim from
// internal/workflow/promotion.go, internal/workflow/conformance/transfer and
// internal/workflow/conformance/termination so this package never has to
// reach into another package's unexported helpers.

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

// ReferenceDefinition returns the P1A talent/performance calibration
// reference workflow as typed Go values.
//
// It composes exactly what CONF-012's GREEN clause names: a population
// snapshot (FACT), a human rating claim (HUMAN_OPINION) and a model-generated
// recommendation (MODEL_INFERENCE) are read as three separately tagged
// facts; a calibration committee resolution carries its own contest lineage
// (AGREED/CONTESTED); a footprint transform reports - but never itself
// decides - authority mismatch and calibration conflict; a proposal
// transform binds the model recommendation only for reference (never for the
// DECISION's own inputs - the REFACTOR clause); a calibration-and-correction
// DECISION blocks a stale population, an unauthorized rating author and a
// contested calibration, and routes a correction to a distinct new-revision
// terminal rather than ever mutating the prior assessment; and a
// post-decision observation of the calibration record routes to a bounded
// repair terminal when it degrades.
func ReferenceDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        WorkflowID,
		Version:           Version,
		Name:              "Talent performance calibration (simulation)",
		InputSchema:       workflowSchema("TalentPerformanceCalibrationInput"),
		OutputSchema:      workflowSchema("TalentPerformanceCalibrationResult"),
		VariablesSchema:   workflowSchema("TalentPerformanceCalibrationVariables"),
		TenantScope:       "acme",
		OrganizationScope: organizationScope,
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeSimulate},
		TerminalProfile:   workflow.TerminalProfileSimulateOnly,
		StartNodeID:       NodeReadPopulation,

		Inputs: []workflow.Field{
			{Path: "worker_id", Type: str("WorkerID")},
			{Path: "review_cycle_id", Type: str("ReviewCycleID")},
			{Path: "rating_author_id", Type: str("WorkerID")},
			{Path: "effective_date", Type: localDate()},
			{Path: "is_correction", Type: boolean()},
			{Path: "prior_assessment_digest", Type: plainStr()},
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
			{ID: ApprovalCalibrationCommittee, ResolverExpression: "CalibrationCommitteeFor(review_cycle)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalHRBP, ResolverExpression: "HRBPFor(worker)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalRewardsPartner, ResolverExpression: "RewardsPartnerFor(worker)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
		},

		Obligations: []workflow.ObligationRequirement{
			{
				ID: ObligationProvenanceRetention, Authority: "customer.policy.talent.performance_evidence",
				InsertionPoint:   workflow.InsertSimulation,
				RequiredAction:   "Retain the FACT, HUMAN_OPINION and MODEL_INFERENCE provenance tags together with the assessment, never collapsed into one undifferentiated value",
				ResponsibleParty: "talent.performance.evidence", SatisfactionCondition: "assessment digest cites the population fact, the human rating claim and the model recommendation as distinct, separately tagged provenance",
				SourceVersion: RuleCalibrationAndCorrection, ReevaluationPolicy: workflow.ReevalReevaluate, Mandatory: true,
			},
			{
				ID: ObligationCalibrationRecord, Authority: "talent.calibration.committee",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Record the calibration committee's adjusted rating and its agreed/contested lineage",
				ResponsibleParty: "talent.calibration.committee", SatisfactionCondition: "calibration record digest recorded against the proposal digest",
				SourceVersion: "talent.calibration.record/v1", ReevaluationPolicy: workflow.ReevalRequireReview,
			},
			{
				ID: ObligationEvidenceRetention, Authority: "records.retention",
				InsertionPoint: workflow.InsertClosure, RequiredAction: "Retain the simulation artifact and its evidence references",
				ResponsibleParty: "operations.records", SatisfactionCondition: "simulation artifact digest recorded with the terminal result",
				SourceVersion: "records.retention.hr-simulation/v1", ReevaluationPolicy: workflow.ReevalPin, Mandatory: true,
			},
			{
				ID: ObligationPriorAssessmentPreserved, Authority: "talent.assessment.records",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Preserve the prior assessment digest as addressable and immutable; a correction creates a new, distinct revision and never mutates, overwrites or resurrects the original assessment as itself",
				ResponsibleParty: "talent.assessment.records", SatisfactionCondition: "prior assessment digest remains independently resolvable alongside the new revision",
				SourceVersion: RuleCalibrationAndCorrection, ReevaluationPolicy: workflow.ReevalPin, Mandatory: true,
			},
		},

		Nodes: nodes(),
		Edges: edges(),
	}
}

func nodes() []workflow.Node {
	return []workflow.Node{
		{
			ID:           NodeReadPopulation,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapReadPopulation, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapReadPopulation, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "review_cycle_id", Type: str("ReviewCycleID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "population_active", Type: boolean()},
				{Path: "rater_id", Type: str("WorkerID")},
				{Path: "population_watermark", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "review_cycle_id", Source: builders.FromInput("review_cycle_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapReadPopulation, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:talent.population.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeReadRatingClaim,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapReadRatingClaim, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapReadRatingClaim, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "review_cycle_id", Type: str("ReviewCycleID")},
				{Path: "rating_author_id", Type: str("WorkerID")},
			},
			Outputs: []workflow.Field{
				{Path: "rating_value", Type: plainStr()},
				{Path: "rating_provenance", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "review_cycle_id", Source: builders.FromInput("review_cycle_id")},
				{Target: "rating_author_id", Source: builders.FromInput("rating_author_id")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapReadRatingClaim, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:talent.rating.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			// This node's output (inference_recommendation) must never flow
			// into the DECISION's own InputMappings - see
			// NodeBuildAssessmentProposal, which is the only consumer, and
			// TestTodo_CONF_012_Security's structural subtest, which asserts
			// this absence directly against the compiled definition.
			ID:           NodeReadModelInference,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapReadModelInference, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapReadModelInference, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "review_cycle_id", Type: str("ReviewCycleID")},
			},
			Outputs: []workflow.Field{
				{Path: "inference_recommendation", Type: plainStr()},
				{Path: "inference_provenance", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "review_cycle_id", Source: builders.FromInput("review_cycle_id")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapReadModelInference, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:talent.model_inference.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeResolveCalibrationCommittee,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapResolveCalibrationCommittee, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapResolveCalibrationCommittee, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "review_cycle_id", Type: str("ReviewCycleID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "calibration_adjusted_rating", Type: plainStr()},
				{Path: "calibration_status", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "review_cycle_id", Source: builders.FromInput("review_cycle_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			RequiredContext: []workflow.ContextRequirement{{
				Kind:                  "PolicyContext",
				FieldPaths:            []string{"calibration_policy_version", "committee_roster_version"},
				Purpose:               purpose,
				MaximumClassification: classification,
				MaxAgeSeconds:         3600,
				RequiredWatermarks:    []string{"policy.talent.calibration.version"},
				Pinned:                true,
				MissingBehavior:       workflow.MissingUnknown,
			}},
			Capability: &workflow.CapabilityRef{
				ID: CapResolveCalibrationCommittee, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:talent.calibration.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeComputeCalibrationFootprint,
			Type:         workflow.StepTransform,
			InputSchema:  workflowSchema("CalibrationFootprintDraft"),
			OutputSchema: workflowSchema("CalibrationFootprint"),
			Inputs: []workflow.Field{
				{Path: "population_active", Type: boolean()},
				{Path: "rating_author_id", Type: str("WorkerID")},
				{Path: "rater_id", Type: str("WorkerID")},
				{Path: "rating_value", Type: plainStr()},
				{Path: "calibration_adjusted_rating", Type: plainStr()},
				{Path: "calibration_status", Type: plainStr()},
			},
			Outputs: []workflow.Field{
				{Path: "authority_mismatch", Type: boolean()},
				{Path: "calibration_conflict", Type: boolean()},
				{Path: "footprint_digest", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "population_active", Source: builders.FromNode(NodeReadPopulation, "population_active")},
				{Target: "rating_author_id", Source: builders.FromInput("rating_author_id")},
				{Target: "rater_id", Source: builders.FromNode(NodeReadPopulation, "rater_id")},
				{Target: "rating_value", Source: builders.FromNode(NodeReadRatingClaim, "rating_value")},
				{Target: "calibration_adjusted_rating", Source: builders.FromNode(NodeResolveCalibrationCommittee, "calibration_adjusted_rating")},
				{Target: "calibration_status", Source: builders.FromNode(NodeResolveCalibrationCommittee, "calibration_status")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformCalibrationFootprint, Version: 1,
				NormalizationProfile: "hcmnext.canonical.talent_calibration_footprint/v1",
				InputTaint: []workflow.TaintInput{
					{Source: "rating_author_id", Level: workflow.TaintTrusted},
					{Source: "rater_id", Level: workflow.TaintTrusted},
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
			ID:           NodeBuildAssessmentProposal,
			Type:         workflow.StepTransform,
			InputSchema:  workflowSchema("AssessmentProposalDraft"),
			OutputSchema: workflowSchema("AssessmentProposal"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "review_cycle_id", Type: str("ReviewCycleID")},
				{Path: "footprint_digest", Type: plainStr()},
				{Path: "rating_value", Type: plainStr()},
				{Path: "calibration_adjusted_rating", Type: plainStr()},
				{Path: "inference_recommendation", Type: plainStr()},
				{Path: "effective_date", Type: localDate()},
				{Path: "is_correction", Type: boolean()},
				{Path: "prior_assessment_digest", Type: plainStr()},
			},
			Outputs: []workflow.Field{
				{Path: "proposal_digest", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "review_cycle_id", Source: builders.FromInput("review_cycle_id")},
				{Target: "footprint_digest", Source: builders.FromNode(NodeComputeCalibrationFootprint, "footprint_digest")},
				{Target: "rating_value", Source: builders.FromNode(NodeReadRatingClaim, "rating_value")},
				{Target: "calibration_adjusted_rating", Source: builders.FromNode(NodeResolveCalibrationCommittee, "calibration_adjusted_rating")},
				// The model recommendation flows into the PROPOSAL, never into
				// the DECISION: this is the only InputMappings reference to
				// NodeReadModelInference in the whole definition.
				{Target: "inference_recommendation", Source: builders.FromNode(NodeReadModelInference, "inference_recommendation")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
				{Target: "is_correction", Source: builders.FromInput("is_correction")},
				{Target: "prior_assessment_digest", Source: builders.FromInput("prior_assessment_digest")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformBuildProposal, Version: 1,
				NormalizationProfile: "hcmnext.canonical.talent_assessment_proposal/v1",
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
			ID:           NodeCalibrationDecision,
			Type:         workflow.StepDecision,
			InputSchema:  workflowSchema("CalibrationAndCorrectionDecisionInput"),
			OutputSchema: workflowSchema("CalibrationAndCorrectionDecisionResult"),
			Inputs: []workflow.Field{
				{Path: "population_active", Type: boolean()},
				{Path: "authority_mismatch", Type: boolean()},
				{Path: "calibration_conflict", Type: boolean()},
				{Path: "is_correction", Type: boolean()},
				{Path: "prior_assessment_digest", Type: plainStr()},
			},
			Outputs: []workflow.Field{{Path: "route_key", Type: plainStr()}},
			InputMappings: []workflow.Mapping{
				{Target: "population_active", Source: builders.FromNode(NodeReadPopulation, "population_active")},
				{Target: "authority_mismatch", Source: builders.FromNode(NodeComputeCalibrationFootprint, "authority_mismatch")},
				{Target: "calibration_conflict", Source: builders.FromNode(NodeComputeCalibrationFootprint, "calibration_conflict")},
				{Target: "is_correction", Source: builders.FromInput("is_correction")},
				{Target: "prior_assessment_digest", Source: builders.FromInput("prior_assessment_digest")},
			},
			Decision: &workflow.DecisionSpec{
				EvaluatorRef: "engines.rules.talent_calibration_and_correction", EvaluatorVersion: 1,
				RuleRef: RuleCalibrationAndCorrection, InputDigestProfile: mappingDigest,
				Routes: []workflow.DecisionRoute{
					{Key: RouteCorrectionRequiresNewRevision, Predicate: "is_correction_against_a_prior_assessment_digest", Precedence: 10},
					{Key: RouteStalePopulationUnknown, Predicate: "population_not_active", Precedence: 20},
					{Key: RouteUnauthorizedRatingBlocked, Predicate: "rating_author_is_not_the_rater_of_record", Precedence: 30},
					{Key: RouteCalibrationConflictBlocked, Predicate: "calibration_status_is_contested", Precedence: 40},
					{Key: RouteCalibrationOK, Predicate: "no_blocking_condition", Precedence: 50},
				},
				DefaultRoute: RouteCalibrationOK,
			},
			Governance: workflow.NodeGovernance{
				Purpose: purpose, Classification: classification,
				RevalidationBoundary: workflow.RevalidateNone, DataAccessManifestRef: dataAccessManifest,
			},
		},
		{
			ID:           NodeObserveCalibrationRecord,
			Type:         workflow.StepObserve,
			InputSchema:  bootstrapCapabilitySchema(CapObserveCalibrationRecord, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapObserveCalibrationRecord, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "review_cycle_id", Type: str("ReviewCycleID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "calibration_record_state", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "review_cycle_id", Source: builders.FromInput("review_cycle_id")},
				{Target: "effective_date", Source: builders.FromInput("effective_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapObserveCalibrationRecord, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:talent.calibration.read"},
			},
			Observe: &workflow.ObserveSpec{
				EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "talent.calibration.projection",
				ExpectedStateFields: []string{"review_cycle_id"}, RequiredWatermarks: []string{"talent.calibration.stream_head"},
				MaxAgeSeconds: 300, ComparisonProfile: "comparison.talent.calibration_record/v1",
				RetryExhaustionRoute: NodeEndDegradedRepair,
			},
			Retry:      &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.observation.bounded/v1"},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:            NodeEndPendingApprovals,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TALENT_CALIBRATION_PENDING_APPROVALS"),
			End: &workflow.EndSpec{
				TerminalCode: "TALENT_CALIBRATION_PENDING_APPROVALS", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "PENDING_OBSERVATION", "PENDING"),
				OutstandingObligationRefs: []string{
					ObligationProvenanceRetention, ObligationCalibrationRecord, ObligationEvidenceRetention,
				},
			},
			Governance: terminalGovernance(
				[]string{ObligationProvenanceRetention, ObligationCalibrationRecord, ObligationEvidenceRetention},
				[]string{ApprovalCalibrationCommittee, ApprovalHRBP, ApprovalRewardsPartner},
			),
		},
		{
			ID:            NodeEndStalePopulation,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TALENT_CALIBRATION_STALE_POPULATION_UNKNOWN"),
			End: &workflow.EndSpec{
				TerminalCode: "TALENT_CALIBRATION_STALE_POPULATION_UNKNOWN", RuntimeStatus: workflow.RuntimeBlocked,
				CompletionMapping:         builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{ObligationEvidenceRetention},
			},
			Governance: terminalGovernance([]string{ObligationEvidenceRetention}, nil),
		},
		{
			ID:            NodeEndUnauthorizedRating,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TALENT_CALIBRATION_UNAUTHORIZED_RATING_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "TALENT_CALIBRATION_UNAUTHORIZED_RATING_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationEvidenceRetention}, nil),
		},
		{
			ID:            NodeEndCalibrationConflict,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TALENT_CALIBRATION_CONFLICT_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "TALENT_CALIBRATION_CONFLICT_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationEvidenceRetention}, nil),
		},
		{
			ID:            NodeEndRevisionCreated,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TALENT_CALIBRATION_REVISION_CREATED"),
			End: &workflow.EndSpec{
				TerminalCode: "TALENT_CALIBRATION_REVISION_CREATED", RuntimeStatus: workflow.RuntimeCompleted,
				// A correction never mutates or resurrects the prior
				// assessment as itself: RequestState is the distinct declared
				// value SUPERSEDED (the same value CONF-005's termination
				// reference uses for its own "distinct intent, not a
				// mutation" reinstatement path), never SIMULATED, and the
				// prior assessment digest is named as an outstanding
				// obligation to preserve.
				CompletionMapping:         builders.Completion("SUPERSEDED", "NOT_PLANNED", "NOT_STARTED", "PENDING_OBSERVATION", "PENDING"),
				OutstandingObligationRefs: []string{ObligationPriorAssessmentPreserved, ObligationEvidenceRetention},
			},
			Governance: terminalGovernance([]string{ObligationPriorAssessmentPreserved, ObligationEvidenceRetention}, nil),
		},
		{
			ID:            NodeEndDegradedRepair,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TALENT_CALIBRATION_SIMULATION_DEGRADED"),
			End: &workflow.EndSpec{
				TerminalCode: "TALENT_CALIBRATION_SIMULATION_DEGRADED", RuntimeStatus: workflow.RuntimeBlocked,
				CompletionMapping:         builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{ObligationCalibrationRecord, ObligationEvidenceRetention},
				RepairRefs:                []string{repairRef},
			},
			Governance: terminalGovernance([]string{ObligationCalibrationRecord, ObligationEvidenceRetention}, nil),
		},
		{
			ID:            NodeEndUnknown,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("worker_id", "WorkerID"),
			InputMappings: builders.TerminalMappings("worker_id", "TALENT_CALIBRATION_SIMULATION_UNKNOWN"),
			End: &workflow.EndSpec{
				TerminalCode: "TALENT_CALIBRATION_SIMULATION_UNKNOWN", RuntimeStatus: workflow.RuntimeBlocked,
				CompletionMapping:         builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{ObligationEvidenceRetention},
			},
			Governance: terminalGovernance([]string{ObligationEvidenceRetention}, nil),
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
	out := capabilityRoutes(NodeReadPopulation, NodeReadRatingClaim)
	out = append(out, capabilityRoutes(NodeReadRatingClaim, NodeReadModelInference)...)
	out = append(out, capabilityRoutes(NodeReadModelInference, NodeResolveCalibrationCommittee)...)
	out = append(out, capabilityRoutes(NodeResolveCalibrationCommittee, NodeComputeCalibrationFootprint)...)
	out = append(out,
		workflow.Edge{From: NodeComputeCalibrationFootprint, To: NodeBuildAssessmentProposal, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeComputeCalibrationFootprint, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)},

		workflow.Edge{From: NodeBuildAssessmentProposal, To: NodeCalibrationDecision, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeBuildAssessmentProposal, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)},

		workflow.Edge{From: NodeCalibrationDecision, To: NodeEndRevisionCreated, RouteKey: RouteCorrectionRequiresNewRevision},
		workflow.Edge{From: NodeCalibrationDecision, To: NodeEndStalePopulation, RouteKey: RouteStalePopulationUnknown},
		workflow.Edge{From: NodeCalibrationDecision, To: NodeEndUnauthorizedRating, RouteKey: RouteUnauthorizedRatingBlocked},
		workflow.Edge{From: NodeCalibrationDecision, To: NodeEndCalibrationConflict, RouteKey: RouteCalibrationConflictBlocked},
		workflow.Edge{From: NodeCalibrationDecision, To: NodeObserveCalibrationRecord, RouteKey: RouteCalibrationOK},
		workflow.Edge{From: NodeCalibrationDecision, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},

		workflow.Edge{From: NodeObserveCalibrationRecord, To: NodeEndPendingApprovals, RouteKey: string(workflow.OutcomePass)},
		workflow.Edge{From: NodeObserveCalibrationRecord, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomeFail)},
		workflow.Edge{From: NodeObserveCalibrationRecord, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomePartial)},
		workflow.Edge{From: NodeObserveCalibrationRecord, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},
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
		{ID: CapReadPopulation, Version: 1},
		{ID: CapReadRatingClaim, Version: 1},
		{ID: CapReadModelInference, Version: 1},
		{ID: CapResolveCalibrationCommittee, Version: 1},
		{ID: CapObserveCalibrationRecord, Version: 1},
	}
}
