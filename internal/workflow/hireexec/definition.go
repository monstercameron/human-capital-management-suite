package hireexec

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
)

// Workflow identity.
const (
	WorkflowID = "hcmnext.workflows.new_hire"
	Version    = 1
)

// Node ids, exported so a caller (the driver's StepRunner/WorkItemFactory and
// this package's own tests) can name the exact node without re-deriving it
// from the definition.
const (
	NodePrepareHire               = "prepare_hire"
	NodeApproveOffer              = "approve_offer"
	NodeAwaitBackgroundCheck      = "await_background_check"
	NodeEvaluateBackgroundCheck   = "evaluate_background_check"
	NodeReviewAdverseResult       = "review_adverse_result"
	NodeCollectNewHireForms       = "collect_new_hire_forms"
	NodeProvisionITAccess         = "provision_system_access"
	NodeProvisionWorkspace        = "provision_workspace"
	NodeEnrollPayroll             = "enroll_payroll"
	NodeAwaitStartDate            = "await_start_date"
	NodeCommitHire                = "commit_hire"
	NodeEndHired                  = "end_hired"
	NodeEndOfferRejected          = "end_offer_rejected"
	NodeEndOfferWithdrawn         = "end_offer_withdrawn"
	NodeEndBackgroundCheckExpired = "end_background_check_expired"
	NodeEndCancelled              = "end_cancelled"
	NodeEndExpired                = "end_expired"
	NodeEndInvalidated            = "end_invalidated"
	NodeEndFailed                 = "end_failed"
)

// RouteBackgroundCheckClear and RouteBackgroundCheckAdverse are the two
// author-declared routes evaluate_background_check's DECISION adds beyond the
// kernel's fixed UNKNOWN outcome (WF-STEP-002's DECISION contract, see
// [workflow.ConformanceFor]).
const (
	RouteBackgroundCheckClear   = "CLEAR"
	RouteBackgroundCheckAdverse = "ADVERSE"
)

// ApprovalHiringManager is the one approval requirement this graph declares:
// the hiring manager's own offer approval.
const ApprovalHiringManager = "approval.new_hire.hiring_manager/v1"

// Work types the human-work nodes bind. review_adverse_result is HR's own
// judgment call; the other three are the sequential provisioning legs
// WF-EXT-019 explains cannot fan out.
const (
	WorkTypeReviewAdverseResult = "task.new_hire.review_adverse_result/v1"
	WorkTypeNewHireForms        = "task.new_hire.collect_forms/v1"
	WorkTypeProvisionITAccess   = "task.new_hire.provision_system_access/v1"
	WorkTypeProvisionWorkspace  = "task.new_hire.provision_workspace/v1"
	WorkTypeEnrollPayroll       = "task.new_hire.enroll_payroll/v1"
)

// Terminal codes reported by the END nodes.
const (
	TerminalHired                  = "NEW_HIRE_COMPLETE"
	TerminalOfferRejected          = "NEW_HIRE_OFFER_REJECTED"
	TerminalOfferWithdrawn         = "NEW_HIRE_OFFER_WITHDRAWN"
	TerminalBackgroundCheckExpired = "NEW_HIRE_BACKGROUND_CHECK_EXPIRED"
	TerminalCancelled              = "NEW_HIRE_CANCELLED"
	TerminalExpired                = "NEW_HIRE_EXPIRED"
	TerminalInvalidated            = "NEW_HIRE_INVALIDATED"
	TerminalFailed                 = "NEW_HIRE_FAILED"
)

// capCommitHire is this definition's one capability identity: the
// authoritative core write that actually creates the employment record. It is
// this package's own fixture identity, exactly as promotionexec's own
// capability ids are -- [capabilities] resolves it for [Compile] the same
// way promotionexec's own static table does, because the compiler
// unconditionally refuses any CAPABILITY node when no registry at all was
// supplied (internal/workflow/shape.go).
const capCommitHire = "hcmnext.people.commit_new_hire"

const (
	organizationScope  = "acme/people"
	purpose            = "NEW_HIRE_EXECUTE"
	classification     = "CONFIDENTIAL_HR"
	dataAccessManifest = "data-access.new_hire.execute/v1"
)

func schema(name string) workflow.SchemaRef {
	return workflow.SchemaRef{
		SchemaID:         "hcmnext.workflows.new_hire." + name + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.workflows.new_hire." + name,
	}
}

