package subscription

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	ErrManagementNotFound = errors.New("subscription: managed subscription not found")
	ErrCursorInvalid      = errors.New("subscription: invalid event cursor")
)

// Management is the application-facing lifecycle adapter over immutable
// subscription revisions. Every operation appends a revision; no revision is
// edited in place.
type Management struct {
	mu       sync.RWMutex
	registry *Registry
}

func NewManagement() *Management { return &Management{registry: NewRegistry()} }

func (m *Management) Create(request RevisionRequest) (EventSubscription, error) {
	if m == nil {
		return EventSubscription{}, ErrInvalidRevision
	}
	revision, err := NewDraft(request)
	if err != nil {
		return EventSubscription{}, err
	}
	if err := m.registry.Append(revision); err != nil {
		return EventSubscription{}, err
	}
	return revision, nil
}

func (m *Management) Get(id string) (EventSubscription, error) {
	if m == nil {
		return EventSubscription{}, ErrManagementNotFound
	}
	history := m.registry.Revisions(id)
	if len(history) == 0 {
		return EventSubscription{}, ErrManagementNotFound
	}
	return history[len(history)-1], nil
}

func (m *Management) List(tenant string) []EventSubscription {
	if m == nil {
		return nil
	}
	items := m.registry.Active()
	if tenant == "" {
		return items
	}
	out := items[:0]
	for _, item := range items {
		if item.TenantScope == tenant {
			out = append(out, item)
		}
	}
	return out
}

func (m *Management) Pause(id, requester string) (EventSubscription, error) {
	current, err := m.Get(id)
	if err != nil {
		return EventSubscription{}, err
	}
	next, err := current.Pause(requester)
	if err != nil {
		return EventSubscription{}, err
	}
	if err := m.registry.Append(next); err != nil {
		return EventSubscription{}, err
	}
	return next, nil
}

func (m *Management) Resume(id, requester, approver string) (EventSubscription, error) {
	current, err := m.Get(id)
	if err != nil {
		return EventSubscription{}, err
	}
	next, err := current.Activate(requester, approver)
	if err != nil {
		return EventSubscription{}, err
	}
	if err := m.registry.Append(next); err != nil {
		return EventSubscription{}, err
	}
	return next, nil
}

func (m *Management) Delete(id, requester string) (EventSubscription, error) {
	current, err := m.Get(id)
	if err != nil {
		return EventSubscription{}, err
	}
	next, err := current.Revoke(requester)
	if err != nil {
		return EventSubscription{}, err
	}
	if err := m.registry.Append(next); err != nil {
		return EventSubscription{}, err
	}
	return next, nil
}

func (m *Management) Replay(request ReplayRequest) (Replay, error) {
	if m == nil {
		return Replay{}, ErrManagementNotFound
	}
	return PlanReplay(request)
}

// PullEvent is a minimized, already-authorized event projection. Raw payload
// bytes are intentionally absent; fields are supplied only by the event
// producer after matching a subscriber's declared fields.
type PullEvent struct {
	Cursor      uint64
	TenantScope string
	Kind        EventKind
	Digest      string
	Fields      map[string]string
}

type PullFeed struct {
	mu     sync.RWMutex
	next   uint64
	events []PullEvent
}

func NewPullFeed() *PullFeed { return &PullFeed{} }

func (f *PullFeed) Append(event PullEvent) (PullEvent, error) {
	if f == nil || strings.TrimSpace(event.TenantScope) == "" || !event.Kind.Valid() || event.Digest == "" {
		return PullEvent{}, ErrInvalidEventDigest
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	event.Cursor = f.next
	event.Fields = cloneStringMap(event.Fields)
	f.events = append(f.events, event)
	return event, nil
}

func (f *PullFeed) Pull(tenant string, after uint64, limit int) ([]PullEvent, uint64, error) {
	if f == nil || strings.TrimSpace(tenant) == "" || limit <= 0 {
		return nil, after, ErrCursorInvalid
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	if after > f.next {
		return nil, after, fmt.Errorf("%w: %d is ahead of head %d", ErrCursorInvalid, after, f.next)
	}
	out := make([]PullEvent, 0, limit)
	for _, event := range f.events {
		if event.Cursor <= after || event.TenantScope != tenant {
			continue
		}
		out = append(out, PullEvent{Cursor: event.Cursor, TenantScope: event.TenantScope, Kind: event.Kind, Digest: event.Digest, Fields: cloneStringMap(event.Fields)})
		if len(out) == limit {
			break
		}
	}
	next := after
	if len(out) > 0 {
		next = out[len(out)-1].Cursor
	}
	return out, next, nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sortPullEvents(events []PullEvent) {
	sort.Slice(events, func(i, j int) bool { return events[i].Cursor < events[j].Cursor })
}
