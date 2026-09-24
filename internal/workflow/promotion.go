package workflow

// This file carries the P1A compilation of the promote-into-management
// reference workflow (planning/reference-workflows/promote-into-management.md).
//
// The reference document describes the whole cross-domain business action:
// reservations, approvals, an ACID core commit, payroll/access/learning
// effects, reconciliation and repair. P1A implements the front half of that —
// preflight and simulation — with five primitives and zero effects, so what is
// modelled here is the honest P1A slice:
//
//	snapshot the worker, simulate compensation, evaluate the pay band, build a
//	typed proposal, decide whether the raise crosses the customer threshold,
//	observe the projection for drift, and end in a terminal that states all
//	five intent dimensions rather than a single "success".
//
// The parts P1A does not implement are not faked: the Finance Partner
// approval the threshold branch discovers is declared as an approval
// requirement and an outstanding obligation on a terminal that reports
// ObligationState=PENDING, not executed by an APPROVAL node that does not
// exist yet.

import "github.com/monstercameron/human-capital-management-suite/internal/capability"

// Reference workflow identity.
const (
	PromotionWorkflowID = "hcmnext.workflows.promote_into_management"
	PromotionVersion    = 1
)

// Node ids of the promotion reference workflow. They are exported because a
// conformance fixture, an inspector projection and the P1B runtime all need to
// name the same nodes.
const (
	PromotionNodeSnapshotWorker  = "snapshot_worker"
	PromotionNodeSimulateComp    = "simulate_compensation"
	PromotionNodeEvaluateBand    = "evaluate_band"
	PromotionNodeBuildProposal   = "build_proposal"
	PromotionNodeRaiseThreshold  = "raise_threshold"
	PromotionDecisionFactsIRID   = "transforms.promotion.project_decision_facts"
	PromotionDecisionFactsIRVer  = "1"
	PromotionNodeObserveDrift    = "observe_projection"
	PromotionNodeEndSimulated    = "end_simulated_consistent"
	PromotionNodeEndApproval     = "end_requires_finance_approval"
	PromotionNodeEndDegraded     = "end_degraded"
	PromotionNodeEndUnknown      = "end_unknown"
	PromotionNodeEndRejected     = "end_rejected"
	PromotionApprovalFinance     = "approval.finance_partner"
	PromotionApprovalManager     = "approval.current_manager"
	PromotionObligationApproval  = "obligation.finance_partner_approval"
	PromotionObligationEvidence  = "obligation.simulation_evidence_retained"
	PromotionObligationTraining  = "obligation.regulatory_manager_training"
	promotionOrganizationScope   = "acme/engineering"
	promotionDataAccessManifest  = "dam.promotion.simulation/v1"
	promotionPurpose             = "SIMULATE_MANAGEMENT_PROMOTION"
	promotionClassification      = "CONFIDENTIAL_HR"
	promotionThresholdRule       = "rules.compensation.raise_threshold/v3"
	promotionThresholdEvaluator  = "engines.decisiontable"
	promotionMappingDigestFormat = "hcmnext.workflow.InputMappingSet/v1"
)

// bootstrapCapabilitySchema mirrors the BOOTSTRAP capability registry's schema
// naming (internal/capability): each bootstrap capability publishes
// "<id>.<slot>/v1" at version 1. A node must name its capability's exact
// request and response schema, so the reference workflow names them here.
func bootstrapCapabilitySchema(id, slot string) SchemaRef {
	return SchemaRef{
		SchemaID:         id + "." + slot + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
	}
}

func workflowSchema(name string) SchemaRef {
	return SchemaRef{
		SchemaID:         name + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.workflows.v1." + name,
	}
}

func str(brand string) ValueType     { return ValueType{Kind: KindString, Brand: brand} }
func plainStr() ValueType            { return ValueType{Kind: KindString} }
func money() ValueType               { return ValueType{Kind: KindMoney} }
func decimal() ValueType             { return ValueType{Kind: KindDecimal} }
func boolean() ValueType             { return ValueType{Kind: KindBool} }
func localDate() ValueType           { return ValueType{Kind: KindLocalDate} }
func nullable(t ValueType) ValueType { t.Nullable = true; return t }

