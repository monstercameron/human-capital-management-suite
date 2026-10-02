package runstate

import (
	"context"
	"time"
)

// RenewLease extends the lease of a RUNNING run held by owner at fence. The
// revision is unchanged, matching the PostgreSQL store.
func (s *MemoryStore) RenewLease(_ context.Context, id, owner string, fence uint64, until, now time.Time) error {
	if s == nil {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[id]
	if !ok {
		return ErrNotFound
	}
	if run.State != StateRunning || run.Lease == nil || run.Lease.Owner != owner || run.Lease.Fence != fence || run.Fence != fence || !run.Lease.Until.After(now) {
		return ErrLease
	}
	if until.After(run.Lease.Until) {
		lease := *run.Lease
		lease.Until = until.UTC()
		run.Lease = &lease
		s.runs[id] = run
	}
	return nil
}

var _ LeaseRenewer = (*MemoryStore)(nil)
