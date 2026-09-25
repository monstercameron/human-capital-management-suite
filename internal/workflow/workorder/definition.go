// Package workorder defines the first executable work-order workflow template.
// Business records, notes, budgets and accepted quantities remain owned by
// field operations; this graph coordinates their governed transitions.
package workorder

import (
	"errors"
	"regexp"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

var ErrInvalidTemplatePin = errors.New("work order workflow: invalid published template pin")

var templateIDPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

const (
	WorkflowID = "hcmnext.workflows.work_order"
	Version    = 1

	NodeSubmitRequest  = "submit_request"
	NodeApproveScope   = "approve_scope"
	NodeApproveBudget  = "approve_budget"
	NodeRelease        = "release_work"
	NodeAwaitExecution = "await_execution"
	NodeInspect        = "inspect_work"
	NodeAccept         = "accept_work"
	NodeClose          = "close_work_order"
	NodeEndClosed      = "end_closed"
	NodeEndRejected    = "end_rejected"
	NodeEndCancelled   = "end_cancelled"
	NodeEndFailed      = "end_failed"

	CapabilitySubmitRequest = "hcmnext.field.work_order.submit_request"
	CapabilityRelease       = "hcmnext.field.work_order.release"
	CapabilityClose         = "hcmnext.field.work_order.close"

	ApprovalScope  = "approval.work_order.scope/v1"
	ApprovalBudget = "approval.work_order.budget/v1"
	ApprovalAccept = "approval.work_order.acceptance/v1"
)

const (
	purpose        = "WORK_ORDER_EXECUTE"
	classification = "CONFIDENTIAL_PROJECT"
	manifest       = "data-access.work_order.execute/v1"
)

// Template is the reviewed, customer-configurable portion of the workflow.
// These fields are material to the definition and therefore to the compiled
// plan digest. A running instance pins the compiled result.
type Template struct {
	TenantScope              string
	OrganizationScope        string
	Version                  uint32
	ScopeApproverExpression  string
	BudgetApproverExpression string
	InspectorExpression      string
}

// DefaultTemplate returns a fresh default template value.
func DefaultTemplate() Template {
	return Template{
		TenantScope:              "tenant-example",
		OrganizationScope:        "tenant-example/projects",
		Version:                  1,
		ScopeApproverExpression:  "ProjectScopeApproverFor(work_order)",
		BudgetApproverExpression: "ProjectFinanceApproverFor(work_order)",
		InspectorExpression:      "WorkOrderInspectorFor(work_order)",
	}
}

// TemplatePin is the exact published template identity selected for one
// work-order kind. It is copied into workflow selection metadata so the
// runtime cannot substitute another tenant template with a similar shape.
type TemplatePin struct {
	TemplateID string
	Version    string
	Digest     string
}

// DefinitionForPin binds the execution workflow to one immutable template
// publication. The caller must resolve and verify the pin from its tenant
// template authority before using this definition.
func DefinitionForPin(template Template, pin TemplatePin) (workflow.Definition, error) {
	if !templateIDPattern.MatchString(pin.TemplateID) || strings.TrimSpace(pin.Version) == "" || strings.TrimSpace(pin.Digest) == "" || template.Version == 0 {
		return workflow.Definition{}, ErrInvalidTemplatePin
	}
	definition := DefinitionFor(template)
	definition.WorkflowID = WorkflowIDForTemplate(pin.TemplateID)
	definition.Name = "Work order execution " + pin.TemplateID + "@" + pin.Version + " " + pin.Digest
	definition.MatchPredicate = map[string]string{
		"work_order.template_id":      pin.TemplateID,
		"work_order.template_version": pin.Version,
		"work_order.template_digest":  pin.Digest,
	}
	return definition, nil
}

// WorkflowIDForTemplate returns a stable execution identity per work-order
// template. The compiled workflow version is the reviewed numeric Template.Version.
func WorkflowIDForTemplate(templateID string) string {
	if !templateIDPattern.MatchString(templateID) {
		return ""
	}
	return WorkflowID + "." + strings.ToLower(templateID)
}

// CompilePinned compiles the workflow selected by a verified exact template
// pin and returns its immutable plan and content digest.
func CompilePinned(template Template, pin TemplatePin) (*workflow.CompiledWorkflow, error) {
	definition, err := DefinitionForPin(template, pin)
	if err != nil {
		return nil, err
	}
	return Compile(definition)
}

// PhaseBinding maps a stable business phase from a published work-order
// template to the node which enters it and the node which must finish before
// the next transition is eligible.
type PhaseBinding struct {
	PhaseID           string
	EntryNodeID       string
	CompletionNodeID  string
	CompletionOutcome string
}

// PhaseBindings exposes the v1 kernel-to-template phase projection for
// validating that a published template follows the executable lifecycle.
func PhaseBindings() []PhaseBinding {
	return []PhaseBinding{
		{PhaseID: "DRAFT", EntryNodeID: NodeSubmitRequest, CompletionNodeID: NodeSubmitRequest, CompletionOutcome: "SUCCEEDED"},
		{PhaseID: "AUTHORIZATION", EntryNodeID: NodeApproveScope, CompletionNodeID: NodeApproveBudget, CompletionOutcome: "APPROVED"},
		{PhaseID: "READY", EntryNodeID: NodeRelease, CompletionNodeID: NodeRelease, CompletionOutcome: "SUCCEEDED"},
		{PhaseID: "EXECUTION", EntryNodeID: NodeAwaitExecution, CompletionNodeID: NodeAwaitExecution, CompletionOutcome: "SUCCEEDED"},
		{PhaseID: "INSPECTION", EntryNodeID: NodeInspect, CompletionNodeID: NodeInspect, CompletionOutcome: "SUCCEEDED"},
		{PhaseID: "ACCEPTED", EntryNodeID: NodeAccept, CompletionNodeID: NodeAccept, CompletionOutcome: "APPROVED"},
		{PhaseID: "CLOSED", EntryNodeID: NodeClose, CompletionNodeID: NodeClose, CompletionOutcome: "SUCCEEDED"},
	}
}

// Definition returns the default versioned work-order workflow.
func Definition() workflow.Definition { return DefinitionFor(DefaultTemplate()) }

// DefinitionFor materializes an immutable workflow definition from a reviewed
// template. Tenant-custom code is not accepted; reviewed scopes and approver
// bindings are the declared customization points.
func DefinitionFor(template Template) workflow.Definition {
	if template.Version == 0 {
		template.Version = 1
	}
	if template.ScopeApproverExpression == "" {
		template.ScopeApproverExpression = "ProjectScopeApproverFor(work_order)"
	}
	if template.BudgetApproverExpression == "" {
		template.BudgetApproverExpression = "ProjectFinanceApproverFor(work_order)"
	}
	if template.InspectorExpression == "" {
		template.InspectorExpression = "WorkOrderInspectorFor(work_order)"
	}
	return workflow.Definition{
		WorkflowID: WorkflowID, Version: template.Version, Name: "Work order execution",
		IntentType:  "WORK_ORDER_EXECUTE",
		InputSchema: schema("Input"), OutputSchema: schema("Result"), VariablesSchema: schema("Variables"),
		TenantScope: template.TenantScope, OrganizationScope: template.OrganizationScope, RiskClass: "HIGH",
		DeclaredModes: []workflow.ExecutionMode{workflow.ModeExecute}, TerminalProfile: workflow.TerminalProfileExecute,
		StartNodeID: NodeSubmitRequest,
		Inputs:      workflowInputs(), Outputs: []workflow.Field{{Path: "work_order_id", Type: brandedString("WorkOrderID")}},
		ApprovalRequirements: []workflow.ApprovalRequirement{
			{ID: ApprovalScope, ResolverExpression: template.ScopeApproverExpression, Scope: template.OrganizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalBudget, ResolverExpression: template.BudgetApproverExpression, Scope: template.OrganizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalAccept, ResolverExpression: template.InspectorExpression, Scope: template.OrganizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
		},
		Limits:                workflow.Limits{MaxFanOut: 6, MaxDepth: 16, MaxNodes: 16},
		FailurePolicyRef:      "policy.workflow.failure.work_order/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.work_order/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.project-confidential/v1",
		Nodes:                 nodes(template), Edges: edges(template),
	}
}

func Compile(definitions ...workflow.Definition) (*workflow.CompiledWorkflow, error) {
	definition := Definition()
	if len(definitions) == 1 {
		definition = definitions[0]
	}
	return workflow.Compile(definition, workflow.Options{Phase: workflow.PhaseP1B, Capabilities: capabilityRecords()})
}

func schema(name string) workflow.SchemaRef {
	return workflow.SchemaRef{SchemaID: "hcmnext.workflows.work_order." + name + "/v1", Version: 1, ProtobufFullName: "hcmnext.workflows.work_order." + name}
}

func capabilitySchema(id, direction string) workflow.SchemaRef {
	return workflow.SchemaRef{SchemaID: id + "." + direction + "/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"}
}

func brandedString(brand string) workflow.ValueType {
	return workflow.ValueType{Kind: workflow.KindString, Brand: brand}
}

func workflowInputs() []workflow.Field {
	return []workflow.Field{
		{Path: "work_order_id", Type: brandedString("WorkOrderID")},
		{Path: "scope_revision", Type: workflow.ValueType{Kind: workflow.KindInteger}},
		{Path: "scope_digest", Type: brandedString("WorkOrderScopeDigest")},
		{Path: "budget_request_id", Type: brandedString("BudgetRequestID")},
		{Path: "budget_digest", Type: brandedString("BudgetDigest")},
		{Path: "initiator_note_id", Type: brandedString("WorkOrderNoteID")},
	}
}

func mappings(fields []workflow.Field) []workflow.Mapping {
	out := make([]workflow.Mapping, 0, len(fields))
	for _, field := range fields {
		out = append(out, workflow.Mapping{Target: field.Path, Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: field.Path}})
	}
	return out
}

func governance(approvals []string, boundary workflow.RevalidationBoundary) workflow.NodeGovernance {
	return workflow.NodeGovernance{Purpose: purpose, Classification: classification, ApprovalRequirements: approvals, RevalidationBoundary: boundary, DataAccessManifestRef: manifest}
}

func capabilityNode(id, nodeID string, effect capability.EffectClass, role workflow.EffectRole, scope string) workflow.Node {
	fields := workflowInputs()
	return workflow.Node{
		ID: nodeID, Type: workflow.StepCapability, SafePointRequested: true,
		InputSchema: capabilitySchema(id, "request"), OutputSchema: capabilitySchema(id, "response"),
		Inputs:        fields,
		Outputs:       []workflow.Field{{Path: "work_order_id", Type: brandedString("WorkOrderID")}},
		InputMappings: mappings(fields), DeclaredEffect: effect, EffectRole: role,
		Capability: &workflow.CapabilityRef{ID: id, Version: 1, OperationMode: workflow.ModeExecute,
			AuthorityScopes: []string{scope}, IdempotencyKeyMapping: "work_order_id", EffectBinding: nodeID + ".work_order_id"},
		FailureRoute: NodeEndFailed, Governance: workflow.NodeGovernance{
			Purpose: purpose, Classification: classification,
			RequiredDecisions:    []workflow.GovernanceKind{workflow.GovernanceAuthZ, workflow.GovernanceLegal, workflow.GovernancePurpose, workflow.GovernanceRisk},
			RevalidationBoundary: workflow.RevalidatePreEffect, DataAccessManifestRef: manifest,
		},
	}
}

func approvalNode(id string, approvals []string) workflow.Node {
	fields := workflowInputs()
	return workflow.Node{ID: id, Type: workflow.StepApproval, DeclaredEffect: capability.EffectPure, InputSchema: schema(id + "Input"), OutputSchema: schema(id + "Result"),
		Inputs: fields, InputMappings: mappings(fields), Governance: governance(approvals, workflow.RevalidatePreExecution)}
}

func taskNode(id string, template Template) workflow.Node {
	fields := workflowInputs()
	return workflow.Node{ID: id, Type: workflow.StepTask, DeclaredEffect: capability.EffectPure, InputSchema: schema(id + "Input"), OutputSchema: schema(id + "Result"), Inputs: fields, InputMappings: mappings(fields),
		Metadata:   map[string]string{"assignee": template.InspectorExpression, "evidence": "required_inspection_evidence"},
		Governance: governance(nil, workflow.RevalidatePreExecution)}
}

func signalNode() workflow.Node {
	fields := workflowInputs()
	return workflow.Node{ID: NodeAwaitExecution, Type: workflow.StepSignal, DeclaredEffect: capability.EffectPure, InputSchema: schema("AwaitExecutionInput"), OutputSchema: schema("AwaitExecutionResult"),
		Inputs: fields, InputMappings: mappings(fields), FailureRoute: NodeEndFailed,
		Signal:     &workflow.SignalSpec{EventType: "hcmnext.events.work_order.execution_completed", CorrelationKeyExpression: "work_order_id", ExpectedSchemaRef: schema("ExecutionCompleted"), AcceptedSources: []string{"hcmnext.field.work_order"}, Ordering: workflow.SignalOrderingMonotonicSequence, CloseAfterSeconds: 2592000},
		Governance: governance(nil, workflow.RevalidatePreExecution)}
}

func endNode(id, code string, status workflow.RuntimeStatus, request, execution, business, consistency, obligation string) workflow.Node {
	fields := []workflow.Field{{Path: "work_order_id", Type: brandedString("WorkOrderID")}}
	end := &workflow.EndSpec{TerminalCode: code, RuntimeStatus: status, CompletionMapping: map[string]string{"RequestState": request, "ExecutionState": execution, "BusinessState": business, "ConsistencyState": consistency, "ObligationState": obligation}}
	if id == NodeEndClosed {
		end.CommitReceiptRef = "receipt.work_order.close/v1"
	}
	if status == workflow.RuntimeRepairRequired {
		end.RepairRefs = []string{"repair.work_order.execution/v1"}
	}
	return workflow.Node{ID: id, Type: workflow.StepEnd, InputSchema: schema(id + "Input"), OutputSchema: schema(id + "Result"), Inputs: fields,
		InputMappings: mappings(fields), DeclaredEffect: capability.EffectPure,
		End:        end,
		Governance: governance(nil, workflow.RevalidatePreClosure)}
}

func nodes(template Template) []workflow.Node {
	result := []workflow.Node{
		capabilityNode(CapabilitySubmitRequest, NodeSubmitRequest, capability.EffectInternalMutation, workflow.RoleAuthoritativeCore, "scope:work_order.write"),
		approvalNode(NodeApproveScope, []string{ApprovalScope}),
		approvalNode(NodeApproveBudget, []string{ApprovalBudget}),
		capabilityNode(CapabilityRelease, NodeRelease, capability.EffectInternalMutation, workflow.RoleDownstreamEffect, "scope:work_order.release"),
		signalNode(),
		approvalNode(NodeAccept, []string{ApprovalAccept}),
		capabilityNode(CapabilityClose, NodeClose, capability.EffectInternalMutation, workflow.RoleDownstreamEffect, "scope:work_order.close"),
		endNode(NodeEndClosed, "WORK_ORDER_CLOSED", workflow.RuntimeCompleted, "APPROVED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"),
		endNode(NodeEndRejected, "WORK_ORDER_REJECTED", workflow.RuntimeCompleted, "REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
		endNode(NodeEndCancelled, "WORK_ORDER_CANCELLED", workflow.RuntimeCancelled, "CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
		endNode(NodeEndFailed, "WORK_ORDER_REPAIR_REQUIRED", workflow.RuntimeRepairRequired, "APPROVED", "REPAIR_REQUIRED", "UNKNOWN", "DEGRADED", "PENDING"),
	}
	result = append(result, taskNode(NodeInspect, template))
	return result
}

func edges(template Template) []workflow.Edge {
	approvalFailure := func(from string) []workflow.Edge {
		return []workflow.Edge{
			{From: from, To: NodeEndRejected, RouteKey: string(workflow.OutcomeRejected)},
			{From: from, To: NodeEndCancelled, RouteKey: "CANCELLED"},
			{From: from, To: NodeEndCancelled, RouteKey: "EXPIRED"},
			{From: from, To: NodeEndCancelled, RouteKey: "INVALIDATED"},
		}
	}
	result := []workflow.Edge{
		{From: NodeSubmitRequest, To: NodeApproveScope, RouteKey: string(workflow.OutcomeSucceeded)},
		{From: NodeSubmitRequest, To: NodeEndFailed, RouteKey: string(workflow.OutcomeRejected)},
		{From: NodeSubmitRequest, To: NodeEndFailed, RouteKey: string(workflow.OutcomeUnknown)},
		{From: NodeSubmitRequest, To: NodeEndFailed, RouteKey: string(workflow.OutcomeAmbiguous)},
		{From: NodeApproveScope, To: NodeApproveBudget, RouteKey: "APPROVED"},
		{From: NodeApproveBudget, To: NodeRelease, RouteKey: "APPROVED"},
		{From: NodeRelease, To: NodeAwaitExecution, RouteKey: string(workflow.OutcomeSucceeded)},
		{From: NodeRelease, To: NodeEndFailed, RouteKey: string(workflow.OutcomeRejected)},
		{From: NodeRelease, To: NodeEndFailed, RouteKey: string(workflow.OutcomeUnknown)},
		{From: NodeRelease, To: NodeEndFailed, RouteKey: string(workflow.OutcomeAmbiguous)},
		{From: NodeAwaitExecution, To: NodeEndFailed, RouteKey: "TIMED_OUT"},
		{From: NodeAwaitExecution, To: NodeEndCancelled, RouteKey: "CANCELLED"},
		{From: NodeAccept, To: NodeClose, RouteKey: "APPROVED"},
		{From: NodeClose, To: NodeEndClosed, RouteKey: string(workflow.OutcomeSucceeded)},
		{From: NodeClose, To: NodeEndFailed, RouteKey: string(workflow.OutcomeRejected)},
		{From: NodeClose, To: NodeEndFailed, RouteKey: string(workflow.OutcomeUnknown)},
		{From: NodeClose, To: NodeEndFailed, RouteKey: string(workflow.OutcomeAmbiguous)},
	}
	result = append(result, approvalFailure(NodeApproveScope)...)
	result = append(result, approvalFailure(NodeApproveBudget)...)
	result = append(result, approvalFailure(NodeAccept)...)
	result = append(result,
		workflow.Edge{From: NodeAwaitExecution, To: NodeInspect, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeInspect, To: NodeAccept, RouteKey: string(workflow.OutcomeSucceeded)},
		workflow.Edge{From: NodeInspect, To: NodeEndFailed, RouteKey: string(workflow.OutcomeRejected)},
		workflow.Edge{From: NodeInspect, To: NodeEndCancelled, RouteKey: "EXPIRED"},
		workflow.Edge{From: NodeInspect, To: NodeEndCancelled, RouteKey: "CANCELLED"},
	)
	return result
}

type capabilityTable map[capability.Key]capability.Record

func (table capabilityTable) Lookup(key capability.Key) (capability.Record, bool) {
	record, ok := table[key]
	return record, ok
}

func capabilityRecords() workflow.CapabilityResolver {
	defs := []struct {
		id, scope string
		effect    capability.EffectClass
	}{
		{CapabilitySubmitRequest, "scope:work_order.write", capability.EffectInternalMutation},
		{CapabilityRelease, "scope:work_order.release", capability.EffectInternalMutation},
		{CapabilityClose, "scope:work_order.close", capability.EffectInternalMutation},
	}
	table := make(capabilityTable, len(defs))
	for _, item := range defs {
		key := capability.Key{ID: item.id, Version: 1}
		table[key] = capability.Record{
			Definition: capability.Definition{
				ID: item.id, Version: 1, OwnerDomain: "field_operations",
				RequestSchema:  capability.SchemaRef{SchemaID: item.id + ".request/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
				ResponseSchema: capability.SchemaRef{SchemaID: item.id + ".response/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
				ErrorSchema:    capability.SchemaRef{SchemaID: item.id + ".error/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
				EffectClass:    item.effect, IdempotencyPolicyRef: "idempotency.work_order/v1", AuthZScopeRef: item.scope,
				LegalBasisRef: "legal.work_order.execution/v1", EntitlementRef: "entitlement.work_order.execution/v1",
				SLOClassRef: "slo.work_order.execution/v1", TestRef: "conformance:" + item.id + "/v1",
			},
			Status: capability.StatusActive, Digest: "sha256:work-order:" + item.id,
		}
	}
	return table
}