func fromInput(path string) Source {
	return Source{Kind: SourceWorkflowInput, Path: path}
}

func fromNode(nodeID, path string) Source {
	return Source{Kind: SourceNodeOutput, NodeID: nodeID, Path: path}
}

func constant(value string, t ValueType) Source {
	return Source{Kind: SourceConstant, Constant: value, Type: t}
}

func governedInvocation(obligations, approvals []string, boundary RevalidationBoundary) NodeGovernance {
	return NodeGovernance{
		Purpose:               promotionPurpose,
		Classification:        promotionClassification,
		RequiredDecisions:     []GovernanceKind{GovernanceAuthZ, GovernanceLegal, GovernancePurpose, GovernanceRisk},
		ObligationRefs:        obligations,
		ApprovalRequirements:  approvals,
		RevalidationBoundary:  boundary,
		DataAccessManifestRef: promotionDataAccessManifest,
	}
}

func terminalGovernance(obligations, approvals []string) NodeGovernance {
	return NodeGovernance{
		Purpose:               promotionPurpose,
		Classification:        promotionClassification,
		ObligationRefs:        obligations,
		ApprovalRequirements:  approvals,
		RevalidationBoundary:  RevalidatePreClosure,
		DataAccessManifestRef: promotionDataAccessManifest,
	}
}

// terminalInputs are the fields every terminal binds: the workflow's declared
// outputs.
func terminalInputs(extra ...Field) []Field {
	base := []Field{
		{Path: "worker_id", Type: str("WorkerID")},
		{Path: "terminal_code", Type: plainStr()},
	}
	return append(base, extra...)
}

