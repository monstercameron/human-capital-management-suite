package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// The sentinels the fenced-lease contract (JOB-003) classifies failures
// with. They compose with the store sentinels in jobs.go: a fence refusal
// never reaches a statement, so a stale or expired worker writes zero rows,
// while a live holder's store refusals (ErrDuplicate, ErrInvalid) pass
// through unchanged.
var (
	// ErrLeaseHeld reports an acquire on a partition another holder
	// currently leases. The current holder's token is unaffected.
	ErrLeaseHeld = errors.New("jobs: partition lease held")

	// ErrStaleFence reports a lease whose holder or fencing token no
	// longer matches the manager's current generation: a superseded
	// worker cannot checkpoint or effect. Nothing was written.
	ErrStaleFence = errors.New("jobs: stale partition fence")

	// ErrLeaseExpired reports a lease whose deadline passed without a
	// renewal. The holder must acquire a new generation. Nothing was
	// written.
	ErrLeaseExpired = errors.New("jobs: partition lease expired")
)

// Lease is one generation of an executor-local fence over a partition.
// Token is the fencing generation: it starts at 1 and increments every
// time a lapsed lease is re-acquired, so a worker holding an older token
// is recognizably stale. ExpiresAt is the holder's deadline in the
// manager's clock.
type Lease struct {
	TenantID    uuid.UUID
	PartitionID uuid.UUID
	Holder      string
	Token       uint64
	ExpiresAt   time.Time
}

type leaseKey struct {
	tenant    uuid.UUID
	partition uuid.UUID
}

type leaseEntry struct {
	holder    string
	token     uint64
	expiresAt time.Time
}

// LeaseManager fences partition execution across competing workers of one
// dispatcher. It is executor-local: the durable resume watermark stays in
// job_checkpoint (see LoadWatermark), while this manager decides which
// live worker may append to it. It is safe for concurrent use.
type LeaseManager struct {
	mu     sync.Mutex
	ttl    time.Duration
	now    func() time.Time
	leases map[leaseKey]leaseEntry
}

// NewLeaseManager bounds every lease it issues to ttl measured on now.
// A nil now reads the wall clock. A non-positive ttl cannot fence, so it
// is refused before any state exists.
func NewLeaseManager(ttl time.Duration, now func() time.Time) (*LeaseManager, error) {
	if ttl <= 0 {
		return nil, invalid("lease_ttl", "a fence needs a positive duration")
	}
	if now == nil {
		now = time.Now
	}
	return &LeaseManager{ttl: ttl, now: now, leases: map[leaseKey]leaseEntry{}}, nil
}

func validLeaseHolder(holder string) bool {
	return holder != "" && holder == strings.TrimSpace(holder)
}

