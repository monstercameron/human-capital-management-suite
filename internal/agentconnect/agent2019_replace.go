package agentconnect

import "fmt"

// Replace makes a newer published revision the live one for a connection that
// is already registered. Register adds the first revision; the console publishes
// every later one (and every rollback, which is a new revision number) through
// Replace. The connection's epoch, every user's link and every user's epoch are
// kept, and every credential lease issued under the old revision is fenced, so
// a skill the new revision no longer grants cannot be called with a lease that
// was minted before.
//
// The replacement is held in memory: the durable form of the revision history
// is the console's own store, which the composition replays after a restart.
func (r *Registry) Replace(revision ConnectionRevision) error {
	if r == nil {
		return ErrInvalid
	}
	if err := revision.validate(); err != nil {
		return err
	}
	key := itemKey(revision.TenantID, revision.ID)
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.items[key]
	if !ok {
		return ErrNotFound
	}
	if revision.Revision <= item.revision.Revision {
		return fmt.Errorf("%w: revision %d is not newer than the live revision %d", ErrInvalid, revision.Revision, item.revision.Revision)
	}
	if revision.Connection != item.revision.Connection {
		return fmt.Errorf("%w: a revision cannot move to another connectivity connection", ErrInvalid)
	}
	connectionID, tenant := revision.ID, revision.TenantID
	r.fenceLeasesLocked(func(call CallLease) bool {
		return call.ConnectionID == connectionID && call.TenantID == tenant
	}, "connection revision replaced")
	item.revision = copyRevision(revision)
	return nil
}