func terminalMappings(code string, extra ...Mapping) []Mapping {
	base := []Mapping{
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

// PromotionReferenceDefinition returns the P1A promote-into-management
// reference workflow as typed Go values. It is the compiler's golden fixture
// and the P1B runtime's first real plan.
func PromotionReferenceDefinition() Definition {
	return Definition{
		WorkflowID:        PromotionWorkflowID,
		Version:           PromotionVersion,
		Name:              "Promote into management (simulation)",
		InputSchema:       workflowSchema("PromoteIntoManagementInput"),
		OutputSchema:      workflowSchema("PromoteIntoManagementResult"),
		VariablesSchema:   workflowSchema("PromoteIntoManagementVariables"),
		TenantScope:       "acme",
		OrganizationScope: promotionOrganizationScope,
		RiskClass:         "HIGH",
		DeclaredModes:     []ExecutionMode{ModeSimulate},
		TerminalProfile:   TerminalProfileSimulateOnly,
		StartNodeID:       PromotionNodeSnapshotWorker,

		Inputs: []Field{
			{Path: "worker_id", Type: str("WorkerID")},
			{Path: "target_job_id", Type: str("JobID")},
			{Path: "proposed_base_pay", Type: money()},
			{Path: "effective_date", Type: localDate()},
		},
		Outputs: []Field{
			{Path: "worker_id", Type: str("WorkerID")},
			{Path: "terminal_code", Type: plainStr()},
		},

		Limits: Limits{MaxFanOut: 4, MaxDepth: 12, MaxNodes: 24},

		FailurePolicyRef:      "policy.workflow.failure.simulation/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.simulation/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.hr-simulation/v1",

		ApprovalRequirements: []ApprovalRequirement{
			{
				ID:                  PromotionApprovalManager,
				ResolverExpression:  "CurrentManagerOf(worker)",
				Scope:               promotionOrganizationScope,
				Quorum:              1,
				SeparationOfDuties:  true,
				EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND",
			},
			{
				ID:                  PromotionApprovalFinance,
				ResolverExpression:  "FinancePartnerFor(cost_center)",
				Scope:               promotionOrganizationScope,
				Quorum:              1,
				SeparationOfDuties:  true,
				EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND",
			},
		},

		Obligations: []ObligationRequirement{
			{
				ID:                    PromotionObligationApproval,
				Authority:             "customer.policy.compensation",
				InsertionPoint:        InsertApproval,
				RequiredAction:        "Obtain Finance Partner approval for the receiving cost center",
				ResponsibleParty:      "FinancePartnerFor(cost_center)",
				SatisfactionCondition: "approval binding recorded against the current material proposal digest",
				SourceVersion:         promotionThresholdRule,
				ReevaluationPolicy:    ReevalReevaluate,
				Mandatory:             true,
			},
			{
				ID:                    PromotionObligationTraining,
				Authority:             "regulation.jurisdiction.resolver",
				InsertionPoint:        InsertSimulation,
				RequiredAction:        "Report mandatory manager training resulting from the promotion",
				ResponsibleParty:      "learning.assignments",
				SatisfactionCondition: "simulation artifact lists the resolved training assignments",
				SourceVersion:         "regulation.manager-training/v2",
				ReevaluationPolicy:    ReevalRequireReview,
			},
			{
				ID:                    PromotionObligationEvidence,
				Authority:             "records.retention",
				InsertionPoint:        InsertClosure,
				RequiredAction:        "Retain the simulation artifact and its evidence references",
				ResponsibleParty:      "operations.records",
				SatisfactionCondition: "simulation artifact digest recorded with the terminal result",
				SourceVersion:         "records.retention.hr-simulation/v1",
				ReevaluationPolicy:    ReevalPin,
				Mandatory:             true,
			},
		},

		Nodes: promotionNodes(),
		Edges: promotionEdges(),
	}
}

func promotionNodes() []Node {
	const (
		capWorkerState = "hcmnext.people.explain_worker_state"
		capSimulate    = "hcmnext.rewards.simulate_compensation"
		capBand        = "hcmnext.rewards.evaluate_pay_band_position"
		capDrift       = "hcmnext.operations.detect_drift"
	)

	return []Node{
		{
			ID:           PromotionNodeSnapshotWorker,
			Type:         StepCapability,
			InputSchema:  bootstrapCapabilitySchema(capWorkerState, "request"),
			OutputSchema: bootstrapCapabilitySchema(capWorkerState, "response"),
			Inputs: []Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "current_job_id", Type: str("JobID")},
				{Path: "current_level", Type: plainStr()},
				{Path: "manager_id", Type: str("WorkerID")},
				{Path: "employment_active", Type: boolean()},
			},
			InputMappings: []Mapping{
				{Target: "worker_id", Source: fromInput("worker_id")},
				{Target: "effective_date", Source: fromInput("effective_date")},
			},
			RequiredContext: []ContextRequirement{{
				Kind:                  "LegalContext",
				FieldPaths:            []string{"jurisdiction", "applicable_rule_versions"},
				Purpose:               promotionPurpose,
				MaximumClassification: promotionClassification,
				MaxAgeSeconds:         3600,
				RequiredWatermarks:    []string{"legal.policy.version"},
				Pinned:                true,
				MissingBehavior:       MissingUnknown,
			}},
			Capability: &CapabilityRef{
				ID: capWorkerState, Version: 1,
				OperationMode:   ModeSimulate,
				AuthorityScopes: []string{"scope:people.read"},
			},
			Governance: governedInvocation(nil, nil, RevalidatePreExecution),
		},
		{
			ID:           PromotionNodeSimulateComp,
			Type:         StepCapability,
			InputSchema:  bootstrapCapabilitySchema(capSimulate, "request"),
			OutputSchema: bootstrapCapabilitySchema(capSimulate, "response"),
			Inputs: []Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "target_job_id", Type: str("JobID")},
				{Path: "proposed_base_pay", Type: money()},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []Field{
				{Path: "annualized_delta", Type: money()},
				{Path: "raise_ratio", Type: decimal()},
				{Path: "cost_center", Type: plainStr()},
			},
			InputMappings: []Mapping{
				{Target: "worker_id", Source: fromNode(PromotionNodeSnapshotWorker, "worker_id")},
				{Target: "target_job_id", Source: fromInput("target_job_id")},
				{Target: "proposed_base_pay", Source: fromInput("proposed_base_pay")},
				{Target: "effective_date", Source: fromInput("effective_date")},
			},
			Capability: &CapabilityRef{
				ID: capSimulate, Version: 1,
				OperationMode:   ModeSimulate,
				AuthorityScopes: []string{"scope:rewards.read"},
			},
			Governance: governedInvocation([]string{PromotionObligationTraining}, nil, RevalidatePreExecution),
		},
		{
			ID:           PromotionNodeEvaluateBand,
			Type:         StepCapability,
			InputSchema:  bootstrapCapabilitySchema(capBand, "request"),
			OutputSchema: bootstrapCapabilitySchema(capBand, "response"),
			Inputs: []Field{
				{Path: "target_job_id", Type: str("JobID")},
				{Path: "proposed_base_pay", Type: money()},
			},
			Outputs: []Field{
				{Path: "band_position", Type: plainStr()},
				{Path: "within_band", Type: boolean()},
			},
			InputMappings: []Mapping{
				{Target: "target_job_id", Source: fromInput("target_job_id")},
				{Target: "proposed_base_pay", Source: fromInput("proposed_base_pay")},
			},
			Capability: &CapabilityRef{
				ID: capBand, Version: 1,
				OperationMode:   ModeSimulate,
				AuthorityScopes: []string{"scope:rewards.read"},
			},
			Governance: governedInvocation(nil, nil, RevalidatePreExecution),
		},
		{
			ID:           PromotionNodeBuildProposal,
			Type:         StepTransform,
			InputSchema:  workflowSchema("PromotionProposalDraft"),
			OutputSchema: workflowSchema("PromotionProposal"),
			Inputs: []Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "current_job_id", Type: str("JobID")},
				{Path: "target_job_id", Type: str("JobID")},
				{Path: "proposed_base_pay", Type: money()},
				{Path: "raise_ratio", Type: decimal()},
				{Path: "band_position", Type: plainStr()},
			},
			Outputs: []Field{
				{Path: "proposal_digest", Type: plainStr()},
				{Path: "raise_ratio", Type: decimal()},
				{Path: "band_position", Type: plainStr()},
			},
			InputMappings: []Mapping{
				{Target: "worker_id", Source: fromNode(PromotionNodeSnapshotWorker, "worker_id")},
				{Target: "current_job_id", Source: fromNode(PromotionNodeSnapshotWorker, "current_job_id")},
				{Target: "target_job_id", Source: fromInput("target_job_id")},
				{Target: "proposed_base_pay", Source: fromInput("proposed_base_pay")},
				{Target: "raise_ratio", Source: fromNode(PromotionNodeSimulateComp, "raise_ratio")},
				{Target: "band_position", Source: fromNode(PromotionNodeEvaluateBand, "band_position")},
			},
			Transform: &TransformSpec{
				TransformRef:         "transforms.promotion.build_proposal",
				Version:              1,
				NormalizationProfile: "hcmnext.canonical.proposal/v1",
				InputTaint: []TaintInput{
					{Source: "worker_id", Level: TaintTrusted},
					{Source: "current_job_id", Level: TaintTrusted},
					{Source: "raise_ratio", Level: TaintTrusted},
					{Source: "band_position", Level: TaintTrusted},
				},
				OutputTaint: TaintTrusted,
				Limits: TransformLimits{
					MaxInputBytes:  64 * 1024,
					MaxOutputBytes: 64 * 1024,
					MaxSteps:       5_000,
				},
			},
			Governance: NodeGovernance{
				Purpose:               promotionPurpose,
				Classification:        promotionClassification,
				RevalidationBoundary:  RevalidateNone,
				DataAccessManifestRef: promotionDataAccessManifest,
			},
		},
		{
			ID:           PromotionNodeRaiseThreshold,
			Type:         StepDecision,
			InputSchema:  workflowSchema("RaiseThresholdInput"),
			OutputSchema: workflowSchema("RaiseThresholdResult"),
			Inputs: []Field{
				{Path: "raise_ratio", Type: decimal()},
				{Path: "band_position", Type: plainStr()},
			},
			Outputs: []Field{
				{Path: "route_key", Type: plainStr()},
			},
			InputMappings: []Mapping{
				{Target: "raise_ratio", Source: fromNode(PromotionNodeBuildProposal, "raise_ratio")},
				{Target: "band_position", Source: fromNode(PromotionNodeBuildProposal, "band_position")},
			},
			Decision: &DecisionSpec{
				EvaluatorRef:       promotionThresholdEvaluator,
				EvaluatorVersion:   1,
				RuleRef:            promotionThresholdRule,
				InputDigestProfile: promotionMappingDigestFormat,
				Routes: []DecisionRoute{
					{Key: "WITHIN_THRESHOLD", Predicate: "raise_ratio_at_or_below_threshold", Precedence: 10},
					{Key: "EXCEEDS_THRESHOLD", Predicate: "raise_ratio_above_threshold", Precedence: 20},
				},
				DefaultRoute: "WITHIN_THRESHOLD",
			},
			Governance: NodeGovernance{
				Purpose:               promotionPurpose,
				Classification:        promotionClassification,
				RevalidationBoundary:  RevalidateNone,
				DataAccessManifestRef: promotionDataAccessManifest,
			},
		},
		{
			ID:           PromotionNodeObserveDrift,
			Type:         StepObserve,
			InputSchema:  bootstrapCapabilitySchema(capDrift, "request"),
			OutputSchema: bootstrapCapabilitySchema(capDrift, "response"),
			Inputs: []Field{
				{Path: "worker_id", Type: str("WorkerID")},
				{Path: "expected_job_id", Type: str("JobID")},
				{Path: "effective_date", Type: localDate()},
			},
			Outputs: []Field{
				{Path: "observed_job_id", Type: nullable(str("JobID"))},
				{Path: "drift_detected", Type: boolean()},
				{Path: "source_watermark", Type: plainStr()},
			},
			InputMappings: []Mapping{
				{Target: "worker_id", Source: fromNode(PromotionNodeSnapshotWorker, "worker_id")},
				{Target: "expected_job_id", Source: fromInput("target_job_id")},
				{Target: "effective_date", Source: fromInput("effective_date")},
			},
			Capability: &CapabilityRef{
				ID: capDrift, Version: 1,
				OperationMode:   ModeSimulate,
				AuthorityScopes: []string{"scope:operations.read"},
			},
			Observe: &ObserveSpec{
				EvidenceKind:         EvidenceAuthoritativeRead,
				SourceAuthority:      "people.employment.projection",
				ExpectedStateFields:  []string{"expected_job_id"},
				RequiredWatermarks:   []string{"people.employment.stream_head"},
				MaxAgeSeconds:        300,
				ComparisonProfile:    "comparison.employment.job_assignment/v1",
				RetryExhaustionRoute: PromotionNodeEndDegraded,
			},
			Retry:      &RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.observation.bounded/v1"},
			Governance: governedInvocation(nil, nil, RevalidatePreExecution),
		},
		{
			ID:            PromotionNodeEndSimulated,
			Type:          StepEnd,
			Inputs:        terminalInputs(Field{Path: "drift_detected", Type: boolean()}),
			InputMappings: terminalMappings("SIMULATION_COMPLETE", Mapping{Target: "drift_detected", Source: fromNode(PromotionNodeObserveDrift, "drift_detected")}),
			End: &EndSpec{
				TerminalCode:      "SIMULATION_COMPLETE",
				RuntimeStatus:     RuntimeCompleted,
				CompletionMapping: completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "NOT_APPLICABLE", "SATISFIED"),
			},
			Governance: terminalGovernance([]string{PromotionObligationEvidence}, []string{PromotionApprovalManager}),
		},
		{
			ID:     PromotionNodeEndApproval,
			Type:   StepEnd,
			Inputs: terminalInputs(Field{Path: "proposal_digest", Type: plainStr()}),
			InputMappings: terminalMappings("SIMULATION_APPROVAL_REQUIRED",
				Mapping{Target: "proposal_digest", Source: fromNode(PromotionNodeBuildProposal, "proposal_digest")}),
			End: &EndSpec{
				TerminalCode:              "SIMULATION_APPROVAL_REQUIRED",
				RuntimeStatus:             RuntimeCompleted,
				CompletionMapping:         completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "NOT_APPLICABLE", "PENDING"),
				OutstandingObligationRefs: []string{PromotionObligationApproval},
			},
			Governance: terminalGovernance(
				[]string{PromotionObligationApproval, PromotionObligationEvidence},
				[]string{PromotionApprovalManager, PromotionApprovalFinance},
			),
		},
		{
			ID:            PromotionNodeEndDegraded,
			Type:          StepEnd,
			Inputs:        terminalInputs(),
			InputMappings: terminalMappings("SIMULATION_DEGRADED"),
			End: &EndSpec{
				TerminalCode:              "SIMULATION_DEGRADED",
				RuntimeStatus:             RuntimeBlocked,
				CompletionMapping:         completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{PromotionObligationEvidence},
			},
			Governance: terminalGovernance([]string{PromotionObligationEvidence}, nil),
		},
		{
			ID:            PromotionNodeEndUnknown,
			Type:          StepEnd,
			Inputs:        terminalInputs(),
			InputMappings: terminalMappings("SIMULATION_UNKNOWN"),
			End: &EndSpec{
				TerminalCode:              "SIMULATION_UNKNOWN",
				RuntimeStatus:             RuntimeBlocked,
				CompletionMapping:         completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
				OutstandingObligationRefs: []string{PromotionObligationEvidence},
			},
			Governance: terminalGovernance([]string{PromotionObligationEvidence}, nil),
		},
		{
			ID:            PromotionNodeEndRejected,
			Type:          StepEnd,
			Inputs:        terminalInputs(),
			InputMappings: terminalMappings("SIMULATION_REJECTED"),
			End: &EndSpec{
				TerminalCode:      "SIMULATION_REJECTED",
				RuntimeStatus:     RuntimeCompleted,
				CompletionMapping: completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
			},
			Governance: terminalGovernance([]string{PromotionObligationEvidence}, nil),
		},
	}
}