func capabilitySchema(id, slot string) workflow.SchemaRef {
	return workflow.SchemaRef{
		SchemaID:         id + "." + slot + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
	}
}

func brandedString(brand string) workflow.ValueType {
	return workflow.ValueType{Kind: workflow.KindString, Brand: brand}
}

func plainString() workflow.ValueType { return workflow.ValueType{Kind: workflow.KindString} }

func input(path string) workflow.Source {
	return builders.FromInput(path)
}

// candidateInputs is the one input field ([WF-EXT-004]'s inputs-not-resolved
// limit means every node's InputMappings exist to satisfy the compiler's
// typed dataflow proof, never to hand a runtime StepRunner anything it
// actually reads) every node in this graph carries: the candidate identity
// the terminal write ultimately reports.
func candidateInputs() []workflow.Field {
	return []workflow.Field{{Path: "candidate_id", Type: brandedString("CandidateID")}}
}

func candidateMappings() []workflow.Mapping {
	return []workflow.Mapping{{Target: "candidate_id", Source: input("candidate_id")}}
}

func nonCapabilityGovernance(approvals []string, boundary workflow.RevalidationBoundary) workflow.NodeGovernance {
	return workflow.NodeGovernance{
		Purpose:               purpose,
		Classification:        classification,
		ApprovalRequirements:  approvals,
		RevalidationBoundary:  boundary,
		DataAccessManifestRef: dataAccessManifest,
	}
}

func capabilityGovernance(boundary workflow.RevalidationBoundary) workflow.NodeGovernance {
	return workflow.NodeGovernance{
		Purpose:        purpose,
		Classification: classification,
		RequiredDecisions: []workflow.GovernanceKind{
			workflow.GovernanceAuthZ, workflow.GovernanceLegal, workflow.GovernancePurpose, workflow.GovernanceRisk,
		},
		RevalidationBoundary:  boundary,
		DataAccessManifestRef: dataAccessManifest,
	}
}

func terminalNode(id, code string, status workflow.RuntimeStatus, dims map[string]string, repair bool) workflow.Node {
	end := &workflow.EndSpec{TerminalCode: code, RuntimeStatus: status, CompletionMapping: dims}
	if repair {
		end.RepairRefs = []string{"repair.new_hire.execute/v1"}
	}
	if id == NodeEndHired {
		end.CommitReceiptRef = "receipt.new_hire.execute/v1"
	}
	return workflow.Node{
		ID:             id,
		Type:           workflow.StepEnd,
		Inputs:         builders.TerminalInputs("candidate_id", "CandidateID"),
		InputMappings:  builders.TerminalMappings("candidate_id", code),
		Governance:     nonCapabilityGovernance(nil, workflow.RevalidatePreClosure),
		End:            end,
		DeclaredEffect: capability.EffectPure,
	}
}

// Definition returns the complete New Hire EXECUTE graph, version 1.
func Definition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        WorkflowID,
		Version:           Version,
		Name:              "New employee hire",
		InputSchema:       schema("Input"),
		OutputSchema:      schema("Result"),
		VariablesSchema:   schema("Variables"),
		TenantScope:       "acme",
		OrganizationScope: organizationScope,
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeExecute},
		TerminalProfile:   workflow.TerminalProfileExecute,
		StartNodeID:       NodePrepareHire,
		Inputs:            candidateInputs(),
		Outputs:           builders.TerminalInputs("candidate_id", "CandidateID"),
		ApprovalRequirements: []workflow.ApprovalRequirement{{
			ID: ApprovalHiringManager, ResolverExpression: "HiringManagerFor(candidate)",
			Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND",
		}},
		Limits:                workflow.Limits{MaxFanOut: 6, MaxDepth: 16, MaxNodes: 24},
		FailurePolicyRef:      "policy.workflow.failure.new_hire.execute/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.new_hire.execute/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.confidential-hr/v1",
		Nodes:                 hireNodes(),
		Edges:                 hireEdges(),
	}
}

