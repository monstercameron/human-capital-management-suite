package agentsystem

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrSyntheticTenantDenied means the trusted tenant authority did not certify
// the requested partition as an isolated evaluation tenant.
var ErrSyntheticTenantDenied = errors.New("agentsystem: synthetic tenant is not authorized")

// SyntheticTenantAuthority certifies that a tenant is provisioned for isolated
// evaluation. Implementations must resolve an explicit trusted tenant marker;
// tenant-name conventions and caller-supplied claims are not sufficient.
type SyntheticTenantAuthority interface {
	AuthorizeSyntheticTenant(context.Context, values.TenantId) error
}

// ForSyntheticTenant authorizes the tenant before asking any tenant-scoped
// store for a partition, then returns the real tenant-bound Runner. The
// authority and platform composition must use evaluation-only tenant stores,
// budget/audit sinks and fixture-safe tool ownership; this method never
// substitutes in-memory stores or an alternate runner.
func (p *Platform) ForSyntheticTenant(ctx context.Context, tenant values.TenantId, authority SyntheticTenantAuthority) (*Runner, error) {
	if ctx == nil || p == nil || authority == nil {
		return nil, fmt.Errorf("%w: context, platform and synthetic tenant authority are required", ErrInvalid)
	}
	if err := tenant.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := authority.AuthorizeSyntheticTenant(ctx, tenant); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSyntheticTenantDenied, err)
	}
	runner, err := p.ForTenant(ctx, tenant)
	if err != nil {
		return nil, err
	}
	if runner.TenantID() != tenant {
		return nil, fmt.Errorf("%w: tenant-scoped runner mismatch", ErrSyntheticTenantDenied)
	}
	return runner, nil
}

// StartSyntheticTask authorizes and binds the synthetic tenant before creating
// a task through Runner.StartTask. It does not confirm or drive the task; the
// caller must use the returned tenant-bound Runner and the same isolated
// evaluation composition for those later transitions.
func (p *Platform) StartSyntheticTask(ctx context.Context, tenant values.TenantId, authority SyntheticTenantAuthority, request StartRequest) (*Runner, agentrun.AgentTask, error) {
	if request.UserAuthority.Tenant != tenant {
		return nil, agentrun.AgentTask{}, fmt.Errorf("%w: requested authority tenant does not match synthetic tenant", ErrSyntheticTenantDenied)
	}
	runner, err := p.ForSyntheticTenant(ctx, tenant, authority)
	if err != nil {
		return nil, agentrun.AgentTask{}, err
	}
	task, err := runner.StartTask(ctx, request)
	if err != nil {
		return nil, agentrun.AgentTask{}, err
	}
	if task.TenantID != tenant.String() {
		return nil, agentrun.AgentTask{}, fmt.Errorf("%w: created task tenant mismatch", ErrSyntheticTenantDenied)
	}
	return runner, task, nil
}

// TenantID reports the partition bound into this Runner before any task starts.
func (r *Runner) TenantID() values.TenantId {
	if r == nil {
		return ""
	}
	return r.tenant
}
