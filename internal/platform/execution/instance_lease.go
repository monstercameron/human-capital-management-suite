package execution

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// defaultInstanceLeaseTTL bounds how long a caller-driven run may hold an
// instance before another replica may take it over. A synchronous promotion
// drain finishes well inside it; a holder that dies releases nothing and its
// claim lapses at this bound.
const defaultInstanceLeaseTTL = 2 * time.Minute

// InstanceLeaser is the production [execute.InstanceLeaser]: it takes and
// releases the WORKFLOW_INSTANCE lease through the durable lease manager, so
// every served advancement is fenced (WF-RUN-036).
type InstanceLeaser struct {
	Manager lease.Manager
	Holder  lease.Identity
	TTL     time.Duration
}

var _ execute.InstanceLeaser = InstanceLeaser{}

// NewInstanceLeaser builds a leaser for holder; a zero TTL uses the default.
func NewInstanceLeaser(holder lease.Identity, ttl time.Duration) InstanceLeaser {
	if ttl <= 0 {
		ttl = defaultInstanceLeaseTTL
	}
	return InstanceLeaser{Holder: holder, TTL: ttl}
}

// defaultInstanceHolder names this process as a lease holder.
func defaultInstanceHolder() lease.Identity {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	return lease.Identity{WorkloadRef: "workload:hcmnext-execution", InstanceRef: "replica:" + host + "-" + strconv.Itoa(os.Getpid())}
}

func instanceResource(instanceID uuid.UUID) lease.Resource {
	return lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: instanceID.String()}
}

// AcquireInstance implements execute.InstanceLeaser.
func (l InstanceLeaser) AcquireInstance(ctx context.Context, ex runtime.Executor, tenantID, instanceID uuid.UUID, at time.Time) (ret0 runtime.Fence, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execution.acquire_instance_lease")
	defer func() { observe.DoneWith(obsOp, retErr) }()
	ttl := l.TTL
	if ttl <= 0 {
		ttl = defaultInstanceLeaseTTL
	}
	grant, err := l.Manager.Acquire(ctx, ex, lease.AcquireRequest{
		TenantID: tenantID, Resource: instanceResource(instanceID), Holder: l.Holder, Now: at, TTL: ttl,
	})
	if err != nil {
		return runtime.Fence{}, err
	}
	return grant.Fence.RuntimeFence(at), nil
}

// ReleaseInstance implements execute.InstanceLeaser.
func (l InstanceLeaser) ReleaseInstance(ctx context.Context, ex runtime.Executor, tenantID uuid.UUID, fence runtime.Fence, at time.Time) (retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execution.release_instance_lease")
	defer func() { observe.DoneWith(obsOp, retErr) }()
	holder, err := lease.ParseHolder(fence.HolderID)
	if err != nil {
		return fmt.Errorf("platform execution: release instance lease: %w", err)
	}
	_, err = l.Manager.Release(ctx, ex, lease.Fence{
		TenantID: tenantID, Resource: lease.Resource{Kind: fence.ResourceKind, ID: fence.ResourceID},
		LeaseID: fence.LeaseID, Holder: holder, Token: fence.Token,
	}, at)
	return err
}
