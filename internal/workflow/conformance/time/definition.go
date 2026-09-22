// Package time is CONF-011's proof that a time-punch/timecard/payroll-
// bridge reference workflow compiles under P1A and walks to completion
// through the real SIMULATE-mode interpreter (internal/workflow/simulate),
// exactly as CONF-001 established for the promote-into-management
// reference and as CONF-003/CONF-005/CONF-009/CONF-010 established for the
// cross-company transfer, termination, payroll and benefits references
// (internal/workflow/conformance/{transfer,termination,payroll,benefits}).
//
// It is a conformance fixture, not a timekeeping product: the capability
// handlers behind every CAPABILITY/OBSERVE node are canned, in-memory
// projections this package owns, never a real punch clock, device fleet or
// payroll-bridge integration. What is under test is the workflow's own
// composition: a duplicate punch, a post-lock edit, a stale reopened
// timecard, a suspected shared-device spoof, an offline-replayed punch, a
// DST-fold-ambiguous timestamp and an out-of-tolerance clock skew are each
// classified to their own distinct ACCEPTED/REVIEW/REJECTED/DUPLICATE
// outcome rather than accepted silently, and payroll-bridge ingestion is
// observed independently of classification with its own bounded repair.
package time

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
)

// Workflow identity.
const (
	WorkflowID = "hcmnext.workflows.time_punch_timecard_payroll_bridge"
	Version    = 1
)

// Node ids.
const (
	NodeReadDeviceAndClockContext = "read_device_and_clock_context"
	NodeBuildPunchProposal        = "build_punch_proposal"
	NodeClassificationDecision    = "classification_decision"
	NodeObservePayrollBridge      = "observe_payroll_bridge"

	NodeEndAcceptedBridgeConfirmed = "end_accepted_bridge_confirmed"
	NodeEndBridgeDegradedRepair    = "end_bridge_degraded_repair"
	NodeEndReviewClockSkew         = "end_review_clock_skew"
	NodeEndReviewDSTFold           = "end_review_dst_fold"
	NodeEndReviewOfflineReplay     = "end_review_offline_replay"
	NodeEndReviewSpoofSuspected    = "end_review_spoof_suspected"
	NodeEndRejectedPostLockEdit    = "end_rejected_post_lock_edit"
	NodeEndRejectedStaleReopen     = "end_rejected_stale_reopen"
	NodeEndDuplicatePunch          = "end_duplicate_punch"
	NodeEndUnknown                 = "end_unknown"
)

// Decision route keys the classification DECISION declares. Precedence is
// fixed: a duplicate punch is checked before any other condition, an
// irreversible post-lock or stale-reopen edit before a merely reviewable
// integrity concern, and every REVIEW reason gets its own route rather than
// being folded into one generic "needs review".
const (
	RouteDuplicatePunch        = "DUPLICATE_PUNCH"
	RouteRejectedPostLockEdit  = "REJECTED_POST_LOCK_EDIT"
	RouteRejectedStaleReopen   = "REJECTED_STALE_REOPEN"
	RouteReviewSpoofSuspected  = "REVIEW_SPOOF_SUSPECTED"
	RouteReviewOfflineReplay   = "REVIEW_OFFLINE_REPLAY"
	RouteReviewDSTFold         = "REVIEW_DST_FOLD"
	RouteReviewClockSkew       = "REVIEW_CLOCK_SKEW"
	RouteAccepted              = "ACCEPTED"
	RuleClassifyPunchIntegrity = "rules.time.classify_punch_integrity/v1"
)

// Transform references.
const (
	TransformBuildPunchProposal = "transforms.time.build_punch_proposal"
)

// Capability identities. This package's own fixture capabilities, never a
// bootstrap or domain capability.
const (
	CapReadDeviceAndClockContext = "hcmnext.conformance.time.read_device_and_clock_context"
	CapObservePayrollBridge      = "hcmnext.conformance.time.observe_payroll_bridge"
)

// Approval requirement ids. Only the ACCEPTED terminal awaits them: a
// REJECTED/REVIEW/DUPLICATE punch has no approval to await because it never
// reaches the payroll bridge.
const (
	ApprovalTimekeepingAdmin = "approval.time.timekeeping_admin"
	ApprovalPayrollBridge    = "approval.time.payroll_bridge_release"
)

