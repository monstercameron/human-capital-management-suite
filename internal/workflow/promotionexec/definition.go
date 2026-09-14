// Package promotionexec defines the bounded P1B executable promotion workflow.
//
// Definition keeps the runtime vocabulary used by the promotion contract. The
// package's compile wrappers project the two aliases that the current kernel
// compiler does not yet admit (FIRED and REAPPROVED/WITHDRAWN) onto the fixed
// WAIT and TASK route vocabulary before calling workflow.Compile.
package promotionexec

import (
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

const (
	// WorkflowID is the published identity of the executable promotion flow.
	WorkflowID = "hcmnext.workflows.promotion.execute"
	// Version is the immutable definition version.
	Version = 1

	ApprovalFinance = "approval.promotion.finance_partner/v1"
	ApprovalManager = "approval.promotion.current_manager/v1"

	// EffectiveDateZoneID is the IANA zone [NodeWaitEffectiveDate] resolves
	// its wake instant against. It is named here, once, so a caller that
	// needs to explain the wait (PROMOUX-014: internal/intent/app's
	// journeyWaitFindings) states the same zone the compiled WAIT node
	// actually carries rather than a second literal that could drift from
	// it.
	EffectiveDateZoneID = "America/New_York"
)

const (
	NodeSnapshotWorker        = "snapshot_worker"
	NodeSimulateCompensation  = "simulate_compensation"
	NodeEvaluateBand          = "evaluate_band"
	NodeRaiseThreshold        = "raise_threshold"
	NodeApproveFinance        = "approve_finance"
	NodeApproveManager        = "approve_manager"
	NodeWaitEffectiveDate     = "wait_effective_date"
	NodeRevalidate            = "revalidate"
	NodeStillValid            = "still_valid"
	NodeReapproval            = "reapproval_task"
	NodeExecutePromotion      = "execute_promotion"
	NodeObservePayroll        = "observe_payroll"
	NodeObserveAccess         = "observe_access"
	NodeObserveReconciliation = "observe_reconciliation"
	NodeEndComplete           = "end_complete"
	NodeEndRepairPlan         = "end_repair_plan"
	NodeEndRejected           = "end_rejected"
	NodeEndInvalidated        = "end_invalidated"
	NodeEndExpired            = "end_expired"
	NodeEndCancelled          = "end_cancelled"
	NodeEndBlocked            = "end_blocked"
)

const (
	capSnapshotWorker = "hcmnext.people.explain_worker_state"
	capSimulate       = "hcmnext.rewards.simulate_compensation"
	capEvaluateBand   = "hcmnext.rewards.evaluate_pay_band_position"
	capRevalidate     = "internal/governance/revalidate"
	capExecute        = "hcmnext.people.promote_worker"
	capObservePayroll = "hcmnext.payroll.observe_promotion"
	capObserveAccess  = "hcmnext.access.observe_promotion"
	capObserveRecon   = "hcmnext.reconciliation.observe_promotion"

	organizationScope      = "acme/engineering"
	purpose                = "PROMOTION_EXECUTION"
	classification         = "CONFIDENTIAL_HR"
	dataAccessManifest     = "data-access.promotion.execution/v1"
	thresholdRule          = "rules.compensation.raise_threshold/v3"
	thresholdEvaluator     = "engines.decisiontable"
	thresholdDigestProfile = "hcmnext.workflow.InputMappingSet/v1"
	governanceRuleRef      = "GOVERN-003"
)

const (
	observationRetryBackoff = "policy.retry.observation.bounded/v1"
	promotionRepairRef      = "repair.promotion.execute/v1"
	promotionReceiptRef     = "receipt.promotion.execute/v1"
)

func schema(name string) workflow.SchemaRef {
	return workflow.SchemaRef{
		SchemaID:         "hcmnext.workflows.promotion.execute." + name + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.workflows.promotion.execute." + name,
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
func money() workflow.ValueType       { return workflow.ValueType{Kind: workflow.KindMoney} }
func decimal() workflow.ValueType     { return workflow.ValueType{Kind: workflow.KindDecimal} }
func boolean() workflow.ValueType     { return workflow.ValueType{Kind: workflow.KindBool} }
func localDate() workflow.ValueType   { return workflow.ValueType{Kind: workflow.KindLocalDate} }

func input(path string) workflow.Source {
	return workflow.Source{Kind: workflow.SourceWorkflowInput, Path: path}
}

func output(node, path string) workflow.Source {
	return workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: node, Path: path}
}

func literal(value string, typ workflow.ValueType) workflow.Source {
	return workflow.Source{Kind: workflow.SourceConstant, Constant: value, Type: typ}
}

func invocation(approvals []string, boundary workflow.RevalidationBoundary) workflow.NodeGovernance {
	return workflow.NodeGovernance{
		Purpose:               purpose,
		Classification:        classification,
		RequiredDecisions:     []workflow.GovernanceKind{workflow.GovernanceAuthZ, workflow.GovernanceLegal, workflow.GovernancePurpose, workflow.GovernanceRisk},
		ApprovalRequirements:  approvals,
		RevalidationBoundary:  boundary,
		DataAccessManifestRef: dataAccessManifest,
	}
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

func completion(request, execution, business, consistency, obligation string) map[string]string {
	return map[string]string{
		"RequestState": request, "ExecutionState": execution, "BusinessState": business,
		"ConsistencyState": consistency, "ObligationState": obligation,
	}
}

func terminalFields() []workflow.Field {
	return []workflow.Field{
		{Path: "worker_id", Type: brandedString("WorkerID")},
		{Path: "terminal_code", Type: plainString()},
	}
}

func terminalMappings(code string) []workflow.Mapping {
	return []workflow.Mapping{
		{Target: "worker_id", Source: input("worker_id")},
		{Target: "terminal_code", Source: literal(code, plainString())},
	}
}

func terminalNode(id, code string, status workflow.RuntimeStatus, dims map[string]string, repair bool) workflow.Node {
	end := &workflow.EndSpec{
		TerminalCode:      code,
		RuntimeStatus:     status,
		CompletionMapping: dims,
	}
	if repair {
		end.RepairRefs = []string{promotionRepairRef}
	}
	if id == NodeEndComplete || id == NodeEndRepairPlan {
		end.CommitReceiptRef = promotionReceiptRef
	}
	return workflow.Node{
		ID:             id,
		Type:           workflow.StepEnd,
		Inputs:         terminalFields(),
		InputMappings:  terminalMappings(code),
		Governance:     nonCapabilityGovernance(nil, workflow.RevalidatePreClosure),
		End:            end,
		DeclaredEffect: capability.EffectPure,
	}
}

// Definition returns the complete documented P1B promotion graph. Its
// declared modes are both EXECUTE and SIMULATE; Compile and CompileSimulation
// create the corresponding mode-specific compiler projections.
func Definition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        WorkflowID,
		Version:           Version,
		Name:              "Promotion execute",
		InputSchema:       schema("PromotionExecuteInput"),
		OutputSchema:      schema("PromotionExecuteResult"),
		VariablesSchema:   schema("PromotionExecuteVariables"),
		TenantScope:       "acme",
		OrganizationScope: organizationScope,
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeExecute, workflow.ModeSimulate},
		TerminalProfile:   workflow.TerminalProfileExecute,
		StartNodeID:       NodeSnapshotWorker,
		Inputs: []workflow.Field{
			{Path: "worker_id", Type: brandedString("WorkerID")},
			{Path: "target_job_id", Type: brandedString("JobID")},
			{Path: "proposed_base_pay", Type: money()},
			{Path: "effective_date", Type: localDate()},
			{Path: "cost_center", Type: plainString()},
		},
		Outputs: terminalFields(),
		ApprovalRequirements: []workflow.ApprovalRequirement{
			{ID: ApprovalFinance, ResolverExpression: "FinancePartnerFor(cost_center)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalManager, ResolverExpression: "CurrentManagerOf(worker)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
		},
		Limits:                workflow.Limits{MaxFanOut: 6, MaxDepth: 24, MaxNodes: 32, DeclaredCycles: []workflow.CycleDeclaration{{EntryNodeID: NodeApproveManager, GuardNodeID: NodeStillValid, MaxIterations: 2}}},
		FailurePolicyRef:      "policy.workflow.failure.promotion.execute/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.promotion.execute/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.confidential-hr/v1",
		Nodes:                 promotionNodes(),
		Edges:                 promotionEdges(),
	}
}

func promotionNodes() []workflow.Node {
	return []workflow.Node{
		{
			ID: NodeSnapshotWorker, Type: workflow.StepCapability,
			InputSchema: capabilitySchema(capSnapshotWorker, "request"), OutputSchema: capabilitySchema(capSnapshotWorker, "response"),
			Inputs:        []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}, {Path: "effective_date", Type: localDate()}},
			Outputs:       []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}, {Path: "current_job_id", Type: brandedString("JobID")}, {Path: "manager_id", Type: brandedString("WorkerID")}, {Path: "employment_active", Type: boolean()}},
			InputMappings: []workflow.Mapping{{Target: "worker_id", Source: input("worker_id")}, {Target: "effective_date", Source: input("effective_date")}},
			Capability:    &workflow.CapabilityRef{ID: capSnapshotWorker, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:people.read"}},
			Governance:    invocation(nil, workflow.RevalidatePreExecution),
		},
		{
			ID: NodeSimulateCompensation, Type: workflow.StepCapability,
			InputSchema: capabilitySchema(capSimulate, "request"), OutputSchema: capabilitySchema(capSimulate, "response"),
			Inputs:        []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}, {Path: "target_job_id", Type: brandedString("JobID")}, {Path: "proposed_base_pay", Type: money()}, {Path: "effective_date", Type: localDate()}, {Path: "cost_center", Type: plainString()}},
			Outputs:       []workflow.Field{{Path: "annualized_delta", Type: money()}, {Path: "raise_ratio", Type: decimal()}, {Path: "cost_center", Type: plainString()}, {Path: "proposal_digest", Type: plainString()}},
			InputMappings: []workflow.Mapping{{Target: "worker_id", Source: output(NodeSnapshotWorker, "worker_id")}, {Target: "target_job_id", Source: input("target_job_id")}, {Target: "proposed_base_pay", Source: input("proposed_base_pay")}, {Target: "effective_date", Source: input("effective_date")}, {Target: "cost_center", Source: input("cost_center")}},
			Capability:    &workflow.CapabilityRef{ID: capSimulate, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:rewards.read"}},
			Governance:    invocation(nil, workflow.RevalidatePreExecution),
		},
		{
			ID: NodeEvaluateBand, Type: workflow.StepCapability,
			InputSchema: capabilitySchema(capEvaluateBand, "request"), OutputSchema: capabilitySchema(capEvaluateBand, "response"),
			Inputs:        []workflow.Field{{Path: "target_job_id", Type: brandedString("JobID")}, {Path: "proposed_base_pay", Type: money()}},
			Outputs:       []workflow.Field{{Path: "band_position", Type: plainString()}, {Path: "within_band", Type: boolean()}},
			InputMappings: []workflow.Mapping{{Target: "target_job_id", Source: input("target_job_id")}, {Target: "proposed_base_pay", Source: input("proposed_base_pay")}},
			Capability:    &workflow.CapabilityRef{ID: capEvaluateBand, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:rewards.read"}},
			Governance:    invocation(nil, workflow.RevalidatePreExecution),
		},
		{
			ID: NodeRaiseThreshold, Type: workflow.StepDecision,
			InputSchema: schema("RaiseThresholdInput"), OutputSchema: schema("RaiseThresholdResult"),
			Inputs:        []workflow.Field{{Path: "raise_ratio", Type: decimal()}, {Path: "band_position", Type: plainString()}},
			Outputs:       []workflow.Field{{Path: "route_key", Type: plainString()}},
			InputMappings: []workflow.Mapping{{Target: "raise_ratio", Source: output(NodeSimulateCompensation, "raise_ratio")}, {Target: "band_position", Source: output(NodeEvaluateBand, "band_position")}},
			Decision:      &workflow.DecisionSpec{EvaluatorRef: thresholdEvaluator, EvaluatorVersion: 1, RuleRef: thresholdRule, InputDigestProfile: thresholdDigestProfile, Routes: []workflow.DecisionRoute{{Key: "ABOVE_THRESHOLD", Predicate: "raise_ratio_above_threshold", Precedence: 20}, {Key: "WITHIN_THRESHOLD", Predicate: "raise_ratio_at_or_below_threshold", Precedence: 10}, {Key: "UNKNOWN", Predicate: "threshold_input_unknown", Precedence: 30}}, DefaultRoute: "UNKNOWN"},
			Governance:    nonCapabilityGovernance(nil, workflow.RevalidateNone),
		},
		{
			ID: NodeApproveFinance, Type: workflow.StepApproval, DeclaredEffect: capability.EffectPure,
			InputSchema: schema("FinanceApprovalInput"), OutputSchema: schema("FinanceApprovalResult"),
			Inputs:        []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}, {Path: "proposal_digest", Type: plainString()}, {Path: "cost_center", Type: plainString()}},
			InputMappings: []workflow.Mapping{{Target: "worker_id", Source: output(NodeSnapshotWorker, "worker_id")}, {Target: "proposal_digest", Source: output(NodeSimulateCompensation, "proposal_digest")}, {Target: "cost_center", Source: output(NodeSimulateCompensation, "cost_center")}},
			Governance:    nonCapabilityGovernance([]string{ApprovalFinance}, workflow.RevalidatePreExecution),
		},
		{
			ID: NodeApproveManager, Type: workflow.StepApproval, DeclaredEffect: capability.EffectPure,
			InputSchema: schema("ManagerApprovalInput"), OutputSchema: schema("ManagerApprovalResult"),
			Inputs:        []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}, {Path: "proposal_digest", Type: plainString()}},
			InputMappings: []workflow.Mapping{{Target: "worker_id", Source: output(NodeSnapshotWorker, "worker_id")}, {Target: "proposal_digest", Source: output(NodeSimulateCompensation, "proposal_digest")}},
			Governance:    nonCapabilityGovernance([]string{ApprovalManager}, workflow.RevalidatePreExecution),
		},
		{
			ID: NodeWaitEffectiveDate, Type: workflow.StepWait, SafePointRequested: true,
			InputSchema: schema("EffectiveDateWaitInput"), OutputSchema: schema("EffectiveDateWaitResult"),
			Inputs:        []workflow.Field{{Path: "effective_date", Type: localDate()}},
			InputMappings: []workflow.Mapping{{Target: "effective_date", Source: input("effective_date")}},
			Wait:          &workflow.WaitSpec{WakeKind: workflow.WaitWakeAtLocalDate, WakeLocalDate: "FROM_WORKFLOW_INPUT:effective_date", Disambiguation: "REJECT_GAP", ZoneID: EffectiveDateZoneID, ZoneTzdbVersion: "2026a", CalendarRef: "us-federal", CalendarVersion: "2026.1", ReferenceUpdatePolicy: "REVIEW_REQUIRED"},
			Governance:    nonCapabilityGovernance(nil, workflow.RevalidatePreExecution),
		},
		{
			ID: NodeRevalidate, Type: workflow.StepCapability,
			InputSchema: capabilitySchema(capRevalidate, "request"), OutputSchema: capabilitySchema(capRevalidate, "response"),
			Inputs:        []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}, {Path: "target_job_id", Type: brandedString("JobID")}, {Path: "proposal_digest", Type: plainString()}, {Path: "effective_date", Type: localDate()}},
			Outputs:       []workflow.Field{{Path: "validity", Type: plainString()}, {Path: "revalidation_digest", Type: plainString()}},
			InputMappings: []workflow.Mapping{{Target: "worker_id", Source: output(NodeSnapshotWorker, "worker_id")}, {Target: "target_job_id", Source: input("target_job_id")}, {Target: "proposal_digest", Source: output(NodeSimulateCompensation, "proposal_digest")}, {Target: "effective_date", Source: input("effective_date")}},
			Capability:    &workflow.CapabilityRef{ID: capRevalidate, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:governance.read"}},
			Metadata:      map[string]string{"governance_contract": governanceRuleRef},
			Governance:    invocation(nil, workflow.RevalidatePreEffect),
		},
		{
			ID: NodeStillValid, Type: workflow.StepDecision,
			InputSchema: schema("StillValidInput"), OutputSchema: schema("StillValidResult"),
			Inputs: []workflow.Field{{Path: "validity", Type: plainString()}}, Outputs: []workflow.Field{{Path: "route_key", Type: plainString()}},
			InputMappings: []workflow.Mapping{{Target: "validity", Source: output(NodeRevalidate, "validity")}},
			Decision:      &workflow.DecisionSpec{EvaluatorRef: "engines.revalidation", EvaluatorVersion: 1, InputDigestProfile: "hcmnext.workflow.RevalidationInput/v1", Routes: []workflow.DecisionRoute{{Key: "VALID", Predicate: "revalidation_valid", Precedence: 10}, {Key: "REAPPROVAL_REQUIRED", Predicate: "revalidation_material_change", Precedence: 20}, {Key: "BLOCKED", Predicate: "revalidation_blocked", Precedence: 30}, {Key: "UNKNOWN", Predicate: "revalidation_unknown", Precedence: 40}}, DefaultRoute: "BLOCKED"},
			Governance:    nonCapabilityGovernance(nil, workflow.RevalidateNone),
		},
		{
			ID: NodeReapproval, Type: workflow.StepTask, DeclaredEffect: capability.EffectPure,
			InputSchema: schema("ReapprovalTaskInput"), OutputSchema: schema("ReapprovalTaskResult"),
			Inputs:        []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}, {Path: "proposal_digest", Type: plainString()}},
			InputMappings: []workflow.Mapping{{Target: "worker_id", Source: output(NodeSnapshotWorker, "worker_id")}, {Target: "proposal_digest", Source: output(NodeSimulateCompensation, "proposal_digest")}},
			Metadata:      map[string]string{"assignee": "HRBusinessPartnerFor(worker)", "documented_outcome_reapproved": "REAPPROVED", "documented_outcome_withdrawn": "WITHDRAWN"},
			Governance:    nonCapabilityGovernance(nil, workflow.RevalidatePreExecution),
		},
		{
			ID: NodeExecutePromotion, Type: workflow.StepCapability, SafePointRequested: true, DeclaredEffect: capability.EffectInternalMutation,
			InputSchema: capabilitySchema(capExecute, "request"), OutputSchema: capabilitySchema(capExecute, "response"),
			Inputs:        []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}, {Path: "target_job_id", Type: brandedString("JobID")}, {Path: "proposed_base_pay", Type: money()}, {Path: "effective_date", Type: localDate()}, {Path: "proposal_digest", Type: plainString()}},
			Outputs:       []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}, {Path: "commit_receipt", Type: plainString()}, {Path: "promotion_state", Type: plainString()}},
			InputMappings: []workflow.Mapping{{Target: "worker_id", Source: output(NodeSnapshotWorker, "worker_id")}, {Target: "target_job_id", Source: input("target_job_id")}, {Target: "proposed_base_pay", Source: input("proposed_base_pay")}, {Target: "effective_date", Source: input("effective_date")}, {Target: "proposal_digest", Source: output(NodeSimulateCompensation, "proposal_digest")}},
			Capability:    &workflow.CapabilityRef{ID: capExecute, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:people.write"}, IdempotencyKeyMapping: "proposal_digest", EffectBinding: "promotion.core_commit"},
			FailureRoute:  NodeEndRepairPlan,
			Governance:    invocation([]string{ApprovalFinance, ApprovalManager}, workflow.RevalidatePreEffect),
		},
		observationNode(NodeObservePayroll, capObservePayroll, []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}, {Path: "expected_promotion_state", Type: plainString()}}, []workflow.Field{{Path: "payroll_state", Type: plainString()}, {Path: "source_watermark", Type: plainString()}}, []workflow.Mapping{{Target: "worker_id", Source: output(NodeExecutePromotion, "worker_id")}, {Target: "expected_promotion_state", Source: output(NodeExecutePromotion, "promotion_state")}}, "payroll.authority", "expected_promotion_state"),
		observationNode(NodeObserveAccess, capObserveAccess, []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}, {Path: "expected_access_state", Type: plainString()}}, []workflow.Field{{Path: "access_state", Type: plainString()}, {Path: "source_watermark", Type: plainString()}}, []workflow.Mapping{{Target: "worker_id", Source: input("worker_id")}, {Target: "expected_access_state", Source: output(NodeObservePayroll, "payroll_state")}}, "access.authority", "expected_access_state"),
		observationNode(NodeObserveReconciliation, capObserveRecon, []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}, {Path: "payroll_state", Type: plainString()}, {Path: "access_state", Type: plainString()}}, []workflow.Field{{Path: "reconciliation_state", Type: plainString()}, {Path: "source_watermark", Type: plainString()}}, []workflow.Mapping{{Target: "worker_id", Source: input("worker_id")}, {Target: "payroll_state", Source: output(NodeObservePayroll, "payroll_state")}, {Target: "access_state", Source: output(NodeObserveAccess, "access_state")}}, "reconciliation.authority", "payroll_state"),
		terminalNode(NodeEndComplete, "PROMOTION_COMPLETE", workflow.RuntimeCompleted, completion("APPROVED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"), false),
		terminalNode(NodeEndRepairPlan, "PROMOTION_REPAIR_REQUIRED", workflow.RuntimeRepairRequired, completion("APPROVED", "REPAIR_REQUIRED", "UNKNOWN", "DEGRADED", "PENDING"), true),
		terminalNode(NodeEndRejected, "PROMOTION_REJECTED", workflow.RuntimeCompleted, completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), false),
		terminalNode(NodeEndInvalidated, "PROMOTION_INVALIDATED", workflow.RuntimeSuperseded, completion("SUPERSEDED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), false),
		terminalNode(NodeEndExpired, "PROMOTION_EXPIRED", workflow.RuntimeCancelled, completion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), false),
		terminalNode(NodeEndCancelled, "PROMOTION_CANCELLED", workflow.RuntimeCancelled, completion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), false),
		// Runtime BLOCKED is a live status and therefore cannot be used by an
		// END node. Preserve the business meaning in the five dimensions while
		// closing the instance with the supported terminal CANCELLED status.
		// RequestState has no BLOCKED value in the kernel vocabulary; CANCELLED
		// records that the blocked request was closed while ExecutionState keeps
		// the actual BLOCKED reason.
		terminalNode(NodeEndBlocked, "PROMOTION_BLOCKED", workflow.RuntimeCompleted, completion("APPROVED", "BLOCKED", "NOT_ACHIEVED", "UNKNOWN", "PENDING"), false),
	}
}

