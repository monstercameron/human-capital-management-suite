// Package agentcost gives agent owners spend limits they can set and a cost
// they can read (AGENTCOST-006). A limit counts runs and money for one agent,
// optionally inside one conversation, over a day in the tenant's time zone.
// The gate answers before a call is made; it records after. Money is held in
// micro-dollars so sums stay exact.
package agentcost

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalid = errors.New("agentcost: invalid request")
	ErrDenied  = errors.New("agentcost: only an owner of this agent can do that")
)

// Limit is a daily ceiling for one agent, or for one agent in one
// conversation when ConversationID is set. A zero dimension means no ceiling
// on that dimension; a limit with both zero is not a limit.
type Limit struct {
	TenantID       string
	AgentID        string
	ConversationID string
	MaxRuns        int64
	MaxSpendMicros int64
}

type limitKey struct{ tenant, agent, conversation string }

func (l Limit) key() limitKey { return limitKey{l.TenantID, l.AgentID, l.ConversationID} }

// Owners answers who owns an agent and who administers the tenant. The
// transport resolves it from the signed-in session and the agent's record.
type Owners interface {
	IsOwner(tenant, agentID, actor string) bool
	IsAdmin(tenant, actor string) bool
}

// Notifier tells an owner something about their agent's spend. It is called
// at most once per limit per day.
type Notifier interface {
	Notify(tenant, ownerID, message string)
}

// AuditEvent records one change to a limit, with the values on each side.
type AuditEvent struct {
	At     time.Time
	Tenant string
	Actor  string
	Agent  string
	Before *Limit
	After  *Limit
}

// Subject names what a call is for, in the words a person reads.
type Subject struct {
	TenantID          string
	AgentID           string
	AgentName         string
	ConversationID    string
	ConversationLabel string
	// EstimateMicros is the most the call is expected to cost.
	EstimateMicros int64
}

// Decision is the gate's answer before a call.
type Decision struct {
	Allowed bool
	// Reached is the plain sentence shown when a limit stops the call.
	Reached string
	// ResetsAt is when the day that is full ends.
	ResetsAt time.Time
	// Unavailable is true when the limits could not be read. The call is not
	// allowed, and no limit has been reached.
	Unavailable bool
}

type dayKey struct {
	limitKey
	day string
}

type usage struct {
	runs, micros int64
	notified     bool
}

// Gate enforces limits before a call and records usage after it.
type Gate struct {
	owners   Owners
	notifier Notifier
	now      func() time.Time
	zone     *time.Location

	mu     sync.Mutex
	limits map[limitKey]Limit
	used   map[dayKey]*usage
	audit  []AuditEvent
	// store and loaded are set by WithStore; without a store the gate is purely
	// in memory.
	store  Store
	loaded map[string]bool
}

// NewGate returns a gate whose days run midnight to midnight in zone. A nil
// zone is UTC and a nil clock is the system clock.
func NewGate(owners Owners, notifier Notifier, zone *time.Location, now func() time.Time) (*Gate, error) {
	if owners == nil {
		return nil, fmt.Errorf("%w: owners are required", ErrInvalid)
	}
	if zone == nil {
		zone = time.UTC
	}
	if now == nil {
		now = func() time.Time { return time.Now() }
	}
	return &Gate{owners: owners, notifier: notifier, now: now, zone: zone, limits: map[limitKey]Limit{}, used: map[dayKey]*usage{}}, nil
}

func (g *Gate) dayOf(at time.Time) (string, time.Time) {
	local := at.In(g.zone)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, g.zone)
	return start.Format("2006-01-02"), start.AddDate(0, 0, 1)
}