func hireNodes() []workflow.Node {
	return []workflow.Node{
		{
			ID: NodePrepareHire, Type: workflow.StepTransform,
			InputSchema: schema("PrepareHireInput"), OutputSchema: schema("PrepareHireResult"),
			Inputs: candidateInputs(), InputMappings: candidateMappings(),
			Transform: &workflow.TransformSpec{
				TransformRef: "transform.new_hire.prepare/v1", Version: 1,
				NormalizationProfile: "profile.new_hire.prepare/v1", OutputTaint: workflow.TaintDerived,
				Limits: workflow.TransformLimits{
					MaxInputBytes: workflow.MaxTransformInputBytes, MaxOutputBytes: workflow.MaxTransformOutputBytes,
					MaxSteps: workflow.MaxTransformSteps,
				},
			},
			DeclaredEffect: capability.EffectPure,
			Governance:     nonCapabilityGovernance(nil, workflow.RevalidatePreExecution),
		},
		{
			ID: NodeApproveOffer, Type: workflow.StepApproval, DeclaredEffect: capability.EffectPure,
			InputSchema: schema("ApproveOfferInput"), OutputSchema: schema("ApproveOfferResult"),
			Inputs: candidateInputs(), InputMappings: candidateMappings(),
			Governance: nonCapabilityGovernance([]string{ApprovalHiringManager}, workflow.RevalidatePreExecution),
		},
		{
			// The background-check provider's callback is a durable SIGNAL,
			// not a synchronous capability call: the provider may take days.
			// WF-EXT-014: correlated on the closed start-time vocabulary
			// ("proposal.intent_id"), never on payload content.
			ID: NodeAwaitBackgroundCheck, Type: workflow.StepSignal, DeclaredEffect: capability.EffectPure,
			InputSchema: schema("AwaitBackgroundCheckInput"), OutputSchema: schema("AwaitBackgroundCheckResult"),
			Inputs: candidateInputs(), InputMappings: candidateMappings(),
			Signal: &workflow.SignalSpec{
				EventType:                "hcmnext.events.new_hire_background_check_result",
				CorrelationKeyExpression: "proposal.intent_id",
				ExpectedSchemaRef:        schema("BackgroundCheckResultPayload"),
				AcceptedSources:          []string{"hcmnext.integrations.background_check"},
				Ordering:                 workflow.SignalOrderingNone,
				CloseAfterSeconds:        1209600, // 14 days
			},
			FailureRoute: NodeEndFailed,
			Governance:   nonCapabilityGovernance(nil, workflow.RevalidatePreExecution),
		},
		{
			// The kernel SIGNAL vocabulary is fixed at SUCCEEDED/TIMED_OUT/
			// CANCELLED ([workflow.ConformanceFor]); it carries no business
			// verdict. This DECISION reads the accepted background-check
			// result and routes CLEAR/ADVERSE -- the business judgment the
			// SIGNAL step itself cannot express.
			ID: NodeEvaluateBackgroundCheck, Type: workflow.StepDecision,
			InputSchema: schema("EvaluateBackgroundCheckInput"), OutputSchema: schema("EvaluateBackgroundCheckResult"),
			Inputs: candidateInputs(), InputMappings: candidateMappings(),
			Decision: &workflow.DecisionSpec{
				EvaluatorRef: "engines.decisiontable", EvaluatorVersion: 1,
				RuleRef: "rules.new_hire.background_check_verdict/v1", InputDigestProfile: "hcmnext.workflow.InputMappingSet/v1",
				Routes: []workflow.DecisionRoute{
					{Key: RouteBackgroundCheckClear, Predicate: "background_check_clear", Precedence: 10},
					{Key: RouteBackgroundCheckAdverse, Predicate: "background_check_adverse", Precedence: 20},
				},
				DefaultRoute: RouteBackgroundCheckAdverse,
			},
			Governance: nonCapabilityGovernance(nil, workflow.RevalidateNone),
		},
		{
			ID: NodeReviewAdverseResult, Type: workflow.StepTask, DeclaredEffect: capability.EffectPure,
			InputSchema: schema("ReviewAdverseResultInput"), OutputSchema: schema("ReviewAdverseResultResult"),
			Inputs: candidateInputs(), InputMappings: candidateMappings(),
			Metadata:   map[string]string{"assignee": "HRBusinessPartnerFor(candidate)"},
			Governance: nonCapabilityGovernance(nil, workflow.RevalidatePreExecution),
		},
		{
			ID: NodeCollectNewHireForms, Type: workflow.StepTask, DeclaredEffect: capability.EffectPure,
			InputSchema: schema("CollectNewHireFormsInput"), OutputSchema: schema("CollectNewHireFormsResult"),
			Inputs: candidateInputs(), InputMappings: candidateMappings(),
			Metadata:   map[string]string{"assignee": "candidate", "forms": "identity,tax_withholding"},
			Governance: nonCapabilityGovernance(nil, workflow.RevalidatePreExecution),
		},
		{
			ID: NodeProvisionITAccess, Type: workflow.StepTask, DeclaredEffect: capability.EffectPure,
			InputSchema: schema("ProvisionITAccessInput"), OutputSchema: schema("ProvisionITAccessResult"),
			Inputs: candidateInputs(), InputMappings: candidateMappings(),
			Metadata:   map[string]string{"assignee": "ITProvisioningFor(candidate)"},
			Governance: nonCapabilityGovernance(nil, workflow.RevalidatePreExecution),
		},
		{
			ID: NodeProvisionWorkspace, Type: workflow.StepTask, DeclaredEffect: capability.EffectPure,
			InputSchema: schema("ProvisionWorkspaceInput"), OutputSchema: schema("ProvisionWorkspaceResult"),
			Inputs: candidateInputs(), InputMappings: candidateMappings(),
			Metadata:   map[string]string{"assignee": "FacilitiesProvisioningFor(candidate)"},
			Governance: nonCapabilityGovernance(nil, workflow.RevalidatePreExecution),
		},
		{
			ID: NodeEnrollPayroll, Type: workflow.StepTask, DeclaredEffect: capability.EffectPure,
			InputSchema: schema("EnrollPayrollInput"), OutputSchema: schema("EnrollPayrollResult"),
			Inputs: candidateInputs(), InputMappings: candidateMappings(),
			Metadata:   map[string]string{"assignee": "PayrollEnrollmentFor(candidate)"},
			Governance: nonCapabilityGovernance(nil, workflow.RevalidatePreExecution),
		},
		{
			// WF-EXT-012: WakeInstant here is an unused definition-time
			// placeholder. The real per-run wake instant -- the candidate's
			// start date -- always comes from the bound proposal's own
			// effective time, substituted by the caller's execute.TimerFactory
			// before the wake requirement is computed; see
			// test/workflow/hire_execute_test.go's hireTimerFactory.
			ID: NodeAwaitStartDate, Type: workflow.StepWait, SafePointRequested: true, DeclaredEffect: capability.EffectPure,
			InputSchema: schema("AwaitStartDateInput"), OutputSchema: schema("AwaitStartDateResult"),
			Inputs: candidateInputs(), InputMappings: candidateMappings(),
			Wait: &workflow.WaitSpec{
				WakeKind: workflow.WaitWakeAtInstant, WakeInstant: "1970-01-01T00:00:00Z",
				ZoneID: "America/New_York", ZoneTzdbVersion: "2026a",
				CalendarRef: "us-federal", CalendarVersion: "2026.1", ReferenceUpdatePolicy: "RECALCULATE",
			},
			FailureRoute: NodeEndFailed,
			Governance:   nonCapabilityGovernance(nil, workflow.RevalidatePreExecution),
		},
		{
			// WF-RUN-037: the one authoritative core write -- the employment
			// record actually comes into existence here. Everything before it
			// is preparation and governed human work; everything after it is
			// the COMPLETED terminal.
			ID: NodeCommitHire, Type: workflow.StepCapability, SafePointRequested: true,
			DeclaredEffect: capability.EffectInternalMutation, EffectRole: workflow.RoleAuthoritativeCore,
			InputSchema: capabilitySchema(capCommitHire, "request"), OutputSchema: capabilitySchema(capCommitHire, "response"),
			Inputs:        candidateInputs(),
			Outputs:       []workflow.Field{{Path: "candidate_id", Type: brandedString("CandidateID")}, {Path: "commit_receipt", Type: plainString()}},
			InputMappings: candidateMappings(),
			Capability: &workflow.CapabilityRef{
				ID: capCommitHire, Version: 1, OperationMode: workflow.ModeExecute,
				AuthorityScopes: []string{"scope:people.write"}, IdempotencyKeyMapping: "candidate_id",
				EffectBinding: "new_hire.core_commit",
			},
			FailureRoute: NodeEndFailed,
			Governance:   capabilityGovernance(workflow.RevalidatePreEffect),
		},
		terminalNode(NodeEndHired, TerminalHired, workflow.RuntimeCompleted,
			builders.Completion("CLOSED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"), false),
		terminalNode(NodeEndOfferRejected, TerminalOfferRejected, workflow.RuntimeCompleted,
			builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), false),
		terminalNode(NodeEndOfferWithdrawn, TerminalOfferWithdrawn, workflow.RuntimeCompleted,
			builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), false),
		terminalNode(NodeEndBackgroundCheckExpired, TerminalBackgroundCheckExpired, workflow.RuntimeCancelled,
			builders.Completion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), false),
		terminalNode(NodeEndCancelled, TerminalCancelled, workflow.RuntimeCancelled,
			builders.Completion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), false),
		terminalNode(NodeEndExpired, TerminalExpired, workflow.RuntimeCancelled,
			builders.Completion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), false),
		terminalNode(NodeEndInvalidated, TerminalInvalidated, workflow.RuntimeSuperseded,
			builders.Completion("SUPERSEDED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), false),
		terminalNode(NodeEndFailed, TerminalFailed, workflow.RuntimeRepairRequired,
			builders.Completion("APPROVED", "REPAIR_REQUIRED", "UNKNOWN", "DEGRADED", "PENDING"), true),
	}
}

