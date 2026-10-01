package application

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/bilateral"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// AgentRestorePendingOwner reads retained source deliveries and current owner
// revisions before redelivery to the existing idempotent admission boundary.
type AgentRestorePendingOwner interface {
	ReplayPending(context.Context, string, int) (int, error)
}

// AgentRestoreEffectOwner resolves a durable source-owner receipt. It must
// never execute an effect or infer success from a model/tool checkpoint.
type AgentRestoreEffectOwner interface {
	LookupRestoredEffect(context.Context, string, string, runstate.Effect) (runstate.EffectStatus, string, string, error)
}
type AgentRestoreRuntime struct {
	Agents        *agentstore.Store
	Runtime       *CommonAgentRuntime
	TenantUUID    func(values.TenantId) uuid.UUID
	PendingOwners []AgentRestorePendingOwner
	EffectOwner   AgentRestoreEffectOwner
	Limit         int
}

// TickTenant is suitable for the existing recovery-role fanout. Restored
// accepted rows remain fenced by current authority before any next work step.
func (r AgentRestoreRuntime) TickTenant(ctx context.Context, tenant string, now time.Time) (int, error) {
	if ctx == nil || r.Agents == nil || r.Runtime == nil || r.TenantUUID == nil || now.IsZero() {
		return 0, agentrun.ErrAuthorityMissing
	}
	limit := r.Limit
	if limit == 0 {
		limit = 100
	}
	ids, err := r.Agents.RestoredRunIDs(ctx, r.TenantUUID(values.TenantId(tenant)), limit)
	if err != nil {
		return 0, err
	}
	changed := 0
	var failures []error
	for _, id := range ids {
		n, err := r.reconcileRun(ctx, tenant, id, now)
		changed += n
		if err != nil {
			failures = append(failures, err)
		}
	}
	for _, owner := range r.PendingOwners {
		if owner == nil {
			failures = append(failures, agentrun.ErrAuthorityMissing)
			continue
		}
		n, err := owner.ReplayPending(ctx, tenant, limit)
		changed += n
		if err != nil {
			failures = append(failures, err)
		}
	}
	return changed, errors.Join(failures...)
}

func (r AgentRestoreRuntime) reconcileRun(ctx context.Context, tenant, id string, now time.Time) (int, error) {
	changed := 0
	run, err := r.Runtime.Recover(ctx, tenant, id)
	if err != nil {
		return changed, err
	}
	service, err := r.Runtime.executionService(ctx, tenant)
	if err != nil {
		return changed, err
	}
	if err = r.Runtime.Recheck(ctx, tenant, id); err != nil {
		if !errors.Is(err, agentrun.ErrAuthorityRefusal) && !errors.Is(err, bilateral.ErrDenied) {
			return changed, err
		}
		err = nil
		requested := false
		if !run.Deadline.After(now) && !run.ExpireRequested {
			run, err = service.Expire(ctx, id, run.Version, now)
			requested = true
		} else if run.Deadline.After(now) && !run.CancelRequested {
			run, err = service.Cancel(ctx, id, run.Version, now)
			requested = true
		}
		if err != nil {
			return changed, err
		}
		if requested {
			changed++
		}
	}
	if run.State == runstate.StateReconciling && r.EffectOwner != nil {
		for _, effect := range run.Effects {
			if effect.Status != runstate.EffectUnknown {
				continue
			}
			status, ref, digest, err := r.EffectOwner.LookupRestoredEffect(ctx, tenant, id, effect)
			if err != nil {
				return changed, err
			}
			if status == runstate.EffectUnknown {
				continue
			}
			run, err = service.ReconcileEffect(ctx, id, effect.ID, run.Version, status, ref, digest, now)
			if err != nil {
				return changed, err
			}
			changed++
		}
	}
	return changed, nil
}
