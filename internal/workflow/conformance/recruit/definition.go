package recruit

import (
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
)

// Workflow identity.
const (
	WorkflowID = "hcmnext.workflows.recruit_hire_onboard"
	Version    = 1
)

// Node ids. Exported so conformance tests can name the exact path a golden
// walk takes without re-deriving it from the definition.
const (
	NodeReadPerson      = "read_person_registry"
	NodeReadCapacity    = "read_position_budget"
	NodeVerifyOffer     = "verify_offer_binding"
	NodeObserveOffer    = "observe_offer_clock"
	NodeVerifyWorkAuth  = "verify_work_authorization"
	NodeCheckReadiness  = "check_downstream_readiness"
	NodeBuildProposal   = "build_employment_proposal"
	NodeRouteHire       = "route_hire_decision"
	NodeEndHired        = "end_hired_pending_start"
	NodeEndDuplicate    = "end_duplicate_blocked"
	NodeEndOffer        = "end_offer_blocked"
	NodeEndOfferExpired = "end_offer_expired"
	NodeEndCapacity     = "end_capacity_blocked"
	NodeEndAuth         = "end_work_auth_blocked"
	NodeEndDegraded     = "end_degraded_repair"
	NodeEndRefused      = "end_step_refused"
	NodeEndUnknown      = "end_unknown"
)

// Decision route keys the hire-routing DECISION declares.
const (
	RouteHired             = "HIRED"
	RouteDuplicate         = "BLOCKED_DUPLICATE"
	RouteOffer             = "BLOCKED_OFFER"
	RouteCapacity          = "BLOCKED_CAPACITY"
	RouteWorkAuth          = "BLOCKED_WORK_AUTH"
	RouteDegraded          = "DEGRADED_START"
	RuleHireRoute          = "rules.recruit.hire_route/v1"
	TransformHireFootprint = "transforms.recruit.hire_footprint"
	TransformHireProposal  = "transforms.recruit.hire_proposal"
)

// Capability identities. They are this package's own fixture capabilities,
// never a bootstrap or domain capability: REV-020-01 proves the workflow's
// composition, not a real ATS, HRIS, payroll or IAM integration.
const (
	CapReadPerson     = "hcmnext.conformance.recruit.read_person"
	CapReadCapacity   = "hcmnext.conformance.recruit.read_capacity"
	CapVerifyOffer    = "hcmnext.conformance.recruit.verify_offer"
	CapObserveOffer   = "hcmnext.conformance.recruit.observe_offer"
	CapVerifyWorkAuth = "hcmnext.conformance.recruit.verify_work_auth"
	CapCheckReadiness = "hcmnext.conformance.recruit.check_readiness"
)

// Approval requirement ids: the reference workflow names the hiring-manager
// offer approval and the HRBP start authorization.
const (
	ApprovalHiringManager = "approval.recruit.hiring_manager"
	ApprovalHRBP          = "approval.recruit.hrbp"
)

// Obligation ids: the position/budget hold is the reservation leg, the
// employment creation is the hire effect graph, and evidence retention keeps
// the simulation artifact checkable.
const (
	ObligationPositionHold = "obligation.recruit.position_hold"
	ObligationEmployment   = "obligation.recruit.employment_creation"
	ObligationEvidence     = "obligation.recruit.simulation_evidence_retained"
)

// Terminal codes reported by the END nodes.
const (
	TerminalHired        = "RECRUIT_SIMULATION_HIRED_PENDING_START"
	TerminalDuplicate    = "RECRUIT_DUPLICATE_BLOCKED"
	TerminalOffer        = "RECRUIT_OFFER_BLOCKED"
	TerminalOfferExpired = "RECRUIT_OFFER_EXPIRED"
	TerminalCapacity     = "RECRUIT_CAPACITY_BLOCKED"
	TerminalWorkAuth     = "RECRUIT_WORK_AUTH_BLOCKED"
	TerminalDegraded     = "RECRUIT_SIMULATION_DEGRADED"
	TerminalRefused      = "RECRUIT_STEP_REFUSED"
	TerminalUnknown      = "RECRUIT_SIMULATION_UNKNOWN"
)

const (
	organizationScope  = "acme/workforce"
	purpose            = "SIMULATE_RECRUIT_HIRE_ONBOARD"
	classification     = "CONFIDENTIAL_HR"
	dataAccessManifest = "dam.recruit.simulation/v1"
	mappingDigest      = "hcmnext.workflow.InputMappingSet/v1"
	repairRef          = "repair.recruit.downstream_or_offer_drift/v1"
)