// Obligation ids. Attestation, Correction and Cutoff are what CONF-011's
// GREEN clause names as preserved dimensions; BridgeIntegrity is mandatory
// and independently tracked, which is what makes a degraded bridge
// observation checkable as its own RED case rather than folded into the
// punch's own classification.
const (
	ObligationAttestation      = "obligation.time.attestation"
	ObligationCorrection       = "obligation.time.correction_routing"
	ObligationCutoff           = "obligation.time.payroll_cutoff_binding"
	ObligationBridgeIntegrity  = "obligation.time.payroll_bridge_integrity"
	ObligationRecordsRetention = "obligation.time.records_retention"
)

const (
	organizationScope  = "acme/time"
	purpose            = "SIMULATE_TIME_PUNCH_TIMECARD_PAYROLL_BRIDGE"
	classification     = "CONFIDENTIAL_TIME_AND_ATTENDANCE"
	dataAccessManifest = "dam.time.simulation/v1"
	mappingDigest      = "hcmnext.workflow.InputMappingSet/v1"
	// repairRef is the bounded repair artifact the degraded bridge terminal
	// links, which is how a failed or partial payroll-bridge ingestion
	// creates tracked repair evidence rather than a silently dropped
	// observation.
	repairRef = "repair.time.payroll_bridge_drift/v1"
)

// ---- small typed-value helpers, mirrored from
// internal/workflow/conformance/{transfer,termination,payroll,benefits} so
// this package never has to reach into another package's unexported
// helpers.

func str(brand string) workflow.ValueType {
	return workflow.ValueType{Kind: workflow.KindString, Brand: brand}
}
func plainStr() workflow.ValueType { return workflow.ValueType{Kind: workflow.KindString} }
func boolean() workflow.ValueType  { return workflow.ValueType{Kind: workflow.KindBool} }

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