// Set creates, changes or (with a zero limit) removes a limit. Raising or
// removing a limit needs an owner of the agent; lowering one may also be done
// by a tenant administrator, so an alarmed administrator can stop spend but
// never allow more. Every change is audited.
func (g *Gate) Set(actor string, next Limit) error {
	if strings.TrimSpace(next.TenantID) == "" || strings.TrimSpace(next.AgentID) == "" || next.MaxRuns < 0 || next.MaxSpendMicros < 0 {
		return ErrInvalid
	}
	removing := next.MaxRuns == 0 && next.MaxSpendMicros == 0
	owner := g.owners.IsOwner(next.TenantID, next.AgentID, actor)
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.hydrateLocked(next.TenantID); err != nil {
		return err
	}
	before, had := g.limits[next.key()]
	if !owner && !(g.owners.IsAdmin(next.TenantID, actor) && !removing && (!had || lowers(before, next))) {
		return ErrDenied
	}
	event := AuditEvent{At: g.now(), Tenant: next.TenantID, Actor: actor, Agent: next.AgentID}
	if had {
		copy := before
		event.Before = &copy
	}
	if !removing {
		copy := next
		event.After = &copy
	}
	// The durable write comes first: a change that was not stored is not made.
	if g.store != nil {
		if err := g.store.SaveLimit(event, next, removing); err != nil {
			return fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
	}
	if removing {
		delete(g.limits, next.key())
	} else {
		g.limits[next.key()] = next
		if !had {
			if err := g.seedUsageLocked(next.key()); err != nil {
				return err
			}
		}
	}
	g.audit = append(g.audit, event)
	return nil
}

// lowers reports whether next is no looser than before in any dimension:
// a ceiling that was set stays set and does not rise, and a dimension that had
// none may gain one.
func lowers(before, next Limit) bool {
	tighter := func(old, now int64) bool {
		if old == 0 {
			return true
		}
		return now != 0 && now <= old
	}
	return tighter(before.MaxRuns, next.MaxRuns) && tighter(before.MaxSpendMicros, next.MaxSpendMicros)
}

// Limits lists an agent's limits to an owner of it, or to a tenant administrator, who
// needs to see a limit to tighten it.
func (g *Gate) Limits(tenant, agentID, actor string) ([]Limit, error) {
	if !g.owners.IsOwner(tenant, agentID, actor) && !g.owners.IsAdmin(tenant, actor) {
		return nil, ErrDenied
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.hydrateLocked(tenant); err != nil {
		return nil, err
	}
	var out []Limit
	for key, limit := range g.limits {
		if key.tenant == tenant && key.agent == agentID {
			out = append(out, limit)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ConversationID < out[j].ConversationID })
	return out, nil
}

// AuditTrail lists the limit changes of an agent, oldest first, to an owner.
func (g *Gate) AuditTrail(tenant, agentID, actor string) ([]AuditEvent, error) {
	if !g.owners.IsOwner(tenant, agentID, actor) {
		return nil, ErrDenied
	}
	if g.store != nil {
		events, err := g.store.LimitAudit(tenant, agentID)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return events, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []AuditEvent
	for _, event := range g.audit {
		if event.Tenant == tenant && event.Agent == agentID {
			out = append(out, event)
		}
	}
	return out, nil
}

// Admit answers whether a call may be made now. It checks the agent's limit
// and the agent's limit for this conversation; the first one the call would
// pass stops it. It does not record anything: Record does, after the call.
func (g *Gate) Admit(subject Subject) Decision {
	at := g.now()
	day, resets := g.dayOf(at)
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.hydrateLocked(subject.TenantID); err != nil {
		return Decision{Unavailable: true, ResetsAt: resets}
	}
	for _, key := range keysFor(subject) {
		limit, ok := g.limits[key]
		if !ok {
			continue
		}
		used := g.used[dayKey{key, day}]
		var runs, micros int64
		if used != nil {
			runs, micros = used.runs, used.micros
		}
		if limit.MaxRuns > 0 && runs+1 > limit.MaxRuns || limit.MaxSpendMicros > 0 && micros+subject.EstimateMicros > limit.MaxSpendMicros {
			return Decision{Reached: reachedSentence(subject, key.conversation != "", resets, g.zone), ResetsAt: resets}
		}
	}
	return Decision{Allowed: true, ResetsAt: resets}
}

// keysFor lists the limits a call counts against: the agent's, and the
// agent's in this conversation when the call has one.
func keysFor(subject Subject) []limitKey {
	keys := []limitKey{{subject.TenantID, subject.AgentID, ""}}
	if subject.ConversationID != "" {
		keys = append(keys, limitKey{subject.TenantID, subject.AgentID, subject.ConversationID})
	}
	return keys
}

func reachedSentence(subject Subject, conversation bool, resets time.Time, zone *time.Location) string {
	name := strings.TrimSpace(subject.AgentName)
	if name == "" {
		name = "This agent"
	}
	where := ""
	if conversation && strings.TrimSpace(subject.ConversationLabel) != "" {
		where = " for " + subject.ConversationLabel
	}
	return fmt.Sprintf("%s reached today's limit%s. It resets at %s.", name, where, resets.In(zone).Format("15:04"))
}

// Record adds a finished call to today's usage under the agent's limit and the
// conversation's limit, and tells the owner once when either reaches 80
// percent of a dimension.
func (g *Gate) Record(subject Subject, spendMicros int64, ownerID string) {
	at := g.now()
	day, _ := g.dayOf(at)
	var notices []string
	g.mu.Lock()
	for _, key := range keysFor(subject) {
		limit, ok := g.limits[key]
		if !ok {
			continue
		}
		slot := g.used[dayKey{key, day}]
		if slot == nil {
			slot = &usage{}
			g.used[dayKey{key, day}] = slot
		}
		slot.runs++
		slot.micros += spendMicros
		if !slot.notified && (limit.MaxRuns > 0 && slot.runs*100 >= limit.MaxRuns*80 || limit.MaxSpendMicros > 0 && slot.micros*100 >= limit.MaxSpendMicros*80) {
			slot.notified = true
			name := subject.AgentName
			if name == "" {
				name = "An agent you own"
			}
			scope := ""
			if key.conversation != "" && subject.ConversationLabel != "" {
				scope = " in " + subject.ConversationLabel
			}
			notices = append(notices, fmt.Sprintf("%s%s has used 80 percent of today's limit.", name, scope))
		}
	}
	notifier := g.notifier
	g.mu.Unlock()
	if notifier != nil {
		for _, notice := range notices {
			notifier.Notify(subject.TenantID, ownerID, notice)
		}
	}
}
