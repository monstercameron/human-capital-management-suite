package agentaccess

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// ErrUnavailable is returned when the console's store cannot be read or
// written. Nothing is changed when it is returned.
var ErrUnavailable = errors.New("agentaccess: the revision store is unavailable")

// RevisionStore is the durable form of the console's revisions and audit
// trail. A nil store keeps the console in memory. Every method is scoped to one
// tenant and applies that tenant's row-level rules.
type RevisionStore interface {
	// LoadRevisions returns every revision of the tenant, in any order.
	LoadRevisions(tenant string) ([]Revision, error)
	// SaveRevisions writes the revisions (inserting new ones, updating the
	// state of existing ones) and appends the audit event in one transaction.
	SaveRevisions(tenant string, at time.Time, event AuditEvent, revisions []Revision) error
	// Audit returns the tenant's console actions, oldest first.
	Audit(tenant string) ([]AuditEvent, error)
}

// WithStore makes the console keep its revisions and audit trail in store. It
// must be called before the console is used.
func (c *Console) WithStore(store RevisionStore) *Console {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = store
	c.loaded = map[string]bool{}
	return c
}

// loadLocked reads one tenant's revisions the first time the console is asked
// about the tenant. The caller holds c.mu.
func (c *Console) loadLocked(tenant string) error {
	if c.store == nil || c.loaded[tenant] {
		return nil
	}
	revisions, err := c.store.LoadRevisions(tenant)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	sort.Slice(revisions, func(i, j int) bool {
		if revisions[i].ConnectionID != revisions[j].ConnectionID {
			return revisions[i].ConnectionID < revisions[j].ConnectionID
		}
		return revisions[i].Number < revisions[j].Number
	})
	for _, revision := range revisions {
		k := key(tenant, revision.ConnectionID)
		c.revisions[k] = append(c.revisions[k], revision)
	}
	c.loaded[tenant] = true
	return nil
}

// persistLocked writes the changed revisions with the audit event, and then
// records the event in memory. Callers change their in-memory copy only after
// it returns nil, so what the console shows is what is stored.
func (c *Console) persistLocked(tenant, actor, action, revisionID string, revisions ...Revision) error {
	event := AuditEvent{At: c.now(), Tenant: tenant, Actor: actor, Action: action, Revision: revisionID}
	if c.store != nil {
		if err := c.store.SaveRevisions(tenant, event.At, event, revisions); err != nil {
			return fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
	}
	c.audit = append(c.audit, event)
	return nil
}
