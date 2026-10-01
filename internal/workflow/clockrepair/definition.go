package clockrepair

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
)

const (
	// WorkflowID identifies the published missing-punch repair workflow.
	WorkflowID = "hcmnext.workflows.time.fix_missing_punch"
	// IntentType identifies a worker's governed correction request.
	IntentType              = "hcmnext.time.fix_missing_punch/v1"
	CapabilityCommitRequest = "hcmnext.time.commit_missing_punch_request"
	CapabilityValidate      = "hcmnext.time.validate_missing_punch_request"
	CapabilityCorrection    = "hcmnext.time.append_missing_punch_correction"
	CapabilityReopen        = "hcmnext.time.request_governed_period_reopen"
	NodeCommitRequest       = "commit_missing_punch_request"
	NodeValidate            = "validate_request"
	NodeEligibility         = "check_eligibility"
	NodeApproval            = "supervisor_approval"
	NodeCorrection          = "append_correction"
	NodeReopen              = "governed_reopen"
	NodeApproved            = "approved"
	NodeRejected            = "rejected"
	NodeRepair              = "repair_required"
	NodeConflict            = "conflict"
	NodeClosedPeriod        = "closed_period_reopen_required"
	NodeCancelled           = "cancelled"
)

const (
	RouteEligible     = "ELIGIBLE"
	RouteIneligible   = "INELIGIBLE"
	RouteConflict     = "CONFLICT"
	RouteClosedPeriod = "CLOSED_PERIOD"
)