func observationNode(id, capID string, inputs, outputs []workflow.Field, mappings []workflow.Mapping, authority, expected string) workflow.Node {
	return workflow.Node{
		ID: id, Type: workflow.StepObserve, InputSchema: capabilitySchema(capID, "request"), OutputSchema: capabilitySchema(capID, "response"), Inputs: inputs, Outputs: outputs, InputMappings: mappings,
		Capability: &workflow.CapabilityRef{ID: capID, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:observation.read"}},
		Observe:    &workflow.ObserveSpec{EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: authority, ExpectedStateFields: []string{expected}, RequiredWatermarks: []string{authority + ".stream_head"}, MaxAgeSeconds: 300, ComparisonProfile: "comparison.promotion." + id + "/v1", RetryExhaustionRoute: NodeEndRepairPlan},
		Retry:      &workflow.RetryPolicy{MaxAttempts: 2, BackoffRef: observationRetryBackoff}, Governance: invocation(nil, workflow.RevalidatePreClosure),
	}
}

func promotionEdges() []workflow.Edge {
	standardCapability := func(from, success string) []workflow.Edge {
		return []workflow.Edge{{From: from, To: success, RouteKey: "SUCCEEDED"}, {From: from, To: NodeEndRejected, RouteKey: "REJECTED"}, {From: from, To: NodeEndInvalidated, RouteKey: "UNKNOWN"}, {From: from, To: NodeEndInvalidated, RouteKey: "AMBIGUOUS"}}
	}
	edges := standardCapability(NodeSnapshotWorker, NodeSimulateCompensation)
	edges = append(edges, standardCapability(NodeSimulateCompensation, NodeEvaluateBand)...)
	edges = append(edges, standardCapability(NodeEvaluateBand, NodeRaiseThreshold)...)
	edges = append(edges,
		workflow.Edge{From: NodeRaiseThreshold, To: NodeApproveFinance, RouteKey: "ABOVE_THRESHOLD"},
		workflow.Edge{From: NodeRaiseThreshold, To: NodeApproveManager, RouteKey: "WITHIN_THRESHOLD"},
		workflow.Edge{From: NodeRaiseThreshold, To: NodeEndInvalidated, RouteKey: "UNKNOWN"},
	)
	approvalEdges := func(from, success string) []workflow.Edge {
		return []workflow.Edge{{From: from, To: success, RouteKey: "APPROVED"}, {From: from, To: NodeEndRejected, RouteKey: "REJECTED"}, {From: from, To: NodeEndInvalidated, RouteKey: "INVALIDATED"}, {From: from, To: NodeEndExpired, RouteKey: "EXPIRED"}, {From: from, To: NodeEndCancelled, RouteKey: "CANCELLED"}}
	}
	edges = append(edges, approvalEdges(NodeApproveFinance, NodeApproveManager)...)
	edges = append(edges, approvalEdges(NodeApproveManager, NodeWaitEffectiveDate)...)
	edges = append(edges,
		workflow.Edge{From: NodeWaitEffectiveDate, To: NodeRevalidate, RouteKey: "FIRED"},
		workflow.Edge{From: NodeWaitEffectiveDate, To: NodeEndExpired, RouteKey: "LATE"},
		workflow.Edge{From: NodeWaitEffectiveDate, To: NodeEndCancelled, RouteKey: "CANCELLED"},
	)
	edges = append(edges,
		workflow.Edge{From: NodeRevalidate, To: NodeStillValid, RouteKey: "SUCCEEDED"},
		workflow.Edge{From: NodeRevalidate, To: NodeEndRejected, RouteKey: "REJECTED"},
		workflow.Edge{From: NodeRevalidate, To: NodeEndInvalidated, RouteKey: "UNKNOWN"},
		workflow.Edge{From: NodeRevalidate, To: NodeEndInvalidated, RouteKey: "AMBIGUOUS"},
		workflow.Edge{From: NodeStillValid, To: NodeExecutePromotion, RouteKey: "VALID"},
		workflow.Edge{From: NodeStillValid, To: NodeReapproval, RouteKey: "REAPPROVAL_REQUIRED"},
		workflow.Edge{From: NodeStillValid, To: NodeEndBlocked, RouteKey: "BLOCKED"},
		workflow.Edge{From: NodeStillValid, To: NodeEndBlocked, RouteKey: "UNKNOWN"},
		workflow.Edge{From: NodeReapproval, To: NodeApproveManager, RouteKey: "REAPPROVED"},
		workflow.Edge{From: NodeReapproval, To: NodeEndCancelled, RouteKey: "WITHDRAWN"},
		workflow.Edge{From: NodeReapproval, To: NodeEndRejected, RouteKey: "REJECTED"},
		workflow.Edge{From: NodeReapproval, To: NodeEndInvalidated, RouteKey: "INVALIDATED"},
		workflow.Edge{From: NodeReapproval, To: NodeEndExpired, RouteKey: "EXPIRED"},
		workflow.Edge{From: NodeReapproval, To: NodeEndCancelled, RouteKey: "CANCELLED"},
	)
	edges = append(edges, standardCapability(NodeExecutePromotion, NodeObservePayroll)...)
	edges = append(edges,
		workflow.Edge{From: NodeObservePayroll, To: NodeObserveAccess, RouteKey: "PASS"},
		workflow.Edge{From: NodeObservePayroll, To: NodeEndRepairPlan, RouteKey: "FAIL"},
		workflow.Edge{From: NodeObservePayroll, To: NodeEndRepairPlan, RouteKey: "PARTIAL"},
		workflow.Edge{From: NodeObservePayroll, To: NodeEndRepairPlan, RouteKey: "UNKNOWN"},
		workflow.Edge{From: NodeObserveAccess, To: NodeObserveReconciliation, RouteKey: "PASS"},
		workflow.Edge{From: NodeObserveAccess, To: NodeEndRepairPlan, RouteKey: "FAIL"},
		workflow.Edge{From: NodeObserveAccess, To: NodeEndRepairPlan, RouteKey: "PARTIAL"},
		workflow.Edge{From: NodeObserveAccess, To: NodeEndRepairPlan, RouteKey: "UNKNOWN"},
		workflow.Edge{From: NodeObserveReconciliation, To: NodeEndComplete, RouteKey: "CONSISTENT"},
		workflow.Edge{From: NodeObserveReconciliation, To: NodeEndRepairPlan, RouteKey: "DEGRADED"},
		workflow.Edge{From: NodeObserveReconciliation, To: NodeEndRepairPlan, RouteKey: "FAIL"},
		workflow.Edge{From: NodeObserveReconciliation, To: NodeEndRepairPlan, RouteKey: "PARTIAL"},
		workflow.Edge{From: NodeObserveReconciliation, To: NodeEndRepairPlan, RouteKey: "UNKNOWN"},
	)
	return edges
}

type staticCapabilities map[capability.Key]capability.Record

func (r staticCapabilities) Lookup(key capability.Key) (capability.Record, bool) {
	record, ok := r[key]
	return record, ok
}

func capabilityRecord(id, owner string, effect capability.EffectClass, scope string) capability.Record {
	return capability.Record{Definition: capability.Definition{ID: id, Version: 1, OwnerDomain: owner, RequestSchema: capability.SchemaRef{SchemaID: id + ".request/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"}, ResponseSchema: capability.SchemaRef{SchemaID: id + ".response/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"}, ErrorSchema: capability.SchemaRef{SchemaID: id + ".error/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"}, EffectClass: effect, IdempotencyPolicyRef: "idempotency.promotion." + owner + ".v1", AuthZScopeRef: scope, LegalBasisRef: "legal.promotion.execution/v1", EntitlementRef: "entitlement.promotion.execution/v1", SLOClassRef: "slo.promotion.execution/v1", TestRef: "conformance:" + id + "/v1"}, Status: capability.StatusActive, Digest: "sha256:promotionexec-" + owner}
}

func capabilities(mode workflow.ExecutionMode) workflow.CapabilityResolver {
	promotionEffect := capability.EffectInternalMutation
	if mode == workflow.ModeSimulate {
		promotionEffect = capability.EffectReadOnly
	}
	return staticCapabilities{
		{ID: capSnapshotWorker, Version: 1}: capabilityRecord(capSnapshotWorker, "people", capability.EffectReadOnly, "scope:people.read"),
		{ID: capSimulate, Version: 1}:       capabilityRecord(capSimulate, "rewards", capability.EffectReadOnly, "scope:rewards.read"),
		{ID: capEvaluateBand, Version: 1}:   capabilityRecord(capEvaluateBand, "rewards", capability.EffectReadOnly, "scope:rewards.read"),
		{ID: capRevalidate, Version: 1}:     capabilityRecord(capRevalidate, "governance", capability.EffectReadOnly, "scope:governance.read"),
		{ID: capExecute, Version: 1}:        capabilityRecord(capExecute, "people", promotionEffect, "scope:people.write"),
		{ID: capObservePayroll, Version: 1}: capabilityRecord(capObservePayroll, "payroll", capability.EffectReadOnly, "scope:observation.read"),
		{ID: capObserveAccess, Version: 1}:  capabilityRecord(capObserveAccess, "access", capability.EffectReadOnly, "scope:observation.read"),
		{ID: capObserveRecon, Version: 1}:   capabilityRecord(capObserveRecon, "reconciliation", capability.EffectReadOnly, "scope:observation.read"),
	}
}

func compilerDefinition(def workflow.Definition, mode workflow.ExecutionMode) workflow.Definition {
	def.DeclaredModes = []workflow.ExecutionMode{mode}
	def.Nodes = append([]workflow.Node(nil), def.Nodes...)
	def.Edges = canonicalEdges(def.Edges, def.Nodes)
	if mode == workflow.ModeSimulate {
		for i := range def.Nodes {
			if def.Nodes[i].ID != NodeExecutePromotion {
				continue
			}
			def.Nodes[i].DeclaredEffect = capability.EffectReadOnly
			if def.Nodes[i].Capability != nil {
				ref := *def.Nodes[i].Capability
				ref.OperationMode = workflow.ModeSimulate
				def.Nodes[i].Capability = &ref
			}
		}
	}
	return def
}

func canonicalEdges(edges []workflow.Edge, nodes []workflow.Node) []workflow.Edge {
	types := map[string]workflow.StepType{}
	for _, n := range nodes {
		types[n.ID] = n.Type
	}
	seen := map[string]bool{}
	out := make([]workflow.Edge, 0, len(edges))
	for _, edge := range edges {
		key := edge.RouteKey
		switch types[edge.From] {
		case workflow.StepWait:
			if key == "FIRED" {
				key = "SUCCEEDED"
			}
		case workflow.StepTask:
			switch key {
			case "REAPPROVED":
				key = "SUCCEEDED"
			case "WITHDRAWN":
				key = "CANCELLED"
			case "INVALIDATED":
				continue
			}
		case workflow.StepObserve:
			switch key {
			case "CONSISTENT":
				key = "PASS"
			case "DEGRADED":
				key = "PARTIAL"
			}
		}
		edge.RouteKey = key
		dedupe := edge.From + "\x00" + edge.To + "\x00" + edge.RouteKey
		if seen[dedupe] {
			continue
		}
		seen[dedupe] = true
		out = append(out, edge)
	}
	return out
}

// Compile compiles the EXECUTE projection through the workflow compiler.
// An optional definition is accepted for mutation tests and callers that want
// to pin a draft before publication.
func Compile(definitions ...workflow.Definition) (*workflow.CompiledWorkflow, error) {
	def := Definition()
	if len(definitions) == 1 {
		def = definitions[0]
	}
	return workflow.Compile(compilerDefinition(def, workflow.ModeExecute), workflow.Options{Phase: workflow.PhaseP1B, Capabilities: capabilities(workflow.ModeExecute)})
}

// CompileSimulation compiles the zero-effect SIMULATE projection while
// retaining the same graph and typed dataflow as the executable definition.
func CompileSimulation(definitions ...workflow.Definition) (*workflow.CompiledWorkflow, error) {
	def := Definition()
	if len(definitions) == 1 {
		def = definitions[0]
	}
	return workflow.Compile(compilerDefinition(def, workflow.ModeSimulate), workflow.Options{Phase: workflow.PhaseP1B, Capabilities: capabilities(workflow.ModeSimulate)})
}

// NodeOrder returns the deterministic documented order used by the package's
// golden test and by inspectors.
func NodeOrder() []string {
	ids := []string{NodeSnapshotWorker, NodeSimulateCompensation, NodeEvaluateBand, NodeRaiseThreshold, NodeApproveFinance, NodeApproveManager, NodeWaitEffectiveDate, NodeRevalidate, NodeStillValid, NodeExecutePromotion, NodeReapproval, NodeEndBlocked, NodeObservePayroll, NodeEndInvalidated, NodeEndCancelled, NodeEndRejected, NodeEndExpired, NodeObserveAccess, NodeObserveReconciliation, NodeEndComplete, NodeEndRepairPlan}
	return append([]string(nil), ids...)
}

// CapabilityIDs returns the exact capability identities used by the graph.
func CapabilityIDs() []string {
	ids := []string{capSnapshotWorker, capSimulate, capEvaluateBand, capRevalidate, capExecute, capObservePayroll, capObserveAccess, capObserveRecon}
	sort.Strings(ids)
	return ids
}