func promotionEdges() []Edge {
	capabilityRoutes := func(from, success string) []Edge {
		return []Edge{
			{From: from, To: success, RouteKey: string(OutcomeSucceeded)},
			{From: from, To: PromotionNodeEndRejected, RouteKey: string(OutcomeRejected)},
			{From: from, To: PromotionNodeEndUnknown, RouteKey: string(OutcomeUnknown)},
			{From: from, To: PromotionNodeEndUnknown, RouteKey: string(OutcomeAmbiguous)},
		}
	}
	edges := capabilityRoutes(PromotionNodeSnapshotWorker, PromotionNodeSimulateComp)
	edges = append(edges, capabilityRoutes(PromotionNodeSimulateComp, PromotionNodeEvaluateBand)...)
	edges = append(edges, capabilityRoutes(PromotionNodeEvaluateBand, PromotionNodeBuildProposal)...)
	edges = append(edges,
		Edge{From: PromotionNodeBuildProposal, To: PromotionNodeRaiseThreshold, RouteKey: string(OutcomeSucceeded)},
		Edge{From: PromotionNodeBuildProposal, To: PromotionNodeEndUnknown, RouteKey: string(OutcomeFailed)},

		Edge{From: PromotionNodeRaiseThreshold, To: PromotionNodeObserveDrift, RouteKey: "WITHIN_THRESHOLD"},
		Edge{From: PromotionNodeRaiseThreshold, To: PromotionNodeEndApproval, RouteKey: "EXCEEDS_THRESHOLD"},
		Edge{From: PromotionNodeRaiseThreshold, To: PromotionNodeEndUnknown, RouteKey: string(OutcomeUnknown)},

		Edge{From: PromotionNodeObserveDrift, To: PromotionNodeEndSimulated, RouteKey: string(OutcomePass)},
		Edge{From: PromotionNodeObserveDrift, To: PromotionNodeEndDegraded, RouteKey: string(OutcomeFail)},
		Edge{From: PromotionNodeObserveDrift, To: PromotionNodeEndDegraded, RouteKey: string(OutcomePartial)},
		Edge{From: PromotionNodeObserveDrift, To: PromotionNodeEndUnknown, RouteKey: string(OutcomeUnknown)},
	)
	return edges
}