// ReferenceDefinition returns the P1A time-punch/timecard/payroll-bridge
// reference workflow as typed Go values.
//
// It composes exactly what CONF-011's GREEN clause names: device and clock
// context (timezone, tzdb version, device trust, lock/reopen state) is read
// once, a punch proposal binds the timezone/tzdb pin, and a classification
// decision distinguishes a duplicate punch, an irreversible post-lock edit,
// a stale reopened timecard, a suspected shared-device spoof, an offline
// replay, a DST-fold-ambiguous timestamp, an out-of-tolerance clock skew and
// a clean ACCEPTED punch - each with its own distinct route - before an
// accepted punch's payroll-bridge ingestion is observed independently with
// PASS/FAIL/PARTIAL/UNKNOWN.
func ReferenceDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        WorkflowID,
		Version:           Version,
		Name:              "Time punch, timecard and payroll-bridge integrity (simulation)",
		InputSchema:       workflowSchema("TimePunchInput"),
		OutputSchema:      workflowSchema("TimePunchResult"),
		VariablesSchema:   workflowSchema("TimePunchVariables"),
		TenantScope:       "acme",
		OrganizationScope: organizationScope,
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeSimulate},
		TerminalProfile:   workflow.TerminalProfileSimulateOnly,
		StartNodeID:       NodeReadDeviceAndClockContext,

		Inputs: []workflow.Field{
			{Path: "worker_id", Type: str("WorkerID")},
			{Path: "punch_id", Type: str("TimePunchID")},
			{Path: "device_id", Type: str("DeviceID")},
			{Path: "reported_clock_skew_seconds", Type: plainStr()},
		},
		Outputs: []workflow.Field{
			{Path: "punch_id", Type: str("TimePunchID")},
			{Path: "terminal_code", Type: plainStr()},
		},

		Limits: workflow.Limits{MaxFanOut: 10, MaxDepth: 12, MaxNodes: 24},

		FailurePolicyRef:      "policy.workflow.failure.simulation/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.simulation/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.hr-simulation/v1",

		ApprovalRequirements: []workflow.ApprovalRequirement{
			{ID: ApprovalTimekeepingAdmin, ResolverExpression: "TimekeepingAdminFor(worker)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalPayrollBridge, ResolverExpression: "PayrollBridgeReleaseApproverFor(worker)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
		},

		Obligations: []workflow.ObligationRequirement{
			{
				ID: ObligationAttestation, Authority: "customer.policy.time.attestation",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Bind the worker's own attestation of the punch to the proposal digest",
				ResponsibleParty: "time.attestation", SatisfactionCondition: "attestation reference is recorded against the proposal digest",
				SourceVersion: RuleClassifyPunchIntegrity, ReevaluationPolicy: workflow.ReevalReevaluate, Mandatory: true,
			},
			{
				ID: ObligationCorrection, Authority: "customer.policy.time.correction",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Route a reviewable or rejected punch to the correction workflow rather than discarding it",
				ResponsibleParty: "time.correction", SatisfactionCondition: "a correction routing reference is recorded for any punch that did not classify as ACCEPTED",
				SourceVersion: "time.correction.routing/v1", ReevaluationPolicy: workflow.ReevalRequireReview,
			},
			{
				ID: ObligationCutoff, Authority: "customer.policy.time.payroll_cutoff",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Bind the punch to its governing payroll cutoff window",
				ResponsibleParty: "time.cutoff", SatisfactionCondition: "the punch's payroll cutoff binding is recorded against the proposal digest",
				SourceVersion: "time.payroll_cutoff.binding/v1", ReevaluationPolicy: workflow.ReevalReevaluate, Mandatory: true,
			},
			{
				ID: ObligationBridgeIntegrity, Authority: "payroll.bridge.integrity",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Observe and reconcile payroll-bridge ingestion of the accepted punch independently of its classification",
				ResponsibleParty: "payroll.bridge", SatisfactionCondition: "bridge ingestion outcome is observed and reconciled independently of the punch's own classification",
				SourceVersion: "payroll.bridge.integrity.policy/v2", ReevaluationPolicy: workflow.ReevalRequireReview, Mandatory: true,
			},
			{
				ID: ObligationRecordsRetention, Authority: "records.retention",
				InsertionPoint: workflow.InsertClosure, RequiredAction: "Preserve the punch's simulation artifact and its timezone/tzdb pin",
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
			ID:           NodeReadDeviceAndClockContext,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapReadDeviceAndClockContext, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapReadDeviceAndClockContext, "response"),
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "punch_id", Type: str("TimePunchID")},
				{Path: "device_id", Type: str("DeviceID")},
				{Path: "reported_clock_skew_seconds", Type: plainStr()},
			},
			Outputs: []workflow.Field{
				{Path: "clock_skew_within_tolerance", Type: boolean()},
				{Path: "dst_fold_ambiguous", Type: boolean()},
				{Path: "offline_replay_detected", Type: boolean()},
				{Path: "shared_device_spoof_suspected", Type: boolean()},
				{Path: "post_lock_edit_attempted", Type: boolean()},
				{Path: "timecard_reopened_stale", Type: boolean()},
				{Path: "duplicate_punch_detected", Type: boolean()},
				{Path: "timezone_id", Type: plainStr()},
				{Path: "tzdb_version", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: builders.FromInput("worker_id")},
				{Target: "punch_id", Source: builders.FromInput("punch_id")},
				{Target: "device_id", Source: builders.FromInput("device_id")},
				{Target: "reported_clock_skew_seconds", Source: builders.FromInput("reported_clock_skew_seconds")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapReadDeviceAndClockContext, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:time.device.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeBuildPunchProposal,
			Type:         workflow.StepTransform,
			InputSchema:  workflowSchema("TimePunchProposalDraft"),
			OutputSchema: workflowSchema("TimePunchProposal"),
			Inputs: []workflow.Field{
				{Path: "punch_id", Type: str("TimePunchID")},
				{Path: "timezone_id", Type: plainStr()},
				{Path: "tzdb_version", Type: plainStr()},
			},
			Outputs: []workflow.Field{
				{Path: "proposal_digest", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "punch_id", Source: builders.FromInput("punch_id")},
				{Target: "timezone_id", Source: builders.FromNode(NodeReadDeviceAndClockContext, "timezone_id")},
				{Target: "tzdb_version", Source: builders.FromNode(NodeReadDeviceAndClockContext, "tzdb_version")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformBuildPunchProposal, Version: 1,
				NormalizationProfile: "hcmnext.canonical.time_punch_proposal/v1",
				InputTaint: []workflow.TaintInput{
					{Source: "punch_id", Level: workflow.TaintTrusted},
					{Source: "timezone_id", Level: workflow.TaintTrusted},
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
			ID:           NodeClassificationDecision,
			Type:         workflow.StepDecision,
			InputSchema:  workflowSchema("TimePunchClassificationInput"),
			OutputSchema: workflowSchema("TimePunchClassificationResult"),
			Inputs: []workflow.Field{
				{Path: "duplicate_punch_detected", Type: boolean()},
				{Path: "post_lock_edit_attempted", Type: boolean()},
				{Path: "timecard_reopened_stale", Type: boolean()},
				{Path: "shared_device_spoof_suspected", Type: boolean()},
				{Path: "offline_replay_detected", Type: boolean()},
				{Path: "dst_fold_ambiguous", Type: boolean()},
				{Path: "clock_skew_within_tolerance", Type: boolean()},
			},
			Outputs: []workflow.Field{{Path: "route_key", Type: plainStr()}},
			InputMappings: []workflow.Mapping{
				{Target: "duplicate_punch_detected", Source: builders.FromNode(NodeReadDeviceAndClockContext, "duplicate_punch_detected")},
				{Target: "post_lock_edit_attempted", Source: builders.FromNode(NodeReadDeviceAndClockContext, "post_lock_edit_attempted")},
				{Target: "timecard_reopened_stale", Source: builders.FromNode(NodeReadDeviceAndClockContext, "timecard_reopened_stale")},
				{Target: "shared_device_spoof_suspected", Source: builders.FromNode(NodeReadDeviceAndClockContext, "shared_device_spoof_suspected")},
				{Target: "offline_replay_detected", Source: builders.FromNode(NodeReadDeviceAndClockContext, "offline_replay_detected")},
				{Target: "dst_fold_ambiguous", Source: builders.FromNode(NodeReadDeviceAndClockContext, "dst_fold_ambiguous")},
				{Target: "clock_skew_within_tolerance", Source: builders.FromNode(NodeReadDeviceAndClockContext, "clock_skew_within_tolerance")},
			},
			Decision: &workflow.DecisionSpec{
				EvaluatorRef: "engines.rules.time_classify_punch_integrity", EvaluatorVersion: 1,
				RuleRef: RuleClassifyPunchIntegrity, InputDigestProfile: mappingDigest,
				Routes: []workflow.DecisionRoute{
					{Key: RouteDuplicatePunch, Predicate: "duplicate_punch_detected", Precedence: 10},
					{Key: RouteRejectedPostLockEdit, Predicate: "post_lock_edit_attempted", Precedence: 20},
					{Key: RouteRejectedStaleReopen, Predicate: "timecard_reopened_stale", Precedence: 30},
					{Key: RouteReviewSpoofSuspected, Predicate: "shared_device_spoof_suspected", Precedence: 40},
					{Key: RouteReviewOfflineReplay, Predicate: "offline_replay_detected", Precedence: 50},
					{Key: RouteReviewDSTFold, Predicate: "dst_fold_ambiguous", Precedence: 60},
					{Key: RouteReviewClockSkew, Predicate: "clock_skew_not_within_tolerance", Precedence: 70},
					{Key: RouteAccepted, Predicate: "no_blocking_or_reviewable_condition", Precedence: 80},
				},
				DefaultRoute: RouteAccepted,
			},
			Governance: workflow.NodeGovernance{
				Purpose: purpose, Classification: classification,
				RevalidationBoundary: workflow.RevalidateNone, DataAccessManifestRef: dataAccessManifest,
			},
		},
		{
			ID:           NodeObservePayrollBridge,
			Type:         workflow.StepObserve,
			InputSchema:  bootstrapCapabilitySchema(CapObservePayrollBridge, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapObservePayrollBridge, "response"),
			Inputs: []workflow.Field{
				{Path: "punch_id", Type: str("TimePunchID")},
				{Path: "proposal_digest", Type: plainStr()},
			},
			Outputs: []workflow.Field{
				{Path: "bridge_state", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "punch_id", Source: builders.FromInput("punch_id")},
				{Target: "proposal_digest", Source: builders.FromNode(NodeBuildPunchProposal, "proposal_digest")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapObservePayrollBridge, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:time.bridge.read"},
			},
			Observe: &workflow.ObserveSpec{
				EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "payroll.bridge.ingestion_projection",
				ExpectedStateFields: []string{"punch_id"}, RequiredWatermarks: []string{"payroll.bridge.stream_head"},
				MaxAgeSeconds: 300, ComparisonProfile: "comparison.time.bridge_integrity/v1",
				RetryExhaustionRoute: NodeEndBridgeDegradedRepair,
			},
			Retry:      &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.observation.bounded/v1"},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:            NodeEndAcceptedBridgeConfirmed,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("punch_id", "TimePunchID"),
			InputMappings: builders.TerminalMappings("punch_id", "TIME_PUNCH_ACCEPTED_BRIDGE_CONFIRMED"),
			End: &workflow.EndSpec{
				TerminalCode: "TIME_PUNCH_ACCEPTED_BRIDGE_CONFIRMED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "PENDING_OBSERVATION", "PENDING"),
				OutstandingObligationRefs: []string{
					ObligationAttestation, ObligationCutoff, ObligationBridgeIntegrity, ObligationRecordsRetention,
				},
			},
			Governance: terminalGovernance(
				[]string{ObligationAttestation, ObligationCutoff, ObligationBridgeIntegrity, ObligationRecordsRetention},
				[]string{ApprovalTimekeepingAdmin, ApprovalPayrollBridge},
			),
		},
		{
			ID:            NodeEndBridgeDegradedRepair,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("punch_id", "TimePunchID"),
			InputMappings: builders.TerminalMappings("punch_id", "TIME_PUNCH_BRIDGE_INGESTION_DEGRADED"),
			End: &workflow.EndSpec{
				TerminalCode: "TIME_PUNCH_BRIDGE_INGESTION_DEGRADED", RuntimeStatus: workflow.RuntimeBlocked,
				CompletionMapping:         builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{ObligationBridgeIntegrity, ObligationRecordsRetention},
				RepairRefs:                []string{repairRef},
			},
			Governance: terminalGovernance([]string{ObligationBridgeIntegrity, ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndReviewClockSkew,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("punch_id", "TimePunchID"),
			InputMappings: builders.TerminalMappings("punch_id", "TIME_PUNCH_REVIEW_CLOCK_SKEW"),
			End: &workflow.EndSpec{
				TerminalCode: "TIME_PUNCH_REVIEW_CLOCK_SKEW", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping:         builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_ACHIEVED", "PENDING_OBSERVATION", "PENDING"),
				OutstandingObligationRefs: []string{ObligationCorrection, ObligationRecordsRetention},
			},
			Governance: terminalGovernance([]string{ObligationCorrection, ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndReviewDSTFold,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("punch_id", "TimePunchID"),
			InputMappings: builders.TerminalMappings("punch_id", "TIME_PUNCH_REVIEW_DST_FOLD"),
			End: &workflow.EndSpec{
				TerminalCode: "TIME_PUNCH_REVIEW_DST_FOLD", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping:         builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_ACHIEVED", "PENDING_OBSERVATION", "PENDING"),
				OutstandingObligationRefs: []string{ObligationCorrection, ObligationRecordsRetention},
			},
			Governance: terminalGovernance([]string{ObligationCorrection, ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndReviewOfflineReplay,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("punch_id", "TimePunchID"),
			InputMappings: builders.TerminalMappings("punch_id", "TIME_PUNCH_REVIEW_OFFLINE_REPLAY"),
			End: &workflow.EndSpec{
				TerminalCode: "TIME_PUNCH_REVIEW_OFFLINE_REPLAY", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping:         builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_ACHIEVED", "PENDING_OBSERVATION", "PENDING"),
				OutstandingObligationRefs: []string{ObligationCorrection, ObligationRecordsRetention},
			},
			Governance: terminalGovernance([]string{ObligationCorrection, ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndReviewSpoofSuspected,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("punch_id", "TimePunchID"),
			InputMappings: builders.TerminalMappings("punch_id", "TIME_PUNCH_REVIEW_SPOOF_SUSPECTED"),
			End: &workflow.EndSpec{
				TerminalCode: "TIME_PUNCH_REVIEW_SPOOF_SUSPECTED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping:         builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_ACHIEVED", "PENDING_OBSERVATION", "PENDING"),
				OutstandingObligationRefs: []string{ObligationCorrection, ObligationRecordsRetention},
			},
			Governance: terminalGovernance([]string{ObligationCorrection, ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndRejectedPostLockEdit,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("punch_id", "TimePunchID"),
			InputMappings: builders.TerminalMappings("punch_id", "TIME_PUNCH_REJECTED_POST_LOCK_EDIT"),
			End: &workflow.EndSpec{
				TerminalCode: "TIME_PUNCH_REJECTED_POST_LOCK_EDIT", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndRejectedStaleReopen,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("punch_id", "TimePunchID"),
			InputMappings: builders.TerminalMappings("punch_id", "TIME_PUNCH_REJECTED_STALE_REOPEN"),
			End: &workflow.EndSpec{
				TerminalCode: "TIME_PUNCH_REJECTED_STALE_REOPEN", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndDuplicatePunch,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("punch_id", "TimePunchID"),
			InputMappings: builders.TerminalMappings("punch_id", "TIME_PUNCH_DUPLICATE"),
			End: &workflow.EndSpec{
				TerminalCode: "TIME_PUNCH_DUPLICATE", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndUnknown,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("punch_id", "TimePunchID"),
			InputMappings: builders.TerminalMappings("punch_id", "TIME_PUNCH_SIMULATION_UNKNOWN"),
			End: &workflow.EndSpec{
				TerminalCode: "TIME_PUNCH_SIMULATION_UNKNOWN", RuntimeStatus: workflow.RuntimeBlocked,
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
	out := capabilityRoutes(NodeReadDeviceAndClockContext, NodeBuildPunchProposal)
	out = append(out,
		workflow.Edge{From: NodeBuildPunchProposal, To: NodeClassificationDecision, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeBuildPunchProposal, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)},

		workflow.Edge{From: NodeClassificationDecision, To: NodeObservePayrollBridge, RouteKey: RouteAccepted},
		workflow.Edge{From: NodeClassificationDecision, To: NodeEndDuplicatePunch, RouteKey: RouteDuplicatePunch},
		workflow.Edge{From: NodeClassificationDecision, To: NodeEndRejectedPostLockEdit, RouteKey: RouteRejectedPostLockEdit},
		workflow.Edge{From: NodeClassificationDecision, To: NodeEndRejectedStaleReopen, RouteKey: RouteRejectedStaleReopen},
		workflow.Edge{From: NodeClassificationDecision, To: NodeEndReviewSpoofSuspected, RouteKey: RouteReviewSpoofSuspected},
		workflow.Edge{From: NodeClassificationDecision, To: NodeEndReviewOfflineReplay, RouteKey: RouteReviewOfflineReplay},
		workflow.Edge{From: NodeClassificationDecision, To: NodeEndReviewDSTFold, RouteKey: RouteReviewDSTFold},
		workflow.Edge{From: NodeClassificationDecision, To: NodeEndReviewClockSkew, RouteKey: RouteReviewClockSkew},
		workflow.Edge{From: NodeClassificationDecision, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},

		workflow.Edge{From: NodeObservePayrollBridge, To: NodeEndAcceptedBridgeConfirmed, RouteKey: string(workflow.OutcomePass)},
		workflow.Edge{From: NodeObservePayrollBridge, To: NodeEndBridgeDegradedRepair, RouteKey: string(workflow.OutcomeFail)},
		workflow.Edge{From: NodeObservePayrollBridge, To: NodeEndBridgeDegradedRepair, RouteKey: string(workflow.OutcomePartial)},
		workflow.Edge{From: NodeObservePayrollBridge, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},
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
		{ID: CapReadDeviceAndClockContext, Version: 1},
		{ID: CapObservePayrollBridge, Version: 1},
	}
}