// Definition returns the executable graph. It contains no handler or storage
// implementation; capabilities are resolved and invoked by the runtime.
func Definition() workflow.Definition {
	inputs := requestFields()
	return workflow.Definition{
		WorkflowID: WorkflowID, Version: 1, Name: "Fix missing punch", IntentType: IntentType,
		InputSchema: schema("Input"), OutputSchema: schema("Result"), VariablesSchema: schema("Variables"), Inputs: inputs,
		Outputs: []workflow.Field{{Path: "terminal_code", Type: stringType()}}, TenantScope: "tenant-template", OrganizationScope: "tenant-template/time", RiskClass: "HIGH",
		DeclaredModes: []workflow.ExecutionMode{workflow.ModeExecute}, TerminalProfile: workflow.TerminalProfileExecute, StartNodeID: NodeCommitRequest,
		Limits: workflow.Limits{MaxFanOut: 5, MaxDepth: 9, MaxNodes: 16}, FailurePolicyRef: "policy.workflow.failure.time/v1", CancellationPolicyRef: "policy.workflow.cancellation.time/v1", MigrationPolicyRef: "policy.workflow.migration.pinned/v1", RetentionPolicyRef: "policy.workflow.retention.time-records/v1",
		ApprovalRequirements: []workflow.ApprovalRequirement{{ID: "approval.time.missing_punch.supervisor/v1", ResolverExpression: "SupervisorFor(worker)", Scope: "tenant-template/time", Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"}},
		Nodes: []workflow.Node{
			capabilityNode(NodeCommitRequest, CapabilityCommitRequest, capability.EffectInternalMutation, workflow.RoleAuthoritativeCore, "scope:time.punch.request", inputs),
			capabilityNode(NodeValidate, CapabilityValidate, capability.EffectReadOnly, workflow.RoleDerivedUpdate, "scope:time.punch.review", inputs),
			decisionNode(inputs), approvalNode(inputs), capabilityNode(NodeCorrection, CapabilityCorrection, capability.EffectInternalMutation, workflow.RoleAuthoritativeCore, "scope:time.punch.correct", inputs), capabilityNode(NodeReopen, CapabilityReopen, capability.EffectInternalMutation, workflow.RoleAuthoritativeCore, "scope:time.period.reopen", inputs),
			terminal(NodeApproved, "TIME_PUNCH_CORRECTED", workflow.RuntimeCompleted, builders.Completion("APPROVED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"), "receipt.time.punch.correction/v1"),
			terminal(NodeRejected, "TIME_PUNCH_CORRECTION_REJECTED", workflow.RuntimeCompleted, builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "CONSISTENT", "SATISFIED"), ""),
			terminal(NodeRepair, "TIME_PUNCH_REPAIR_REQUIRED", workflow.RuntimeRepairRequired, builders.Completion("SUBMITTED", "REPAIR_REQUIRED", "UNKNOWN", "DEGRADED", "PENDING"), ""),
			terminal(NodeConflict, "TIME_PUNCH_CORRECTION_CONFLICT", workflow.RuntimeBlocked, builders.Completion("SUBMITTED", "BLOCKED", "UNKNOWN", "DEGRADED", "PENDING"), ""),
			terminal(NodeClosedPeriod, "TIME_PUNCH_CLOSED_PERIOD_REOPEN_REQUIRED", workflow.RuntimeBlocked, builders.Completion("SUBMITTED", "BLOCKED", "UNKNOWN", "PENDING_OBSERVATION", "PENDING"), ""),
			terminal(NodeCancelled, "TIME_PUNCH_CORRECTION_CANCELLED", workflow.RuntimeCancelled, builders.Completion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), ""),
		},
		Edges: []workflow.Edge{
			edge(NodeCommitRequest, NodeValidate, string(workflow.OutcomeSucceeded)), edge(NodeCommitRequest, NodeRepair, string(workflow.OutcomeRejected)), edge(NodeCommitRequest, NodeRepair, string(workflow.OutcomeUnknown)), edge(NodeCommitRequest, NodeRepair, string(workflow.OutcomeAmbiguous)),
			edge(NodeValidate, NodeEligibility, string(workflow.OutcomeSucceeded)), edge(NodeValidate, NodeRepair, string(workflow.OutcomeRejected)), edge(NodeValidate, NodeRepair, string(workflow.OutcomeUnknown)), edge(NodeValidate, NodeRepair, string(workflow.OutcomeAmbiguous)),
			edge(NodeEligibility, NodeApproval, RouteEligible), edge(NodeEligibility, NodeRejected, RouteIneligible), edge(NodeEligibility, NodeConflict, RouteConflict), edge(NodeEligibility, NodeReopen, RouteClosedPeriod), edge(NodeEligibility, NodeRepair, string(workflow.OutcomeUnknown)),
			edge(NodeApproval, NodeCorrection, "APPROVED"), edge(NodeApproval, NodeRejected, string(workflow.OutcomeRejected)), edge(NodeApproval, NodeRejected, "EXPIRED"), edge(NodeApproval, NodeRejected, "INVALIDATED"), edge(NodeApproval, NodeCancelled, "CANCELLED"),
			edge(NodeCorrection, NodeApproved, string(workflow.OutcomeSucceeded)), edge(NodeCorrection, NodeConflict, string(workflow.OutcomeRejected)), edge(NodeCorrection, NodeRepair, string(workflow.OutcomeUnknown)), edge(NodeCorrection, NodeRepair, string(workflow.OutcomeAmbiguous)),
			edge(NodeReopen, NodeClosedPeriod, string(workflow.OutcomeSucceeded)), edge(NodeReopen, NodeRepair, string(workflow.OutcomeRejected)), edge(NodeReopen, NodeRepair, string(workflow.OutcomeUnknown)), edge(NodeReopen, NodeRepair, string(workflow.OutcomeAmbiguous)),
		},
	}
}

func stringType() workflow.ValueType { return workflow.ValueType{Kind: workflow.KindString} }
func branded(brand string) workflow.ValueType {
	return workflow.ValueType{Kind: workflow.KindString, Brand: brand}
}
func schema(name string) workflow.SchemaRef {
	return workflow.SchemaRef{SchemaID: WorkflowID + "." + name + "/v1", Version: 1, ProtobufFullName: "hcmnext.time.workflow." + name}
}

