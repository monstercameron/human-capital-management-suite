package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
)

// RoleConfig is the independently deployable role split hosted by the
// scheduler process.  Both roles use the same durable queue lease and clock;
// shard labels are configuration evidence and are included in role logs.
type RoleConfig struct {
	TimerEnabled  bool
	SignalEnabled bool
	TimerShard    string
	SignalShard   string
}

func DefaultRoleConfig() RoleConfig {
	return RoleConfig{TimerEnabled: true, SignalEnabled: true, TimerShard: "timer", SignalShard: "signal"}
}

func (r RoleConfig) withDefaults() RoleConfig {
	d := DefaultRoleConfig()
	if !r.TimerEnabled && !r.SignalEnabled && r.TimerShard == "" && r.SignalShard == "" {
		return d
	}
	if r.TimerShard == "" {
		r.TimerShard = d.TimerShard
	}
	if r.SignalShard == "" {
		r.SignalShard = d.SignalShard
	}
	return r
}

func (r RoleConfig) Validate() error {
	if !r.TimerEnabled && !r.SignalEnabled {
		return fmt.Errorf("%w: timer and signal roles are both disabled", ErrConfig)
	}
	if r.TimerEnabled && r.TimerShard == "" {
		return fmt.Errorf("%w: enabled timer role needs a shard", ErrConfig)
	}
	if r.SignalEnabled && r.SignalShard == "" {
		return fmt.Errorf("%w: enabled signal role needs a shard", ErrConfig)
	}
	return nil
}

// SignalRole is the semantic signal receiver hosted by the scheduler role.
// It is intentionally a port: durable matching and continuation creation live
// in internal/data/signals, while this package owns leases and role
// scheduling. [SignalDispatcher] is the production implementation.
type SignalRole interface {
	RunSignalRole(ctx context.Context, claim lease.AcquireRequest, now time.Time, shard string) (int, error)
}

// FencedSignalRole is the stronger scheduler-host seam. Implementations that
// consume durable signal work may use the queue fence to prove that the role
// is running under the lease this tick acquired. SignalRole remains supported
// for semantic adapters whose own transactional receive path already carries
// its evidence and fencing.
type FencedSignalRole interface {
	RunFencedSignalRole(ctx context.Context, claim lease.AcquireRequest, fence lease.Fence, now time.Time, shard string) (int, error)
}
