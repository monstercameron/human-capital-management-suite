package agentrunstate

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// RenewLease is the worker heartbeat. It is one conditional UPDATE: the lease
// moves only while owner still holds the fence and the lease has not expired,
// and the revision is left alone so the worker's own checkpoints keep working.
// Another process taking the run over bumps the revision and clears the lease,
// after which this answers runstate.ErrLease and the heartbeat stops.
func (s *TenantStore) RenewLease(ctx context.Context, id, owner string, fence uint64, until, now time.Time) error {
	if s == nil || s.db == nil || id == "" || owner == "" || fence == 0 || fence > math.MaxInt64 || until.IsZero() || now.IsZero() {
		return runstate.ErrLease
	}
	return s.db.RunTenantTx(ctx, s.tenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `UPDATE agent_run_execution SET lease_until=GREATEST(lease_until,$5)
			WHERE tenant_id=$1 AND run_id=$2 AND state='RUNNING' AND fence=$3 AND lease_owner=$4 AND lease_until>$6`,
			s.tenantID, id, int64(fence), owner, until.UTC(), now.UTC())
		if err != nil {
			return fmt.Errorf("agentrunstate: renew lease: %w", err)
		}
		if n != 1 {
			return runstate.ErrLease
		}
		return nil
	})
}

var _ runstate.LeaseRenewer = (*TenantStore)(nil)