// CompilePromotionReference compiles the reference workflow against a
// capability registry. It is the one call a fixture, a conformance suite or a
// runtime bootstrap needs.
func CompilePromotionReference(registry CapabilityResolver) (*CompiledWorkflow, error) {
	return Compile(PromotionReferenceDefinition(), Options{
		Phase:        PhaseP1A,
		Capabilities: registry,
	})
}

// CompilePromotionReferenceWithRules compiles the current version of the
// reference workflow against an immutable rule payload resolver. Frozen v1
// keeps its original two-input DECISION contract; v2 carries every threshold
// factor as a declared, digest-pinned input and resolves the aliased table.
func CompilePromotionReferenceWithRules(registry CapabilityResolver, references ReferenceResolver) (*CompiledWorkflow, error) {
	definition := PromotionReferenceV2Definition()
	opts, err := PromotionReferenceV2Options(registry, references)
	if err != nil {
		return nil, err
	}
	return Compile(definition, opts)
}

// PromotionReferenceV2Definition returns the current version of the reference
// workflow. The original PromotionReferenceDefinition remains the frozen v1
// publication; callers publishing or reproducing the simulator's current plan
// must use this definition and its matching compilation options.
func PromotionReferenceV2Definition() Definition {
	definition := PromotionReferenceDefinition()
	definition.Version = PromotionVersion + 1
	definition.InputSchema = workflowSchema("PromoteIntoManagementInputV2")
	definition.Inputs = append(definition.Inputs,
		Field{Path: "budget_authority", Type: plainStr()},
		Field{Path: "grade_change", Type: boolean()},
	)
	for i := range definition.Nodes {
		node := &definition.Nodes[i]
		if node.ID == PromotionNodeBuildProposal && node.Transform != nil {
			node.Transform.ProgramRef = &VersionedRef{ID: PromotionDecisionFactsIRID, Version: PromotionDecisionFactsIRVer}
			continue
		}
		if node.ID != PromotionNodeRaiseThreshold || node.Decision == nil {
			continue
		}
		node.InputSchema = workflowSchema("RaiseThresholdInputV2")
		node.Inputs = append(node.Inputs,
			Field{Path: "increase_percent", Type: decimal()},
			Field{Path: "budget_authority", Type: plainStr()},
			Field{Path: "grade_change", Type: boolean()},
		)
		node.InputMappings = append(node.InputMappings,
			Mapping{Target: "increase_percent", Source: fromNode(PromotionNodeBuildProposal, "raise_ratio")},
			Mapping{Target: "budget_authority", Source: fromInput("budget_authority")},
			Mapping{Target: "grade_change", Source: fromInput("grade_change")},
		)
		node.Decision.RuleVersion = "v3"
	}
	return definition
}

// PromotionReferenceV2Options binds the exact capability and reference
// authorities required by PromotionReferenceV2Definition.
func PromotionReferenceV2Options(registry CapabilityResolver, references ReferenceResolver) (Options, error) {
	schemas, err := DeclaredSchemaResolver(PromotionReferenceV2Definition())
	if err != nil {
		return Options{}, err
	}
	return Options{
		Phase: PhaseP1A, Capabilities: registry,
		References: ComposeReferenceResolvers(references, schemas),
	}, nil
}

// promotionCapabilityIDs is the exact set of capability versions the reference
// workflow binds. A registry that publishes these can compile it.
func promotionCapabilityIDs() []capability.Key {
	return []capability.Key{
		{ID: "hcmnext.people.explain_worker_state", Version: 1},
		{ID: "hcmnext.rewards.simulate_compensation", Version: 1},
		{ID: "hcmnext.rewards.evaluate_pay_band_position", Version: 1},
		{ID: "hcmnext.operations.detect_drift", Version: 1},
	}
}

// PromotionCapabilities lists the capability versions the reference workflow
// binds, so a caller can prove its registry publishes all of them before
// compiling.
func PromotionCapabilities() []capability.Key { return promotionCapabilityIDs() }
