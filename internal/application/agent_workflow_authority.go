package application

import (
	"context"
	"reflect"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// AgentWorkflowSourceAuthority checks the source-owned workflow occurrence.
// General current agent grants and bounded scope remain the admission owner's
// responsibility. Both checks must pass before the READY execution is created.
type AgentWorkflowSourceAuthority struct {
	Core          dbport.Beginner
	ResolveTenant func(string) uuid.UUID
	Versions      version.Store
}

func (a AgentWorkflowSourceAuthority) CheckRequest(ctx context.Context, request agentrun.Request) error {
	parts := strings.Split(request.Source.Ref, ":")
	if a.Core == nil || a.ResolveTenant == nil || a.Versions == nil || request.Source.Kind != agentrun.SourceWorkflow ||
		len(parts) != 3 || parts[0] != "workflow" || parts[2] == "" {
		return agentrun.ErrAuthorityRefusal
	}
	instanceID, err := uuid.Parse(parts[1])
	tenant := a.ResolveTenant(request.Source.TenantID)
	if err != nil || instanceID == uuid.Nil || tenant == uuid.Nil {
		return agentrun.ErrAuthorityRefusal
	}
	tx, err := a.Core.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	instance, err := (runtime.Store{}).LoadInstance(ctx, tx, tenant, instanceID)
	if err != nil {
		return err
	}
	if instance.RuntimeStatus != runtime.InstanceRunning && instance.RuntimeStatus != runtime.InstanceWaiting {
		return agentrun.ErrAuthorityRefusal
	}
	// Admission names an occurrence the runtime reached. Rechecks after the
	// invocation finishes may observe its durable successful attempt while the
	// instance waits for the agent; a future or failed node grants no authority.
	if !slices.Contains(instance.CurrentNodeIDs, parts[2]) {
		attempts, loadErr := (runtime.Store{}).LoadNodeExecutions(ctx, tx, tenant, instanceID)
		if loadErr != nil {
			return loadErr
		}
		reached := false
		for _, attempt := range attempts {
			if attempt.NodeID == parts[2] && attempt.StepType == workflow.StepCapability && attempt.Status == runtime.NodeSucceeded {
				reached = true
			}
		}
		if !reached {
			return agentrun.ErrAuthorityRefusal
		}
	}
	delegation, found, err := runtime.LoadExecutionDelegation(ctx, tx, tenant, instanceID)
	if err != nil {
		return err
	}
	if !found || delegation.Validate() != nil || delegation.TenantKey != request.Source.TenantID || !slices.Contains(delegation.Purposes, request.Purpose) {
		return agentrun.ErrAuthorityRefusal
	}
	actor := request.Principal.InvokerID
	if request.Principal.Mode == agentrun.ModeSponsored {
		actor = request.Principal.SponsorID
	}
	if actor != delegation.Subject {
		return agentrun.ErrAuthorityRefusal
	}
	execution, found, err := runtime.LoadExecutionContext(ctx, tx, tenant, instanceID)
	if err != nil {
		return err
	}
	if !found || execution.Digest() != instance.EffectiveContextRef || execution.LegalEntity != request.LegalEntity ||
		execution.Purpose != request.Purpose {
		return agentrun.ErrAuthorityRefusal
	}
	published, err := version.Resolve(version.BindTx(ctx, tx, a.Versions), instance.WorkflowID, version.Pin{CompiledPlanDigest: instance.CompiledPlanHash})
	if err != nil {
		return err
	}
	plan, err := workflow.DecodeCanonicalPlan(published.CanonicalPlanBytes)
	if err != nil {
		return err
	}
	if plan.Digest() != instance.CompiledPlanHash || plan.WorkflowID != instance.WorkflowID || plan.Version != instance.WorkflowVersion {
		return agentrun.ErrAuthorityRefusal
	}
	node, found := plan.Node(parts[2])
	if !found || node.Type != workflow.StepCapability || node.Capability == nil || node.Capability.ID != "agents.invoke" ||
		node.Capability.Version != 1 || node.Governance.Purpose != request.Purpose {
		return agentrun.ErrAuthorityRefusal
	}
	// The complete bounded request is an immutable input in this first binding.
	// Dynamic dataflow needs an artifact-owner validator before it is admitted.
	for _, mapping := range node.Mappings {
		if mapping.Target != "request_json" || mapping.SourceKind != workflow.SourceConstant {
			continue
		}
		var pinned agentrun.Request
		if agentWorkflowDecodeRequest(mapping.Constant, &pinned) != nil {
			return agentrun.ErrAuthorityRefusal
		}
		pinned.Source = request.Source
		if reflect.DeepEqual(pinned, request) {
			return nil
		}
	}
	return agentrun.ErrAuthorityRefusal
}