func requestFields() []workflow.Field {
	return []workflow.Field{{Path: "session_id", Type: branded("TimeSessionID")}, {Path: "original_observation_id", Type: branded("TimeObservationID")}, {Path: "expected_revision", Type: workflow.ValueType{Kind: workflow.KindInteger}}, {Path: "proposed_clock_out_at", Type: workflow.ValueType{Kind: workflow.KindInstant}}, {Path: "reason", Type: branded("CorrectionReason")}, {Path: "original_workflow_instance_ref", Type: branded("WorkflowInstanceRef")}}
}
func mappings(fields []workflow.Field) []workflow.Mapping {
	out := make([]workflow.Mapping, 0, len(fields))
	for _, f := range fields {
		out = append(out, workflow.Mapping{Target: f.Path, Source: builders.FromInput(f.Path)})
	}
	return out
}
func governance(boundary workflow.RevalidationBoundary, approvals ...string) workflow.NodeGovernance {
	return workflow.NodeGovernance{Purpose: "TIME_PUNCH_CORRECTION", Classification: "CONFIDENTIAL_TIME_AND_ATTENDANCE", ApprovalRequirements: approvals, RequiredDecisions: []workflow.GovernanceKind{workflow.GovernanceAuthZ, workflow.GovernanceLegal, workflow.GovernancePurpose, workflow.GovernanceRisk}, RevalidationBoundary: boundary, DataAccessManifestRef: "data-access.time.punch_correction/v1"}
}
func capabilityNode(id, capID string, effect capability.EffectClass, role workflow.EffectRole, scope string, fields []workflow.Field) workflow.Node {
	if !effect.IsWrite() {
		role = ""
	}
	return workflow.Node{ID: id, Type: workflow.StepCapability, InputSchema: capabilitySchema(capID, "request"), OutputSchema: capabilitySchema(capID, "response"), Inputs: fields, Outputs: []workflow.Field{{Path: "request_ref", Type: branded("WorkflowInstanceRef")}}, InputMappings: mappings(fields), DeclaredEffect: effect, EffectRole: role, SafePointRequested: true, FailureRoute: NodeRepair, Capability: &workflow.CapabilityRef{ID: capID, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{scope}, IdempotencyKeyMapping: "original_workflow_instance_ref", EffectBinding: id + ".original_workflow_instance_ref"}, Governance: governance(workflow.RevalidatePreEffect)}
}
func decisionNode(fields []workflow.Field) workflow.Node {
	return workflow.Node{ID: NodeEligibility, Type: workflow.StepDecision, InputSchema: schema("EligibilityInput"), OutputSchema: schema("EligibilityResult"), Inputs: fields, Outputs: []workflow.Field{{Path: "route_key", Type: stringType()}}, InputMappings: mappings(fields), Decision: &workflow.DecisionSpec{EvaluatorRef: "engines.rules.time.missing_punch_eligibility", EvaluatorVersion: 1, RuleRef: "rules.time.missing_punch_eligibility/v1", InputDigestProfile: "hcmnext.canonical.time.missing_punch/v1", Routes: []workflow.DecisionRoute{{Key: RouteEligible, Predicate: "request_is_eligible", Precedence: 10}, {Key: RouteIneligible, Predicate: "request_is_ineligible", Precedence: 20}, {Key: RouteConflict, Predicate: "revision_or_observation_conflict", Precedence: 30}, {Key: RouteClosedPeriod, Predicate: "period_closed_requires_reopen", Precedence: 40}, {Key: string(workflow.OutcomeUnknown), Predicate: "eligibility_evaluation_unknown", Precedence: 50}}, DefaultRoute: RouteIneligible}, Governance: governance(workflow.RevalidatePreExecution)}
}
func approvalNode(fields []workflow.Field) workflow.Node {
	return workflow.Node{ID: NodeApproval, Type: workflow.StepApproval, DeclaredEffect: capability.EffectPure, InputSchema: schema("ApprovalInput"), OutputSchema: schema("ApprovalResult"), Inputs: fields, InputMappings: mappings(fields), Governance: governance(workflow.RevalidatePreExecution, "approval.time.missing_punch.supervisor/v1")}
}
func terminal(id, code string, status workflow.RuntimeStatus, dims map[string]string, receipt string) workflow.Node {
	end := &workflow.EndSpec{TerminalCode: code, RuntimeStatus: status, CompletionMapping: dims}
	end.CommitReceiptRef = receipt
	if id == NodeRepair || id == NodeConflict {
		end.RepairRefs = []string{"repair.time.missing_punch/v1"}
	}
	return workflow.Node{ID: id, Type: workflow.StepEnd, InputSchema: schema(id + "Input"), OutputSchema: schema(id + "Result"), Inputs: []workflow.Field{{Path: "terminal_code", Type: stringType()}}, InputMappings: []workflow.Mapping{{Target: "terminal_code", Source: builders.Constant(code, stringType())}}, DeclaredEffect: capability.EffectPure, End: end, Governance: governance(workflow.RevalidatePreClosure)}
}
func edge(from, to, route string) workflow.Edge {
	return workflow.Edge{From: from, To: to, RouteKey: route}
}