func hireEdges() []workflow.Edge {
	return []workflow.Edge{
		{From: NodePrepareHire, To: NodeApproveOffer, RouteKey: string(workflow.OutcomeSucceeded)},
		{From: NodePrepareHire, To: NodeEndFailed, RouteKey: string(workflow.OutcomeFailed)},

		{From: NodeApproveOffer, To: NodeAwaitBackgroundCheck, RouteKey: "APPROVED"},
		{From: NodeApproveOffer, To: NodeEndOfferRejected, RouteKey: string(workflow.OutcomeRejected)},
		{From: NodeApproveOffer, To: NodeEndInvalidated, RouteKey: "INVALIDATED"},
		{From: NodeApproveOffer, To: NodeEndExpired, RouteKey: "EXPIRED"},
		{From: NodeApproveOffer, To: NodeEndCancelled, RouteKey: "CANCELLED"},

		{From: NodeAwaitBackgroundCheck, To: NodeEvaluateBackgroundCheck, RouteKey: string(workflow.OutcomeSucceeded)},
		{From: NodeAwaitBackgroundCheck, To: NodeEndBackgroundCheckExpired, RouteKey: "TIMED_OUT"},
		{From: NodeAwaitBackgroundCheck, To: NodeEndCancelled, RouteKey: "CANCELLED"},

		{From: NodeEvaluateBackgroundCheck, To: NodeCollectNewHireForms, RouteKey: RouteBackgroundCheckClear},
		{From: NodeEvaluateBackgroundCheck, To: NodeReviewAdverseResult, RouteKey: RouteBackgroundCheckAdverse},
		{From: NodeEvaluateBackgroundCheck, To: NodeEndInvalidated, RouteKey: string(workflow.OutcomeUnknown)},

		// HR's review of an adverse result either lets the hire continue
		// (SUCCEEDED) or withdraws the offer; every other TASK outcome is
		// the same withdrawal, never a silent continuation.
		{From: NodeReviewAdverseResult, To: NodeCollectNewHireForms, RouteKey: string(workflow.OutcomeSucceeded)},
		{From: NodeReviewAdverseResult, To: NodeEndOfferWithdrawn, RouteKey: string(workflow.OutcomeRejected)},
		{From: NodeReviewAdverseResult, To: NodeEndOfferWithdrawn, RouteKey: "EXPIRED"},
		{From: NodeReviewAdverseResult, To: NodeEndOfferWithdrawn, RouteKey: "CANCELLED"},

		{From: NodeCollectNewHireForms, To: NodeProvisionITAccess, RouteKey: string(workflow.OutcomeSucceeded)},
		{From: NodeCollectNewHireForms, To: NodeEndOfferWithdrawn, RouteKey: string(workflow.OutcomeRejected)},
		{From: NodeCollectNewHireForms, To: NodeEndExpired, RouteKey: "EXPIRED"},
		{From: NodeCollectNewHireForms, To: NodeEndCancelled, RouteKey: "CANCELLED"},

		// WF-EXT-019: the three provisioning tasks run in this fixed
		// SEQUENCE -- IT access, then workspace, then payroll -- because
		// PARALLEL/JOIN do not run in EXECUTE.
		{From: NodeProvisionITAccess, To: NodeProvisionWorkspace, RouteKey: string(workflow.OutcomeSucceeded)},
		{From: NodeProvisionITAccess, To: NodeEndFailed, RouteKey: string(workflow.OutcomeRejected)},
		{From: NodeProvisionITAccess, To: NodeEndFailed, RouteKey: "EXPIRED"},
		{From: NodeProvisionITAccess, To: NodeEndCancelled, RouteKey: "CANCELLED"},

		{From: NodeProvisionWorkspace, To: NodeEnrollPayroll, RouteKey: string(workflow.OutcomeSucceeded)},
		{From: NodeProvisionWorkspace, To: NodeEndFailed, RouteKey: string(workflow.OutcomeRejected)},
		{From: NodeProvisionWorkspace, To: NodeEndFailed, RouteKey: "EXPIRED"},
		{From: NodeProvisionWorkspace, To: NodeEndCancelled, RouteKey: "CANCELLED"},

		{From: NodeEnrollPayroll, To: NodeAwaitStartDate, RouteKey: string(workflow.OutcomeSucceeded)},
		{From: NodeEnrollPayroll, To: NodeEndFailed, RouteKey: string(workflow.OutcomeRejected)},
		{From: NodeEnrollPayroll, To: NodeEndFailed, RouteKey: "EXPIRED"},
		{From: NodeEnrollPayroll, To: NodeEndCancelled, RouteKey: "CANCELLED"},

		{From: NodeAwaitStartDate, To: NodeCommitHire, RouteKey: string(workflow.OutcomeSucceeded)},
		{From: NodeAwaitStartDate, To: NodeEndExpired, RouteKey: "LATE"},
		{From: NodeAwaitStartDate, To: NodeEndCancelled, RouteKey: "CANCELLED"},

		{From: NodeCommitHire, To: NodeEndHired, RouteKey: string(workflow.OutcomeSucceeded)},
		{From: NodeCommitHire, To: NodeEndFailed, RouteKey: string(workflow.OutcomeRejected)},
		{From: NodeCommitHire, To: NodeEndFailed, RouteKey: string(workflow.OutcomeUnknown)},
		{From: NodeCommitHire, To: NodeEndFailed, RouteKey: string(workflow.OutcomeAmbiguous)},
	}
}

