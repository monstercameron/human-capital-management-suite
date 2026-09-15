package application

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	wfrecover "github.com/monstercameron/human-capital-management-suite/internal/workflow/recover"
)

// recoveryLeaseTTL bounds one redelivery the recovery role runs. It matches
// the scheduler's own instance claim window: a redelivery is one bounded
// synchronous drain, exactly like a dispatched timer resume.
const recoveryLeaseTTL = 5 * time.Minute

// readyRedeliverFunc is the cell call the recovery role redelivers through.
type readyRedeliverFunc func(ctx context.Context, instanceID string, expectedVersion int64) (app.ExecutionResult, error)

// readyRedeliverer adapts the cell's RedeliverReady to the recovery sweep's
// port. Like the timer dispatcher it refuses an orphan of any tenant but the
// one this serve process was configured for: an orphaned row is not
// permission to cross a tenant.
func readyRedeliverer(tenantID, tenant string, redeliver readyRedeliverFunc) wfrecover.Redeliverer {
	return wfrecover.RedelivererFunc(func(ctx context.Context, req wfrecover.Redelivery) error {
		if req.Orphan.TenantID.String() != tenantID {
			return fmt.Errorf("application: orphaned instance %s belongs to another tenant", req.Orphan.InstanceID)
		}
		_, err := redeliver(app.WithResumeTenant(ctx, tenant), req.Orphan.InstanceID.String(), req.Orphan.InstanceVersion)
		return err
	})
}

// composeRecoveryRole builds WF-RUN-003's production recovery sweep for the
// serve scheduler: orphaned instances a dead driver left READY are claimed by
// lease takeover under this replica's identity and redelivered through the
// cell's RedeliverReady.
func composeRecoveryRole(pool *pgxadapter.Pool, identity, tenantID, tenant string, redeliver readyRedeliverFunc) (wfrecover.Sweeper, error) {
	if pool == nil {
		return wfrecover.Sweeper{}, fmt.Errorf("application: the recovery role needs a database pool")
	}
	return wfrecover.NewSweeper(wfrecover.SweeperOptions{
		DB:        pool,
		Holder:    lease.Identity{WorkloadRef: "workload:hcmnext-serve-recovery", InstanceRef: identity},
		LeaseTTL:  recoveryLeaseTTL,
		Leases:    lease.Manager{},
		Redeliver: readyRedeliverer(tenantID, tenant, redeliver),
	})
}