// Offer clock states reported by the offer observation.
const (
	OfferStateValid   = "OFFER_VALID"
	OfferStateExpired = "OFFER_EXPIRED"
)

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

// ReferenceDefinition returns the P1A Recruit/Hire/Onboard reference workflow
// as typed Go values: person-registry read, position/budget read, offer
// binding verification, an offer-clock observation (the executable leg of the
// offer-expiry window), work-authorization document verification, downstream
// readiness, a bound employment proposal and a routing decision with one
// terminal per outcome class.
func ReferenceDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        WorkflowID,
		Version:           Version,
		Name:              "Recruit, hire and onboard (simulation)",
		InputSchema:       workflowSchema("RecruitHireOnboardInput"),
		OutputSchema:      workflowSchema("RecruitHireOnboardResult"),
		VariablesSchema:   workflowSchema("RecruitHireOnboardVariables"),
		TenantScope:       "acme",
		OrganizationScope: organizationScope,
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeSimulate},
		TerminalProfile:   workflow.TerminalProfileSimulateOnly,
		StartNodeID:       NodeReadPerson,

		Inputs: []workflow.Field{
			{Path: "candidate_id", Type: str("CandidateID")},
			{Path: "offer_id", Type: str("OfferID")},
			{Path: "target_position_id", Type: str("PositionID")},
			{Path: "start_date", Type: localDate()},
		},
		Outputs: []workflow.Field{
			{Path: "candidate_id", Type: str("CandidateID")},
			{Path: "terminal_code", Type: plainStr()},
		},

		Limits: workflow.Limits{MaxFanOut: 8, MaxDepth: 16, MaxNodes: 32},

		FailurePolicyRef:      "policy.workflow.failure.simulation/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.simulation/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.hr-simulation/v1",

		ApprovalRequirements: []workflow.ApprovalRequirement{
			{ID: ApprovalHiringManager, ResolverExpression: "HiringManagerOf(target_position)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalHRBP, ResolverExpression: "HRBPFor(target_position)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
		},

		Obligations: []workflow.ObligationRequirement{
			{
				ID: ObligationPositionHold, Authority: "customer.policy.workforce.recruit",
				InsertionPoint:   workflow.InsertSimulation,
				RequiredAction:   "Hold the target position and its budget line against the bound employment proposal",
				ResponsibleParty: "workforce.recruit.coordinator", SatisfactionCondition: "proposal digest cites one held position and one held budget line",
				SourceVersion: RuleHireRoute, ReevaluationPolicy: workflow.ReevalReevaluate, Mandatory: true,
			},
			{
				ID: ObligationEmployment, Authority: "customer.policy.workforce.hire",
				InsertionPoint: workflow.InsertSimulation,
				RequiredAction: "Compose the employment creation effect graph for the hired candidate", ResponsibleParty: "workforce.hire.coordinator", SatisfactionCondition: "employment effect graph cites the proposal digest and the start date",
				SourceVersion: RuleHireRoute, ReevaluationPolicy: workflow.ReevalReevaluate, Mandatory: true,
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

func capabilityNode(id, cap string, scopes []string, inputs, outputs []workflow.Field, mappings []workflow.Mapping) workflow.Node {
	return workflow.Node{
		ID:            id,
		Type:          workflow.StepCapability,
		InputSchema:   bootstrapCapabilitySchema(cap, "request"),
		OutputSchema:  bootstrapCapabilitySchema(cap, "response"),
		Inputs:        inputs,
		Outputs:       outputs,
		InputMappings: mappings,
		Capability: &workflow.CapabilityRef{
			ID: cap, Version: 1, OperationMode: workflow.ModeSimulate,
			AuthorityScopes: scopes,
		},
		Governance: governedInvocation(nil, nil, organizationScope),
	}
}

func nodes() []workflow.Node {
	return []workflow.Node{
		capabilityNode(NodeReadPerson, CapReadPerson,
			[]string{"scope:people.read"},
			[]workflow.Field{
				{Path: "candidate_id", Type: str("CandidateID")},
			},
			[]workflow.Field{
				{Path: "person_id", Type: str("PersonID")},
				{Path: "duplicate_person", Type: boolean()},
			},
			[]workflow.Mapping{
				{Target: "candidate_id", Source: builders.FromInput("candidate_id")},
			},
		),
		capabilityNode(NodeReadCapacity, CapReadCapacity,
			[]string{"scope:workforce.read"},
			[]workflow.Field{
				{Path: "target_position_id", Type: str("PositionID")},
				{Path: "start_date", Type: localDate()},
			},
			[]workflow.Field{
				{Path: "position_available", Type: boolean()},
				{Path: "budget_available", Type: boolean()},
			},
			[]workflow.Mapping{
				{Target: "target_position_id", Source: builders.FromInput("target_position_id")},
				{Target: "start_date", Source: builders.FromInput("start_date")},
			},
		),
		capabilityNode(NodeVerifyOffer, CapVerifyOffer,
			[]string{"scope:offers.read"},
			[]workflow.Field{
				{Path: "offer_id", Type: str("OfferID")},
				{Path: "candidate_id", Type: str("CandidateID")},
			},
			[]workflow.Field{
				{Path: "offer_approved", Type: boolean()},
				{Path: "offer_accepted", Type: boolean()},
			},
			[]workflow.Mapping{
				{Target: "offer_id", Source: builders.FromInput("offer_id")},
				{Target: "candidate_id", Source: builders.FromInput("candidate_id")},
			},
		),
		{
			ID:           NodeObserveOffer,
			Type:         workflow.StepObserve,
			InputSchema:  bootstrapCapabilitySchema(CapObserveOffer, "request"),
			OutputSchema: bootstrapCapabilitySchema(CapObserveOffer, "response"),
			Inputs: []workflow.Field{
				{Path: "offer_id", Type: str("OfferID")},
				{Path: "start_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "offer_state", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "offer_id", Source: builders.FromInput("offer_id")},
				{Target: "start_date", Source: builders.FromInput("start_date")},
			},
			Capability: &workflow.CapabilityRef{
				ID: CapObserveOffer, Version: 1, OperationMode: workflow.ModeSimulate,
				AuthorityScopes: []string{"scope:offers.read"},
			},
			Observe: &workflow.ObserveSpec{
				EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "recruiting.offer.projection",
				ExpectedStateFields: []string{"offer_id"}, RequiredWatermarks: []string{"recruiting.offer.stream_head"},
				MaxAgeSeconds: 300, ComparisonProfile: "comparison.recruit.offer_expiry/v1",
				RetryExhaustionRoute: NodeEndDegraded,
			},
			Retry:      &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.observation.bounded/v1"},
			Governance: governedInvocation(nil, nil, organizationScope),
		},
		capabilityNode(NodeVerifyWorkAuth, CapVerifyWorkAuth,
			[]string{"scope:compliance.read"},
			[]workflow.Field{
				{Path: "candidate_id", Type: str("CandidateID")},
				{Path: "start_date", Type: localDate()},
			},
			[]workflow.Field{
				{Path: "work_auth_valid", Type: boolean()},
				{Path: "work_auth_evidence_refs", Type: plainStr()},
			},
			[]workflow.Mapping{
				{Target: "candidate_id", Source: builders.FromInput("candidate_id")},
				{Target: "start_date", Source: builders.FromInput("start_date")},
			},
		),
		capabilityNode(NodeCheckReadiness, CapCheckReadiness,
			[]string{"scope:operations.read"},
			[]workflow.Field{
				{Path: "candidate_id", Type: str("CandidateID")},
				{Path: "start_date", Type: localDate()},
			},
			[]workflow.Field{
				{Path: "iam_ready", Type: boolean()},
				{Path: "payroll_ready", Type: boolean()},
				{Path: "equipment_ready", Type: boolean()},
				{Path: "learning_ready", Type: boolean()},
				{Path: "all_ready", Type: boolean()},
			},
			[]workflow.Mapping{
				{Target: "candidate_id", Source: builders.FromInput("candidate_id")},
				{Target: "start_date", Source: builders.FromInput("start_date")},
			},
		),
		{
			ID:           NodeBuildProposal,
			Type:         workflow.StepTransform,
			InputSchema:  workflowSchema("RecruitProposalDraft"),
			OutputSchema: workflowSchema("RecruitProposal"),
			Inputs: []workflow.Field{
				{Path: "candidate_id", Type: str("CandidateID")},
				{Path: "offer_id", Type: str("OfferID")},
				{Path: "target_position_id", Type: str("PositionID")},
				{Path: "person_id", Type: str("PersonID")},
				{Path: "offer_state", Type: plainStr()},
				{Path: "work_auth_evidence_refs", Type: plainStr()},
				{Path: "start_date", Type: localDate()},
			},
			Outputs: []workflow.Field{
				{Path: "proposal_digest", Type: plainStr()},
			},
			InputMappings: []workflow.Mapping{
				{Target: "candidate_id", Source: builders.FromInput("candidate_id")},
				{Target: "offer_id", Source: builders.FromInput("offer_id")},
				{Target: "target_position_id", Source: builders.FromInput("target_position_id")},
				{Target: "person_id", Source: builders.FromNode(NodeReadPerson, "person_id")},
				{Target: "offer_state", Source: builders.FromNode(NodeObserveOffer, "offer_state")},
				{Target: "work_auth_evidence_refs", Source: builders.FromNode(NodeVerifyWorkAuth, "work_auth_evidence_refs")},
				{Target: "start_date", Source: builders.FromInput("start_date")},
			},
			Transform: &workflow.TransformSpec{
				TransformRef: TransformHireProposal, Version: 1,
				NormalizationProfile: "hcmnext.canonical.recruit_proposal/v1",
				InputTaint: []workflow.TaintInput{
					{Source: "candidate_id", Level: workflow.TaintTrusted},
					{Source: "offer_state", Level: workflow.TaintTrusted},
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
			ID:           NodeRouteHire,
			Type:         workflow.StepDecision,
			InputSchema:  workflowSchema("RecruitRouteInput"),
			OutputSchema: workflowSchema("RecruitRouteResult"),
			Inputs: []workflow.Field{
				{Path: "duplicate_person", Type: boolean()},
				{Path: "offer_approved", Type: boolean()},
				{Path: "offer_accepted", Type: boolean()},
				{Path: "position_available", Type: boolean()},
				{Path: "budget_available", Type: boolean()},
				{Path: "work_auth_valid", Type: boolean()},
				{Path: "all_ready", Type: boolean()},
				{Path: "proposal_digest", Type: plainStr()},
			},
			Outputs: []workflow.Field{{Path: "route_key", Type: plainStr()}},
			InputMappings: []workflow.Mapping{
				{Target: "duplicate_person", Source: builders.FromNode(NodeReadPerson, "duplicate_person")},
				{Target: "offer_approved", Source: builders.FromNode(NodeVerifyOffer, "offer_approved")},
				{Target: "offer_accepted", Source: builders.FromNode(NodeVerifyOffer, "offer_accepted")},
				{Target: "position_available", Source: builders.FromNode(NodeReadCapacity, "position_available")},
				{Target: "budget_available", Source: builders.FromNode(NodeReadCapacity, "budget_available")},
				{Target: "work_auth_valid", Source: builders.FromNode(NodeVerifyWorkAuth, "work_auth_valid")},
				{Target: "all_ready", Source: builders.FromNode(NodeCheckReadiness, "all_ready")},
				{Target: "proposal_digest", Source: builders.FromNode(NodeBuildProposal, "proposal_digest")},
			},
			Decision: &workflow.DecisionSpec{
				EvaluatorRef: "engines.rules.recruit_hire_route", EvaluatorVersion: 1,
				RuleRef: RuleHireRoute, InputDigestProfile: mappingDigest,
				Routes: []workflow.DecisionRoute{
					{Key: RouteDuplicate, Predicate: "person_already_employed", Precedence: 10},
					{Key: RouteOffer, Predicate: "offer_not_approved_or_accepted", Precedence: 20},
					{Key: RouteCapacity, Predicate: "position_or_budget_exhausted", Precedence: 30},
					{Key: RouteWorkAuth, Predicate: "work_authorization_missing", Precedence: 40},
					{Key: RouteDegraded, Predicate: "downstream_not_ready", Precedence: 50},
					{Key: RouteHired, Predicate: "all_gates_clear", Precedence: 60},
				},
				DefaultRoute: RouteHired,
			},
			Governance: workflow.NodeGovernance{
				Purpose: purpose, Classification: classification,
				RevalidationBoundary: workflow.RevalidateNone, DataAccessManifestRef: dataAccessManifest,
			},
		},
		endNode(NodeEndHired, TerminalHired, workflow.RuntimeCompleted,
			builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "PENDING_OBSERVATION", "PENDING"),
			[]string{ObligationPositionHold, ObligationEmployment, ObligationEvidence},
			[]string{ApprovalHiringManager, ApprovalHRBP}, nil),
		endNode(NodeEndDuplicate, TerminalDuplicate, workflow.RuntimeCompleted,
			builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "PENDING"),
			[]string{ObligationEvidence}, nil, nil),
		endNode(NodeEndOffer, TerminalOffer, workflow.RuntimeCompleted,
			builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "PENDING"),
			[]string{ObligationEvidence}, nil, nil),
		endNode(NodeEndOfferExpired, TerminalOfferExpired, workflow.RuntimeCompleted,
			builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "PENDING"),
			[]string{ObligationEvidence}, nil, nil),
		endNode(NodeEndCapacity, TerminalCapacity, workflow.RuntimeCompleted,
			builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "PENDING"),
			[]string{ObligationEvidence}, nil, nil),
		endNode(NodeEndAuth, TerminalWorkAuth, workflow.RuntimeCompleted,
			builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "PENDING"),
			[]string{ObligationEvidence}, nil, nil),
		endNode(NodeEndDegraded, TerminalDegraded, workflow.RuntimeBlocked,
			builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
			[]string{ObligationEmployment, ObligationEvidence}, nil, []string{repairRef}),
		endNode(NodeEndRefused, TerminalRefused, workflow.RuntimeBlocked,
			builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "PENDING"),
			[]string{ObligationEvidence}, nil, nil),
		endNode(NodeEndUnknown, TerminalUnknown, workflow.RuntimeBlocked,
			builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"),
			[]string{ObligationEvidence}, nil, nil),
	}
}

