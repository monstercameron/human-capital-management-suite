// Package payroll is CONF-009's proof that a payroll run/calculation/
// release/settlement reference workflow compiles under P1A and walks to
// completion through the real SIMULATE-mode interpreter
// (internal/workflow/simulate), exactly as CONF-001 established for the
// promote-into-management reference and as CONF-003/CONF-005 established for
// the cross-company transfer and termination references
// (internal/workflow/conformance/{transfer,termination}).
//
// It is a conformance fixture, not a payroll engine: the capability handlers
// behind every CAPABILITY/OBSERVE node are canned, in-memory projections this
// package owns, and the gross-to-net calculation is a fixed-rate TRANSFORM
// using exact decimal arithmetic (internal/kernel/values), never a real tax
// engine or provider integration. What is under test is the workflow's own
// composition: a stale population/cutoff and a duplicate run are refused
// rather than released, a release computes exact decimal line totals and
// binds a calculation trace, and provider settlement is observed
// independently of release with PASS/FAIL/PARTIAL/UNKNOWN routed to distinct,
// separately-tracked terminals rather than a single collapsed "success".
package payroll

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
)

// Workflow identity.
const (
	WorkflowID = "hcmnext.workflows.payroll_run_calculation_release_settlement"
	Version    = 1
)

// Node ids.
const (
	NodeReadPopulationAndCutoff   = "read_population_and_cutoff"
	NodeComputeCalculation        = "compute_calculation"
	NodeBuildRunProposal          = "build_run_proposal"
	NodeReleaseDecision           = "release_decision"
	NodeObserveProviderSettlement = "observe_provider_settlement"

	NodeEndPendingSettlementObligations = "end_pending_settlement_obligations"
	NodeEndDuplicateRunBlocked          = "end_duplicate_run_blocked"
	NodeEndStaleCutoffBlocked           = "end_stale_cutoff_blocked"
	NodeEndReversalIntent               = "end_requires_reversal_intent"
	NodeEndAlreadySettledInvalid        = "end_already_settled_invalid"
	NodeEndSettlementRejectedRepair     = "end_settlement_rejected_repair"
	NodeEndSettlementAmbiguousRepair    = "end_settlement_ambiguous_repair"
	NodeEndUnknown                      = "end_unknown"
)

// Decision route keys the release DECISION declares.
const (
	RouteReversalIntent         = "REQUIRES_REVERSAL_INTENT"
	RouteAlreadySettledInvalid  = "ALREADY_SETTLED_INVALID"
	RouteDuplicateRunBlocked    = "DUPLICATE_RUN_BLOCKED"
	RouteStaleCutoffBlocked     = "STALE_CUTOFF_BLOCKED"
	RouteRoutineReleaseRequired = "ROUTINE_RELEASE_REQUIRED"
	RuleReleaseAndIntegrity     = "rules.payroll.release_and_integrity/v1"
)

// Transform references.
const (
	TransformComputeCalculation = "transforms.payroll.compute_calculation"
	TransformBuildRunProposal   = "transforms.payroll.build_run_proposal"
)

// Capability identities. This package's own fixture capabilities, never a
// bootstrap or domain capability.
const (
	CapReadPopulationAndCutoff   = "hcmnext.conformance.payroll.read_population_and_cutoff"
	CapObserveProviderSettlement = "hcmnext.conformance.payroll.observe_provider_settlement"
)

// Approval requirement ids.
const (
	ApprovalPayrollController = "approval.payroll.controller"
	ApprovalFinanceRelease    = "approval.payroll.finance_release"
	ApprovalTaxCompliance     = "approval.payroll.tax_compliance"
)

// Obligation ids. CalculationTrace and SettlementReconciliation are
// mandatory and independently tracked, which is what makes "missing trace"
// and "partial payment reported as full success" (CONF-009's RED clauses)
// checkable rather than merely asserted.
const (
	ObligationCalculationTrace         = "obligation.payroll.calculation_trace"
	ObligationSettlementReconciliation = "obligation.payroll.settlement_reconciliation"
	ObligationRecordsRetention         = "obligation.payroll.records_retention"
)