// Acquire takes the current generation for holder, or reports the live
// holder with [ErrLeaseHeld]. A re-acquire by the current holder is
// idempotent: it returns the live lease unchanged. A lapsed lease is
// re-issued to the acquirer at the next fencing token, which is what
// renders the previous holder stale.
func (m *LeaseManager) Acquire(tenant, partition uuid.UUID, holder string) (Lease, error) {
	if tenant == uuid.Nil {
		return Lease{}, invalid("tenant_id", "a lease is tenant scoped")
	}
	if partition == uuid.Nil {
		return Lease{}, invalid("partition_id", "a lease names its partition")
	}
	if !validLeaseHolder(holder) {
		return Lease{}, invalid("holder", "a lease names its holder with an unpadded identifier")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := leaseKey{tenant: tenant, partition: partition}
	at := m.now()
	if current, ok := m.leases[key]; ok && at.Before(current.expiresAt) {
		if current.holder == holder {
			return Lease{TenantID: tenant, PartitionID: partition, Holder: holder, Token: current.token, ExpiresAt: current.expiresAt}, nil
		}
		return Lease{}, fmt.Errorf("%w: partition %s held by %q", ErrLeaseHeld, partition, current.holder)
	}
	var token uint64 = 1
	if current, ok := m.leases[key]; ok {
		token = current.token + 1
	}
	entry := leaseEntry{holder: holder, token: token, expiresAt: at.Add(m.ttl)}
	m.leases[key] = entry
	return Lease{TenantID: tenant, PartitionID: partition, Holder: holder, Token: token, ExpiresAt: entry.expiresAt}, nil
}

// Renew extends a current lease by one ttl from now. A superseded lease
// reports [ErrStaleFence]; a lapsed one reports [ErrLeaseExpired]. The
// token never changes across renewals.
func (m *LeaseManager) Renew(lease Lease) (Lease, error) {
	if err := checkLeaseShape(lease); err != nil {
		return Lease{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.leases[leaseKey{tenant: lease.TenantID, partition: lease.PartitionID}]
	if !ok || current.holder != lease.Holder || current.token != lease.Token {
		return Lease{}, fmt.Errorf("%w: holder %q token %d is not current", ErrStaleFence, lease.Holder, lease.Token)
	}
	if at := m.now(); !at.Before(current.expiresAt) {
		return Lease{}, fmt.Errorf("%w: holder %q token %d lapsed", ErrLeaseExpired, lease.Holder, lease.Token)
	}
	current.expiresAt = m.now().Add(m.ttl)
	m.leases[leaseKey{tenant: lease.TenantID, partition: lease.PartitionID}] = current
	lease.ExpiresAt = current.expiresAt
	return lease, nil
}

// Release drops a current lease. A superseded lease reports
// [ErrStaleFence] and changes nothing.
func (m *LeaseManager) Release(lease Lease) error {
	if err := checkLeaseShape(lease); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := leaseKey{tenant: lease.TenantID, partition: lease.PartitionID}
	current, ok := m.leases[key]
	if !ok || current.holder != lease.Holder || current.token != lease.Token {
		return fmt.Errorf("%w: holder %q token %d is not current", ErrStaleFence, lease.Holder, lease.Token)
	}
	delete(m.leases, key)
	return nil
}

// Check reports whether lease is the current generation: nil when the
// holder may write, [ErrStaleFence] when superseded, [ErrLeaseExpired]
// when lapsed. It never touches the store.
func (m *LeaseManager) Check(lease Lease) error {
	if err := checkLeaseShape(lease); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.leases[leaseKey{tenant: lease.TenantID, partition: lease.PartitionID}]
	if !ok || current.holder != lease.Holder || current.token != lease.Token {
		return fmt.Errorf("%w: holder %q token %d is not current", ErrStaleFence, lease.Holder, lease.Token)
	}
	if at := m.now(); !at.Before(current.expiresAt) {
		return fmt.Errorf("%w: holder %q token %d lapsed", ErrLeaseExpired, lease.Holder, lease.Token)
	}
	return nil
}

func checkLeaseShape(lease Lease) error {
	if lease.TenantID == uuid.Nil {
		return invalid("tenant_id", "a lease is tenant scoped")
	}
	if lease.PartitionID == uuid.Nil {
		return invalid("partition_id", "a lease names its partition")
	}
	if !validLeaseHolder(lease.Holder) {
		return invalid("holder", "a lease names its holder with an unpadded identifier")
	}
	return nil
}

// Checkpoint appends one checkpoint for the leased partition, but only
// when lease is still the current generation: the fence is checked before
// any statement runs, so a stale or expired worker's write never reaches
// the store. The checkpoint must belong to the leased tenant and
// partition; anything else is refused as invalid, not as stale.
func (m *LeaseManager) Checkpoint(ctx context.Context, ex Executor, lease Lease, in JobCheckpoint) (JobCheckpoint, error) {
	if err := m.Check(lease); err != nil {
		return JobCheckpoint{}, err
	}
	if in.TenantID != lease.TenantID || in.PartitionID != lease.PartitionID {
		return JobCheckpoint{}, invalid("partition_id", "a fenced checkpoint belongs to the leased partition")
	}
	return (CheckpointStore{}).Checkpoint(ctx, ex, in)
}
