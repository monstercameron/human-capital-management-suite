package agentcost

import (
	"errors"
	"fmt"
	"time"
)

// ErrUnavailable is returned when the durable store cannot be read or written.
// The gate fails closed on it: a call is not admitted when its limits cannot be
// read.
var ErrUnavailable = errors.New("agentcost: the cost store is unavailable")

// Store is the durable form of the limits, the audit trail and the finished
// runs. A nil Store keeps everything in memory. Every method is scoped to one
// tenant; an implementation applies the tenant's row-level rules.
type Store interface {
	// LoadLimits returns the tenant's limits. A removed limit is not returned.
	LoadLimits(tenant string) ([]Limit, error)
	// SaveLimit writes the limit (or removes it when removed is true) and
	// appends the audit row in one transaction.
	SaveLimit(event AuditEvent, next Limit, removed bool) error
	// LimitAudit returns the audit trail of one agent, oldest first.
	LimitAudit(tenant, agentID string) ([]AuditEvent, error)
	// Usage counts the runs and the spend of one agent, in one conversation when
	// conversation is not empty, since the given time.
	Usage(tenant, agentID, conversation string, since time.Time) (runs, micros int64, err error)
	// AppendRun records one finished run. A run id that is already stored is
	// not recorded again and is not an error.
	AppendRun(run Run) error
	// RunsSince returns the tenant's runs that finished at or after since.
	RunsSince(tenant string, since time.Time) ([]Run, error)
}

// WithStore makes the gate write its limits and audit through store and read
// them back after a restart. It must be called before the gate is used.
func (g *Gate) WithStore(store Store) *Gate {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.store = store
	g.loaded = map[string]bool{}
	return g
}

// hydrateLocked reads one tenant's limits and today's usage the first time the
// gate is asked about the tenant. The caller holds g.mu.
func (g *Gate) hydrateLocked(tenant string) error {
	if g.store == nil || g.loaded[tenant] {
		return nil
	}
	limits, err := g.store.LoadLimits(tenant)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	loaded := make(map[limitKey]Limit, len(limits))
	for _, limit := range limits {
		loaded[limit.key()] = limit
	}
	for key, limit := range loaded {
		if err := g.seedUsageLocked(key); err != nil {
			return err
		}
		g.limits[key] = limit
	}
	g.loaded[tenant] = true
	return nil
}

// seedUsageLocked sets today's usage under a limit from the finished runs, so a
// restart or a newly set limit counts what already happened today.
func (g *Gate) seedUsageLocked(key limitKey) error {
	if g.store == nil {
		return nil
	}
	day, _ := g.dayOf(g.now())
	start, err := time.ParseInLocation("2006-01-02", day, g.zone)
	if err != nil {
		return err
	}
	runs, micros, err := g.store.Usage(key.tenant, key.agent, key.conversation, start)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	slot := g.used[dayKey{key, day}]
	if slot == nil {
		slot = &usage{}
		g.used[dayKey{key, day}] = slot
	}
	slot.runs, slot.micros = runs, micros
	return nil
}

// Meter ties the gate to the ledger at the one place a run is admitted: Admit
// before the call, Finish after it.
type Meter struct {
	Gate   *Gate
	Ledger *Ledger
}

// Admit answers whether the call may be made.
func (m Meter) Admit(subject Subject) Decision {
	if m.Gate == nil {
		return Decision{Allowed: true}
	}
	return m.Gate.Admit(subject)
}

// Finish records a finished call under the agent's limits and in the ledger the
// cost report reads.
func (m Meter) Finish(subject Subject, run Run, ownerID string) error {
	if m.Gate != nil {
		m.Gate.Record(subject, run.SpendMicros, ownerID)
	}
	if m.Ledger != nil {
		return m.Ledger.Append(run)
	}
	return nil
}