const (
	organizationScope  = "acme/payroll"
	purpose            = "SIMULATE_PAYROLL_RUN_RELEASE_SETTLEMENT"
	classification     = "CONFIDENTIAL_PAYROLL"
	dataAccessManifest = "dam.payroll.simulation/v1"
	mappingDigest      = "hcmnext.workflow.InputMappingSet/v1"
	// repairRef is the bounded repair artifact a degraded settlement terminal
	// links, which is how a rejected or ambiguous provider outcome creates
	// tracked repair evidence rather than a silently dropped observation.
	repairRef = "repair.payroll.settlement_drift/v1"
)

// ---- small typed-value helpers, mirrored from
// internal/workflow/conformance/{transfer,termination} so this package never
// has to reach into another package's unexported helpers.

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

// ReferenceDefinition returns the P1A payroll run/calculation/release/
// settlement reference workflow as typed Go values.
//
// It composes exactly what CONF-009's GREEN clause names: population and
// cutoff are read once and their staleness/duplication is checked, an exact
// decimal calculation binds a trace, a proposal is built, a release decision
// refuses a stale cutoff, a duplicate run and a reversal reaching an
// already-settled run (which becomes a distinct reversal/correction intent
// per the same pattern CONF-005's reinstatement route establishes), and
// provider settlement is observed independently of release with
// PASS/FAIL/PARTIAL/UNKNOWN routed to distinct terminals.
func ReferenceDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        WorkflowID,
		Version:           Version,
		Name:              "Payroll run, calculation, release and settlement (simulation)",
		InputSchema:       workflowSchema("PayrollRunInput"),
		OutputSchema:      workflowSchema("PayrollRunResult"),
		VariablesSchema:   workflowSchema("PayrollRunVariables"),
		TenantScope:       "acme",
		OrganizationScope: organizationScope,
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeSimulate},
		TerminalProfile:   workflow.TerminalProfileSimulateOnly,
		StartNodeID:       NodeReadPopulationAndCutoff,

		Inputs: []workflow.Field{
			{Path: "run_id", Type: str("PayrollRunID")},
			{Path: "population_snapshot_id", Type: str("PopulationSnapshotID")},
			{Path: "cutoff_date", Type: localDate()},
			{Path: "gross_pay_input", Type: money()},
			{Path: "reversal_requested", Type: boolean()},
		},
		Outputs: []workflow.Field{
			{Path: "run_id", Type: str("PayrollRunID")},
			{Path: "terminal_code", Type: plainStr()},
		},

		Limits: workflow.Limits{MaxFanOut: 6, MaxDepth: 12, MaxNodes: 24},

		FailurePolicyRef:      "policy.workflow.failure.simulation/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.simulation/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.hr-simulation/v1",

		ApprovalRequirements: []workflow.ApprovalRequirement{
			{ID: ApprovalPayrollController, ResolverExpression: "PayrollControllerFor(run)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalFinanceRelease, ResolverExpression: "FinanceReleaseApproverFor(run)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalTaxCompliance, ResolverExpression: "TaxComplianceReviewerFor(run)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
		},

		Obligations: []workflow.ObligationRequirement{
			{
				ID: ObligationCalculationTrace, Authority: "customer.policy.payroll.calculation",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Retain the exact decimal calculation trace bound to the run's proposal digest",
				ResponsibleParty: "payroll.calculation", SatisfactionCondition: "calculation trace digest is recorded against the proposal digest",
				SourceVersion: RuleReleaseAndIntegrity, ReevaluationPolicy: workflow.ReevalReevaluate, Mandatory: true,
			},
			{
				ID: ObligationSettlementReconciliation, Authority: "payroll.provider.settlement",
				InsertionPoint: workflow.InsertSimulation, RequiredAction: "Observe and reconcile provider settlement independently of release, never folding a partial or ambiguous outcome into success",
				ResponsibleParty: "payroll.settlement", SatisfactionCondition: "settlement outcome is observed and reconciled independently of the calculation and release decision",
				SourceVersion: "payroll.provider.settlement.policy/v2", ReevaluationPolicy: workflow.ReevalRequireReview, Mandatory: true,
			},
			{
				ID: ObligationRecordsRetention, Authority: "records.retention",
				InsertionPoint: workflow.InsertClosure, RequiredAction: "Preserve the run's simulation artifact and calculation trace",
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
			ID:           NodeReadPopulationAndCutoff,
			Type:         workflow.StepCapability,
			InputSchema:  bootstrapCapabilitySchema(CapReadPopulationAndCutoff, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapReadPopulationAndCutoff, "response"),
			Inputs: []workflow.Field{
				{Path: "run_id", Type: str("PayrollRunID")},
				{Path: "population_snapshot_id", Type: str("PopulationSnapshotID")},
				{Path: "cutoff_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "cutoff_active", Type: boolean()},
				{Path: "duplicate_run_detected", Type: boolean()},
				{Path: "already_settled", Type: boolean()},
				{Path: "population_watermark", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "run_id", Source: builders.FromInput("run_id")},
				{Target: "population_snapshot_id", Source: builders.FromInput("population_snapshot_id")},
				{Target: "cutoff_date", Source: builders.FromInput("cutoff_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapReadPopulationAndCutoff, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:payroll.read"},
			},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:           NodeComputeCalculation,
			Type:         workflow.StepTransform,
			InputSchema:  workflowSchema("PayrollCalculationDraft"),
			OutputSchema: workflowSchema("PayrollCalculation"),
			Inputs: []workflow.Field{
				{Path: "gross_pay_input", Type: money()},
				{Path: "population_watermark", Type: plainStr()},
			},
			Outputs: []workflow.Field{
				{Path: "net_pay_amount", Type: money()},
				{Path: "tax_amount", Type: money()},
				{Path: "calculation_trace_digest", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "gross_pay_input", Source: builders.FromInput("gross_pay_input")},
				{Target: "population_watermark", Source: builders.FromNode(NodeReadPopulationAndCutoff, "population_watermark")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformComputeCalculation, Version: 1,
				NormalizationProfile: "hcmnext.canonical.payroll_calculation/v1",
				InputTaint: []workflow.TaintInput{
					{Source: "gross_pay_input", Level: workflow.TaintTrusted},
					{Source: "population_watermark", Level: workflow.TaintTrusted},
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
			ID:           NodeBuildRunProposal,
			Type:         workflow.StepTransform,
			InputSchema:  workflowSchema("PayrollRunProposalDraft"),
			OutputSchema: workflowSchema("PayrollRunProposal"),
			Inputs: []workflow.Field{
				{Path: "run_id", Type: str("PayrollRunID")},
				{Path: "cutoff_date", Type: localDate()},
				{Path: "calculation_trace_digest", Type: plainStr()},
				{Path: "population_watermark", Type: plainStr()},
			},
			Outputs: []workflow.Field{
				{Path: "proposal_digest", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "run_id", Source: builders.FromInput("run_id")},
				{Target: "cutoff_date", Source: builders.FromInput("cutoff_date")},
				{Target: "calculation_trace_digest", Source: builders.FromNode(NodeComputeCalculation, "calculation_trace_digest")},
				{Target: "population_watermark", Source: builders.FromNode(NodeReadPopulationAndCutoff, "population_watermark")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformBuildRunProposal, Version: 1,
				NormalizationProfile: "hcmnext.canonical.payroll_run_proposal/v1",
				InputTaint: []workflow.TaintInput{
					{Source: "run_id", Level: workflow.TaintTrusted},
					{Source: "calculation_trace_digest", Level: workflow.TaintTrusted},
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
			ID:           NodeReleaseDecision,
			Type:         workflow.StepDecision,
			InputSchema:  workflowSchema("PayrollReleaseDecisionInput"),
			OutputSchema: workflowSchema("PayrollReleaseDecisionResult"),
			Inputs: []workflow.Field{
				{Path: "cutoff_active", Type: boolean()},
				{Path: "duplicate_run_detected", Type: boolean()},
				{Path: "already_settled", Type: boolean()},
				{Path: "reversal_requested", Type: boolean()},
			},
			Outputs: []workflow.Field{{Path: "route_key", Type: plainStr()}},
			InputMappings: []workflow.Mapping{
				{Target: "cutoff_active", Source: builders.FromNode(NodeReadPopulationAndCutoff, "cutoff_active")},
				{Target: "duplicate_run_detected", Source: builders.FromNode(NodeReadPopulationAndCutoff, "duplicate_run_detected")},
				{Target: "already_settled", Source: builders.FromNode(NodeReadPopulationAndCutoff, "already_settled")},
				{Target: "reversal_requested", Source: builders.FromInput("reversal_requested")},
			},
			Decision: &workflow.DecisionSpec{
				EvaluatorRef: "engines.rules.payroll_release_and_integrity", EvaluatorVersion: 1,
				RuleRef: RuleReleaseAndIntegrity, InputDigestProfile: mappingDigest,
				Routes: []workflow.DecisionRoute{
					{Key: RouteReversalIntent, Predicate: "reversal_requested_after_already_settled", Precedence: 10},
					{Key: RouteAlreadySettledInvalid, Predicate: "already_settled_without_reversal", Precedence: 20},
					{Key: RouteDuplicateRunBlocked, Predicate: "duplicate_run_detected", Precedence: 30},
					{Key: RouteStaleCutoffBlocked, Predicate: "cutoff_not_active", Precedence: 40},
					{Key: RouteRoutineReleaseRequired, Predicate: "no_blocking_condition", Precedence: 50},
				},
				DefaultRoute: RouteRoutineReleaseRequired,
			},
			Governance: workflow.NodeGovernance{
				Purpose: purpose, Classification: classification,
				RevalidationBoundary: workflow.RevalidateNone, DataAccessManifestRef: dataAccessManifest,
			},
		},
		{
			ID:           NodeObserveProviderSettlement,
			Type:         workflow.StepObserve,
			InputSchema:  bootstrapCapabilitySchema(CapObserveProviderSettlement, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapObserveProviderSettlement, "response"),
			Inputs: []workflow.Field{
				{Path: "run_id", Type: str("PayrollRunID")},
				{Path: "proposal_digest", Type: plainStr()},
			},
			Outputs: []workflow.Field{
				{Path: "settlement_state", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "run_id", Source: builders.FromInput("run_id")},
				{Target: "proposal_digest", Source: builders.FromNode(NodeBuildRunProposal, "proposal_digest")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapObserveProviderSettlement, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:payroll.provider.read"},
			},
			Observe: &workflow.ObserveSpec{
				EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "payroll.provider.settlement_projection",
				ExpectedStateFields: []string{"run_id"}, RequiredWatermarks: []string{"payroll.provider.stream_head"},
				MaxAgeSeconds: 300, ComparisonProfile: "comparison.payroll.settlement_state/v1",
				RetryExhaustionRoute: NodeEndSettlementAmbiguousRepair,
			},
			Retry:      &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.observation.bounded/v1"},
			Governance: governedInvocation(nil, nil),
		},
		{
			ID:            NodeEndPendingSettlementObligations,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("run_id", "PayrollRunID"),
			InputMappings: builders.TerminalMappings("run_id", "PAYROLL_SIMULATION_PENDING_SETTLEMENT_OBLIGATIONS"),
			End: &workflow.EndSpec{
				TerminalCode: "PAYROLL_SIMULATION_PENDING_SETTLEMENT_OBLIGATIONS", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "PENDING_OBSERVATION", "PENDING"),
				OutstandingObligationRefs: []string{
					ObligationCalculationTrace, ObligationSettlementReconciliation, ObligationRecordsRetention,
				},
			},
			Governance: terminalGovernance(
				[]string{ObligationCalculationTrace, ObligationSettlementReconciliation, ObligationRecordsRetention},
				[]string{ApprovalPayrollController, ApprovalFinanceRelease, ApprovalTaxCompliance},
			),
		},
		{
			ID:            NodeEndDuplicateRunBlocked,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("run_id", "PayrollRunID"),
			InputMappings: builders.TerminalMappings("run_id", "PAYROLL_DUPLICATE_RUN_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "PAYROLL_DUPLICATE_RUN_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndStaleCutoffBlocked,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("run_id", "PayrollRunID"),
			InputMappings: builders.TerminalMappings("run_id", "PAYROLL_STALE_CUTOFF_BLOCKED"),
			End: &workflow.EndSpec{
				TerminalCode: "PAYROLL_STALE_CUTOFF_BLOCKED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndReversalIntent,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("run_id", "PayrollRunID"),
			InputMappings: builders.TerminalMappings("run_id", "PAYROLL_REQUIRES_REVERSAL_INTENT"),
			End: &workflow.EndSpec{
				TerminalCode: "PAYROLL_REQUIRES_REVERSAL_INTENT", RuntimeStatus: workflow.RuntimeCompleted,
				// A reversal reaching an already-settled run never mutates the
				// original settled run: the original is superseded, and what
				// must be handled is a distinct reversal/correction intent,
				// exactly as CONF-005's REFACTOR clause established for
				// termination cancellation.
				CompletionMapping: builders.Completion("SUPERSEDED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndAlreadySettledInvalid,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("run_id", "PayrollRunID"),
			InputMappings: builders.TerminalMappings("run_id", "PAYROLL_ALREADY_SETTLED_INVALID"),
			End: &workflow.EndSpec{
				TerminalCode: "PAYROLL_ALREADY_SETTLED_INVALID", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndSettlementRejectedRepair,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("run_id", "PayrollRunID"),
			InputMappings: builders.TerminalMappings("run_id", "PAYROLL_SETTLEMENT_REJECTED_DEGRADED"),
			End: &workflow.EndSpec{
				TerminalCode: "PAYROLL_SETTLEMENT_REJECTED_DEGRADED", RuntimeStatus: workflow.RuntimeBlocked,
				CompletionMapping:         builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{ObligationSettlementReconciliation, ObligationRecordsRetention},
				RepairRefs:                []string{repairRef},
			},
			Governance: terminalGovernance([]string{ObligationSettlementReconciliation, ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndSettlementAmbiguousRepair,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("run_id", "PayrollRunID"),
			InputMappings: builders.TerminalMappings("run_id", "PAYROLL_SETTLEMENT_AMBIGUOUS_DEGRADED"),
			End: &workflow.EndSpec{
				TerminalCode: "PAYROLL_SETTLEMENT_AMBIGUOUS_DEGRADED", RuntimeStatus: workflow.RuntimeBlocked,
				CompletionMapping:         builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{ObligationSettlementReconciliation, ObligationRecordsRetention},
				RepairRefs:                []string{repairRef},
			},
			Governance: terminalGovernance([]string{ObligationSettlementReconciliation, ObligationRecordsRetention}, nil),
		},
		{
			ID:            NodeEndUnknown,
			Type:          workflow.StepEnd,
			Inputs:        builders.TerminalInputs("run_id", "PayrollRunID"),
			InputMappings: builders.TerminalMappings("run_id", "PAYROLL_SIMULATION_UNKNOWN"),
			End: &workflow.EndSpec{
				TerminalCode: "PAYROLL_SIMULATION_UNKNOWN", RuntimeStatus: workflow.RuntimeBlocked,
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
	out := capabilityRoutes(NodeReadPopulationAndCutoff, NodeComputeCalculation)
	out = append(out,
		workflow.Edge{From: NodeComputeCalculation, To: NodeBuildRunProposal, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeComputeCalculation, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)},

		workflow.Edge{From: NodeBuildRunProposal, To: NodeReleaseDecision, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeBuildRunProposal, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)},

		workflow.Edge{From: NodeReleaseDecision, To: NodeObserveProviderSettlement, RouteKey: RouteRoutineReleaseRequired},
		workflow.Edge{From: NodeReleaseDecision, To: NodeEndReversalIntent, RouteKey: RouteReversalIntent},
		workflow.Edge{From: NodeReleaseDecision, To: NodeEndAlreadySettledInvalid, RouteKey: RouteAlreadySettledInvalid},
		workflow.Edge{From: NodeReleaseDecision, To: NodeEndDuplicateRunBlocked, RouteKey: RouteDuplicateRunBlocked},
		workflow.Edge{From: NodeReleaseDecision, To: NodeEndStaleCutoffBlocked, RouteKey: RouteStaleCutoffBlocked},
		workflow.Edge{From: NodeReleaseDecision, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},

		workflow.Edge{From: NodeObserveProviderSettlement, To: NodeEndPendingSettlementObligations, RouteKey: string(workflow.OutcomePass)},
		workflow.Edge{From: NodeObserveProviderSettlement, To: NodeEndSettlementRejectedRepair, RouteKey: string(workflow.OutcomeFail)},
		workflow.Edge{From: NodeObserveProviderSettlement, To: NodeEndSettlementAmbiguousRepair, RouteKey: string(workflow.OutcomePartial)},
		workflow.Edge{From: NodeObserveProviderSettlement, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},
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
		{ID: CapReadPopulationAndCutoff, Version: 1},
		{ID: CapObserveProviderSettlement, Version: 1},
	}
}
