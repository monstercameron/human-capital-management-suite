// Package prototype contains bounded executable workflow definitions used to
// integrate the P1B runtime before they replace any canonical release golden.
package prototype

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

const (
	// ApprovalWorkflowID identifies the deliberately small executable P1B
	// workflow. It is separate from workflow.PromotionWorkflowID so compiling
	// it cannot change the SIMULATE-only promotion reference or its goldens.
	ApprovalWorkflowID = "hcmnext.workflows.prototype.promotion_approval"

	NodeApproval    = "approve_promotion"
	NodeApproved    = "end_approved"
	NodeRejected    = "end_rejected"
	NodeInvalidated = "end_invalidated"
	NodeExpired     = "end_expired"
	NodeCancelled   = "end_cancelled"

	ApprovalRequirementID  = "approval.prototype.promotion/v1"
	ApprovedProposalDigest = "sha256:prototype-approved-proposal"
	ApprovalIRSchemaV1     = 1
)

// ApprovalDefinition returns the smallest bounded executable graph that can
// park on a human approval and route every outcome the APPROVAL primitive
// declares. It performs no business mutation: ModeExecute here means the
// runtime may create and complete governed human work, while a later driver
// owns the separately guarded terminal ledger write.
func ApprovalDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        ApprovalWorkflowID,
		Version:           1,
		Name:              "Prototype promotion approval",
		InputSchema:       schema("ApprovalInput"),
		OutputSchema:      schema("ApprovalResult"),
		VariablesSchema:   schema("ApprovalVariables"),
		TenantScope:       "prototype",
		OrganizationScope: "prototype/people",
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeExecute},
		TerminalProfile:   workflow.TerminalProfileExecute,
		StartNodeID:       NodeApproval,
		Inputs: []workflow.Field{
			{Path: "worker_id", Type: brandedString("WorkerID")},
			{Path: "proposal_digest", Type: stringType()},
		},
		Outputs: terminalFields(),
		ApprovalRequirements: []workflow.ApprovalRequirement{{
			ID:                  ApprovalRequirementID,
			ResolverExpression:  "CurrentManagerOf(worker)",
			Scope:               "prototype/people",
			Quorum:              1,
			SeparationOfDuties:  true,
			EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND",
		}},
		Limits: workflow.Limits{MaxFanOut: 5, MaxDepth: 2, MaxNodes: 6},

		FailurePolicyRef:      "policy.workflow.failure.prototype/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.prototype/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.prototype/v1",

		Nodes: []workflow.Node{
			{
				ID:           NodeApproval,
				Type:         workflow.StepApproval,
				InputSchema:  schema("ApprovalTaskInput"),
				OutputSchema: schema("ApprovalTaskResult"),
				Inputs: []workflow.Field{
					{Path: "worker_id", Type: brandedString("WorkerID")},
					{Path: "proposal_digest", Type: stringType()},
				},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
					{Target: "proposal_digest", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "proposal_digest"}},
				},
				DeclaredEffect: capability.EffectPure,
				Governance: workflow.NodeGovernance{
					Purpose:               "PROMOTION_APPROVAL",
					Classification:        "CONFIDENTIAL_HR",
					ApprovalRequirements:  []string{ApprovalRequirementID},
					RevalidationBoundary:  workflow.RevalidatePreExecution,
					DataAccessManifestRef: "data-access.prototype.promotion-approval/v1",
				},
			},
			terminalNode(NodeApproved, "APPROVED", workflow.RuntimeCompleted,
				completion("APPROVED", "NOT_PLANNED", "NOT_STARTED", "NOT_APPLICABLE", "SATISFIED"), true),
			terminalNode(NodeRejected, "REJECTED", workflow.RuntimeCompleted,
				completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), false),
			terminalNode(NodeInvalidated, "INVALIDATED", workflow.RuntimeSuperseded,
				completion("SUPERSEDED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), false),
			terminalNode(NodeExpired, "EXPIRED", workflow.RuntimeCancelled,
				completion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), false),
			terminalNode(NodeCancelled, "CANCELLED", workflow.RuntimeCancelled,
				completion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), false),
		},
		Edges: []workflow.Edge{
			{From: NodeApproval, To: NodeApproved, RouteKey: "APPROVED"},
			{From: NodeApproval, To: NodeRejected, RouteKey: "REJECTED"},
			{From: NodeApproval, To: NodeInvalidated, RouteKey: "INVALIDATED"},
			{From: NodeApproval, To: NodeExpired, RouteKey: "EXPIRED"},
			{From: NodeApproval, To: NodeCancelled, RouteKey: "CANCELLED"},
		},
	}
}

// CompileApproval compiles the published 1.0.0 prototype using its original
// IR schema. The default compiler schema advances independently; changing it
// here would produce a different digest under the same durable workflow
// version and make an otherwise unchanged local-dev database refuse startup.
// No capability registry is needed because this graph invokes no capability
// and performs no business mutation.
func CompileApproval() (*workflow.CompiledWorkflow, error) {
	return workflow.Compile(ApprovalDefinition(), workflow.Options{Phase: workflow.PhaseP1B, IRSchemaVersion: ApprovalIRSchemaV1})
}

func schema(name string) workflow.SchemaRef {
	return workflow.SchemaRef{
		SchemaID:         "hcmnext.workflows.prototype." + name + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.workflow.prototype." + name,
	}
}

func stringType() workflow.ValueType { return workflow.ValueType{Kind: workflow.KindString} }

func brandedString(brand string) workflow.ValueType {
	return workflow.ValueType{Kind: workflow.KindString, Brand: brand}
}

func terminalFields() []workflow.Field {
	return []workflow.Field{
		{Path: "worker_id", Type: brandedString("WorkerID")},
		{Path: "terminal_code", Type: stringType()},
	}
}

func terminalNode(
	id, code string,
	status workflow.RuntimeStatus,
	dimensions map[string]string,
	approved bool,
) workflow.Node {
	end := &workflow.EndSpec{
		TerminalCode:      code,
		RuntimeStatus:     status,
		CompletionMapping: dimensions,
	}
	if approved {
		end.ApprovalRequired = true
		end.ApprovedMaterialDigest = ApprovedProposalDigest
	}
	return workflow.Node{
		ID:     id,
		Type:   workflow.StepEnd,
		Inputs: terminalFields(),
		InputMappings: []workflow.Mapping{
			{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
			{Target: "terminal_code", Source: workflow.Source{
				Kind: workflow.SourceConstant, Constant: code, Type: stringType(),
			}},
		},
		Governance: workflow.NodeGovernance{
			Purpose:               "PROMOTION_APPROVAL",
			Classification:        "CONFIDENTIAL_HR",
			RevalidationBoundary:  workflow.RevalidatePreClosure,
			DataAccessManifestRef: "data-access.prototype.promotion-approval/v1",
		},
		End: end,
	}
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
