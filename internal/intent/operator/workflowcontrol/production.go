package workflowcontrol

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// GrantSource is the durable JIT grant store (internal/data/truststore.Store
// satisfies it).
type GrantSource interface {
	ActiveJITGrants(ctx context.Context, tenantID uuid.UUID, tenantKey values.TenantId, requester, capability string, now time.Time) ([]*jit.Grant, error)
}

// ErrNoAuthority reports that the operator holds no current grant for a
// control. The controller turns it into a governed DENIED outcome.
var ErrNoAuthority = errors.New("workflowcontrol: no current operator authority")

// InstanceApprovalField is the grant field that narrows a JIT grant to one
// workflow instance. A grant carrying it for the addressed instance is the
// durable dual-control record: its approver, whom the grant store already
// requires to differ from the requester, approved this operator acting on
// exactly this instance under this capability.
func InstanceApprovalField(instanceID string) string { return "workflow_instance:" + instanceID }

// JITAuthority resolves an operator's authority from the durable grant store.
// It presents the operator's current, unrevoked JIT grant naming the control.
// For a dual-control kind it presents the grant approver as the second
// approver only when the grant is narrowed to the addressed instance
// ([InstanceApprovalField]); a broad grant carries no per-action second
// approval, so a cancel under one is DENIED by the gateway rather than
// silently authorized. Simulation evidence is never invented here: the
// controller's preflight supplies it ([WithPreflightSimulation]).
type JITAuthority struct {
	Grants    GrantSource
	TenantIDs TenantIDs
	Clock     func() time.Time
}

// ResolveAuthority implements AuthorityResolver.
func (a JITAuthority) ResolveAuthority(ctx context.Context, tenant values.TenantId, operatorID string, kind operator.Kind, instanceID string) (ret0 Authority, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.control.resolve_authority", tenant, kind)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if a.Grants == nil || a.TenantIDs == nil {
		return Authority{}, fmt.Errorf("%w: grant source and tenant mapping are required", ErrInvalidCommand)
	}
	now := time.Now().UTC()
	if a.Clock != nil {
		now = a.Clock().UTC()
	}
	tenantID, err := a.TenantIDs(tenant)
	if err != nil {
		return Authority{}, err
	}
	grants, err := a.Grants.ActiveJITGrants(ctx, tenantID, tenant, operatorID, string(kind), now)
	if err != nil {
		return Authority{}, fmt.Errorf("workflowcontrol: load operator grants: %w", err)
	}
	policy, _ := operator.PolicyFor(kind)
	var broad *jit.Grant
	for _, g := range grants {
		if !slices.Contains(policy.Roles, g.Role) {
			continue
		}
		if policy.DualControl && instanceID != "" && slices.Contains(g.Fields, InstanceApprovalField(instanceID)) {
			return Authority{JIT: g, SecondApprover: g.Approver}, nil
		}
		if broad == nil {
			broad = g
		}
	}
	if broad != nil {
		return Authority{JIT: broad}, nil
	}
	// No usable grant: present none, and the gateway refuses with
	// OPERATOR_AUTHORITY_REQUIRED as a governed denial.
	return Authority{}, nil
}

// PlanSet resolves an instance's pinned plan from a fixed set of compiled
// plans, by digest.
type PlanSet map[string]*workflow.CompiledWorkflow

// NewPlanSet indexes compiled plans by digest.
func NewPlanSet(plans ...*workflow.CompiledWorkflow) PlanSet {
	set := PlanSet{}
	for _, p := range plans {
		if p != nil {
			set[p.Digest()] = p
		}
	}
	return set
}

// ResolvePlan implements PlanResolver.
func (s PlanSet) ResolvePlan(_ context.Context, _ runtime.Executor, inst runtime.Instance) (*workflow.CompiledWorkflow, error) {
	if p, ok := s[inst.CompiledPlanHash]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("workflowcontrol: no compiled plan %s is loaded in this cell", inst.CompiledPlanHash)
}

// KeyedTenantIDs adapts a tenant-key mapper to TenantIDs.
func KeyedTenantIDs(mapper func(key string) uuid.UUID) TenantIDs {
	return func(t values.TenantId) (uuid.UUID, error) {
		if mapper == nil || strings.TrimSpace(t.String()) == "" {
			return uuid.Nil, fmt.Errorf("%w: tenant", ErrInvalidCommand)
		}
		id := mapper(t.String())
		if id == uuid.Nil {
			return uuid.Nil, fmt.Errorf("%w: tenant %s has no storage identity", ErrInvalidCommand, t)
		}
		return id, nil
	}
}