func endNode(id, code string, status workflow.RuntimeStatus, completion map[string]string, obligations, approvals, repairs []string) workflow.Node {
	return workflow.Node{
		ID:            id,
		Type:          workflow.StepEnd,
		Inputs:        builders.TerminalInputs("candidate_id", "CandidateID"),
		InputMappings: builders.TerminalMappings("candidate_id", code),
		End: &workflow.EndSpec{
			TerminalCode: code, RuntimeStatus: status,
			CompletionMapping:         completion,
			OutstandingObligationRefs: obligations,
			RepairRefs:                repairs,
		},
		Governance: terminalGovernance(obligations, approvals),
	}
}

func edges() []workflow.Edge {
	capabilityRoutes := func(from, success string) []workflow.Edge {
		return []workflow.Edge{
			{From: from, To: success, RouteKey: string(workflow.OutcomeSucceeded)},
			{From: from, To: NodeEndRefused, RouteKey: string(workflow.OutcomeRejected)},
			{From: from, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},
			{From: from, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeAmbiguous)},
		}
	}
	out := capabilityRoutes(NodeReadPerson, NodeReadCapacity)
	out = append(out, capabilityRoutes(NodeReadCapacity, NodeVerifyOffer)...)
	out = append(out, capabilityRoutes(NodeVerifyOffer, NodeObserveOffer)...)
	out = append(out,
		workflow.Edge{From: NodeObserveOffer, To: NodeVerifyWorkAuth, RouteKey: string(workflow.OutcomePass)},
		workflow.Edge{From: NodeObserveOffer, To: NodeEndOfferExpired, RouteKey: string(workflow.OutcomeFail)},
		workflow.Edge{From: NodeObserveOffer, To: NodeEndDegraded, RouteKey: string(workflow.OutcomePartial)},
		workflow.Edge{From: NodeObserveOffer, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},
	)
	out = append(out, capabilityRoutes(NodeVerifyWorkAuth, NodeCheckReadiness)...)
	out = append(out, capabilityRoutes(NodeCheckReadiness, NodeBuildProposal)...)
	out = append(out,
		workflow.Edge{From: NodeBuildProposal, To: NodeRouteHire, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeBuildProposal, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)},

		workflow.Edge{From: NodeRouteHire, To: NodeEndHired, RouteKey: RouteHired},
		workflow.Edge{From: NodeRouteHire, To: NodeEndDuplicate, RouteKey: RouteDuplicate},
		workflow.Edge{From: NodeRouteHire, To: NodeEndOffer, RouteKey: RouteOffer},
		workflow.Edge{From: NodeRouteHire, To: NodeEndCapacity, RouteKey: RouteCapacity},
		workflow.Edge{From: NodeRouteHire, To: NodeEndAuth, RouteKey: RouteWorkAuth},
		workflow.Edge{From: NodeRouteHire, To: NodeEndDegraded, RouteKey: RouteDegraded},
		workflow.Edge{From: NodeRouteHire, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},
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
func CapabilityIDs() []string {
	return []string{
		CapReadPerson,
		CapReadCapacity,
		CapVerifyOffer,
		CapObserveOffer,
		CapVerifyWorkAuth,
		CapCheckReadiness,
	}
}