type staticCapabilities map[capability.Key]capability.Record

func (r staticCapabilities) Lookup(key capability.Key) (capability.Record, bool) {
	record, ok := r[key]
	return record, ok
}

// capabilities resolves this definition's one capability identity. Its
// resolution rules mirror promotionexec's own fixture table: a real people
// domain would publish this record; here it exists only so the compiler --
// which unconditionally refuses a CAPABILITY node when no registry was
// supplied at all (internal/workflow/shape.go) -- can compile the node.
func capabilities() workflow.CapabilityResolver {
	return staticCapabilities{
		{ID: capCommitHire, Version: 1}: capability.Record{
			Definition: capability.Definition{
				ID: capCommitHire, Version: 1, OwnerDomain: "people",
				RequestSchema:        capability.SchemaRef{SchemaID: capCommitHire + ".request/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
				ResponseSchema:       capability.SchemaRef{SchemaID: capCommitHire + ".response/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
				ErrorSchema:          capability.SchemaRef{SchemaID: capCommitHire + ".error/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
				EffectClass:          capability.EffectInternalMutation,
				IdempotencyPolicyRef: "idempotency.new_hire.people/v1",
				AuthZScopeRef:        "scope:people.write",
				LegalBasisRef:        "legal.new_hire.execution/v1",
				EntitlementRef:       "entitlement.new_hire.execution/v1",
				SLOClassRef:          "slo.new_hire.execution/v1",
				TestRef:              "conformance:" + capCommitHire + "/v1",
			},
			Status: capability.StatusActive,
			Digest: "sha256:hireexec-people",
		},
	}
}

// Compile compiles the EXECUTE plan through the workflow compiler, which
// canonicalizes outcome routes and proves the graph's reachability, route
// coverage and effect-role placement (WF-RUN-037). An optional definition is
// accepted for mutation tests that want to pin a draft before publication,
// exactly as promotionexec.Compile accepts one.
func Compile(definitions ...workflow.Definition) (*workflow.CompiledWorkflow, error) {
	def := Definition()
	if len(definitions) == 1 {
		def = definitions[0]
	}
	return workflow.Compile(def, CompileOptions())
}

// NodeOrder returns the deterministic documented main-path order this
// package's golden test pins: the start node through to end_hired, following
// the success route of every node in between.
func NodeOrder() []string {
	return []string{
		NodePrepareHire, NodeApproveOffer, NodeAwaitBackgroundCheck, NodeEvaluateBackgroundCheck,
		NodeCollectNewHireForms, NodeProvisionITAccess, NodeProvisionWorkspace, NodeEnrollPayroll,
		NodeAwaitStartDate, NodeCommitHire, NodeEndHired,
	}
}
