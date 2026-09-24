package reservation

import (
	"github.com/google/uuid"
	"sort"
	"sync"
	"time"
)

// Store is a concurrency-safe reference implementation of the protocol.
// Production adapters may persist the same compare-and-swap decisions in an
// ACID store; remote providers must be reconciled, never described as atomic.
type Store struct {
	mu        sync.Mutex
	nextFence uint64
	seq       uint64
	items     map[uuid.UUID]*Reservation
	byKey     map[string]uuid.UUID
	events    map[uuid.UUID][]Event
}

func NewStore() *Store {
	return &Store{items: make(map[uuid.UUID]*Reservation), byKey: make(map[string]uuid.UUID), events: make(map[uuid.UUID][]Event)}
}
func key(r Request) string { return r.Resource + "\x00" + r.Owner + "\x00" + r.IdempotencyKey }

// Acquire atomically checks expiry, proposal identity and overlapping active
// quantity, then allocates a strictly increasing fence. Repeating the exact
// request returns the same hold; reusing its key for a changed proposal is a
// typed conflict and cannot consume the old hold.
func (s *Store) Acquire(req Request, capacity Quantity, now time.Time) (Reservation, error) {
	holds, err := s.AcquireBatch([]AcquireRequest{{Request: req, Capacity: capacity}}, now)
	if err != nil {
		return Reservation{}, err
	}
	return holds[0], nil
}

type AcquireRequest struct {
	Request  Request
	Capacity Quantity
}

// AcquireBatch reserves every requested resource under one lock. Validation,
// idempotency and capacity checks complete before any fence is written.
func (s *Store) AcquireBatch(requests []AcquireRequest, now time.Time) ([]Reservation, error) {
	if len(requests) == 0 {
		return nil, wrap(CodeInvalid, uuid.Nil, ErrInvalidRequest)
	}
	for _, request := range requests {
		if err := request.Request.Validate(now); err != nil {
			return nil, wrap(CodeInvalid, uuid.Nil, err)
		}
		if request.Capacity.Scale != request.Request.Quantity.Scale || request.Capacity.Value <= 0 {
			return nil, wrap(CodeInvalid, uuid.Nil, ErrInvalidQuantity)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = make(map[uuid.UUID]*Reservation)
	}
	if s.byKey == nil {
		s.byKey = make(map[string]uuid.UUID)
	}
	if s.events == nil {
		s.events = make(map[uuid.UUID][]Event)
	}

	keys := make(map[string]struct{}, len(requests))
	results := make([]Reservation, len(requests))
	existing := 0
	for i, candidate := range requests {
		k := key(candidate.Request)
		if _, duplicate := keys[k]; duplicate {
			return nil, wrap(CodeInvalid, uuid.Nil, ErrConflict)
		}
		keys[k] = struct{}{}
		if oldID, ok := s.byKey[k]; ok {
			old := *s.items[oldID]
			if old.Request.ProposalDigest != candidate.Request.ProposalDigest || old.Request.AuthorityDigest != candidate.Request.AuthorityDigest || old.Request.Quantity != candidate.Request.Quantity || old.Request.Interval != candidate.Request.Interval {
				return nil, wrap(CodeConflict, old.ID, ErrConflict)
			}
			results[i] = old
			existing++
		}
	}
	if existing != 0 {
		if existing != len(requests) {
			return nil, wrap(CodeConflict, uuid.Nil, ErrConflict)
		}
		return results, nil
	}

	// Include earlier requests in this same batch when checking shared resource capacity.
	for i, candidate := range requests {
		req, capacity := candidate.Request, candidate.Capacity
		used := Quantity{Scale: req.Quantity.Scale}
		for _, old := range s.items {
			if (old.Status != Held && old.Status != Committed) || old.Request.Resource != req.Resource || old.Request.Version != req.Version || !old.Request.Interval.Overlaps(req.Interval) {
				continue
			}
			var err error
			used, err = used.Add(old.Request.Quantity)
			if err != nil {
				return nil, wrap(CodeCapacity, uuid.Nil, err)
			}
		}
		for j := 0; j < i; j++ {
			prior := requests[j].Request
			if prior.Resource != req.Resource || prior.Version != req.Version || !prior.Interval.Overlaps(req.Interval) {
				continue
			}
			var err error
			used, err = used.Add(prior.Quantity)
			if err != nil {
				return nil, wrap(CodeCapacity, uuid.Nil, err)
			}
		}
		if used.Value > capacity.Value-req.Quantity.Value {
			return nil, wrap(CodeCapacity, uuid.Nil, ErrCapacity)
		}
	}
	for i, candidate := range requests {
		s.nextFence++
		s.seq++
		id := uuid.New()
		item := &Reservation{ID: id, Request: candidate.Request, Capacity: candidate.Capacity, Status: Held, Fence: s.nextFence, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
		s.items[id] = item
		s.byKey[key(candidate.Request)] = id
		s.events[id] = append(s.events[id], Event{Sequence: s.seq, ReservationID: id, To: Held, Fence: item.Fence, At: now.UTC()})
		results[i] = *item
	}
	return results, nil
}

func (s *Store) transition(id uuid.UUID, fence uint64, to Status, now time.Time) (Reservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Reservation{}, wrap(CodeInvalid, id, ErrNotFound)
	}
	if item.Fence != fence {
		return Reservation{}, wrap(CodeFence, id, ErrFence)
	}
	if to == Consumed && item.Status == Held && !item.Request.ExpiresAt.After(now) {
		// Consumption is the execution barrier: a caller must not be able to
		// consume a hold merely because expiry processing has not run yet.
		// Record the expiry transition first so a refused execution cannot
		// leave the reservation looking available to a later retry.
		item.Status, item.UpdatedAt = Expired, now.UTC()
		s.seq++
		s.events[id] = append(s.events[id], Event{Sequence: s.seq, ReservationID: id, From: Held, To: Expired, Fence: fence, At: now.UTC()})
		return *item, wrap(CodeExpired, id, ErrExpired)
	}
	if to == Consumed && item.Status == Expired {
		return *item, wrap(CodeExpired, id, ErrExpired)
	}
	if item.Status == to {
		return *item, nil
	} // idempotent replay
	if item.Status != Held || !to.Terminal() {
		return Reservation{}, wrap(CodeTransition, id, ErrInvalidTransition)
	}
	item.Status, item.UpdatedAt = to, now.UTC()
	s.seq++
	s.events[id] = append(s.events[id], Event{Sequence: s.seq, ReservationID: id, From: Held, To: to, Fence: fence, At: now.UTC()})
	return *item, nil
}
func (s *Store) Consume(id uuid.UUID, fence uint64, now time.Time) (Reservation, error) {
	return s.transition(id, fence, Consumed, now)
}
func (s *Store) Release(id uuid.UUID, fence uint64, now time.Time) (Reservation, error) {
	return s.transition(id, fence, Released, now)
}

// TransitionBatch applies one lifecycle change to a complete fence set or
// changes none of them. Committed stays capacity-active; appointment booking
// uses it to keep a confirmed appointment fenced until release.
func (s *Store) TransitionBatch(fences []Fence, to Status, now time.Time) ([]Reservation, error) {
	if len(fences) == 0 || (to != Committed && to != Released && to != Expired) {
		return nil, wrap(CodeInvalid, uuid.Nil, ErrInvalidTransition)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]*Reservation, len(fences))
	seen := make(map[uuid.UUID]struct{}, len(fences))
	changed := false
	for i, fence := range fences {
		if _, duplicate := seen[fence.ID]; duplicate {
			return nil, wrap(CodeInvalid, fence.ID, ErrInvalidRequest)
		}
		seen[fence.ID] = struct{}{}
		item, ok := s.items[fence.ID]
		if !ok {
			return nil, wrap(CodeInvalid, fence.ID, ErrNotFound)
		}
		if item.Fence != fence.Token {
			return nil, wrap(CodeFence, fence.ID, ErrFence)
		}
		if item.Status == to {
			items[i] = item
			continue
		}
		switch to {
		case Committed:
			if item.Status != Held {
				return nil, wrap(CodeTransition, fence.ID, ErrInvalidTransition)
			}
			if !item.Request.ExpiresAt.After(now) {
				return nil, wrap(CodeInvalid, fence.ID, ErrExpired)
			}
		case Released:
			if item.Status != Held && item.Status != Committed {
				return nil, wrap(CodeTransition, fence.ID, ErrInvalidTransition)
			}
		case Expired:
			if item.Status != Held || item.Request.ExpiresAt.After(now) {
				return nil, wrap(CodeTransition, fence.ID, ErrInvalidTransition)
			}
		}
		items[i] = item
		changed = true
	}
	if !changed {
		out := make([]Reservation, len(items))
		for i, item := range items {
			out[i] = *item
		}
		return out, nil
	}
	for i, item := range items {
		if item.Status == to {
			continue
		}
		from := item.Status
		item.Status, item.UpdatedAt = to, now.UTC()
		s.seq++
		s.events[item.ID] = append(s.events[item.ID], Event{Sequence: s.seq, ReservationID: item.ID, From: from, To: to, Fence: item.Fence, At: now.UTC()})
		items[i] = item
	}
	out := make([]Reservation, len(items))
	for i, item := range items {
		out[i] = *item
	}
	return out, nil
}

func (s *Store) CommitBatch(fences []Fence, now time.Time) ([]Reservation, error) {
	return s.TransitionBatch(fences, Committed, now)
}
func (s *Store) ReleaseBatch(fences []Fence, now time.Time) ([]Reservation, error) {
	return s.TransitionBatch(fences, Released, now)
}
func (s *Store) ExpireBatch(fences []Fence, now time.Time) ([]Reservation, error) {
	return s.TransitionBatch(fences, Expired, now)
}

// UpdateIntervalBatch atomically moves every fence to the same interval after
// checking capacity with all batch members excluded from their old intervals.
// Updated fencing tokens invalidate callers that still hold the old tokens.
func (s *Store) UpdateIntervalBatch(fences []Fence, interval Interval, now time.Time) ([]Reservation, error) {
	if len(fences) == 0 || interval.Validate() != nil {
		return nil, wrap(CodeInvalid, uuid.Nil, ErrInvalidInterval)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]*Reservation, len(fences))
	ids := make(map[uuid.UUID]struct{}, len(fences))
	for i, fence := range fences {
		if _, duplicate := ids[fence.ID]; duplicate {
			return nil, wrap(CodeInvalid, fence.ID, ErrInvalidRequest)
		}
		ids[fence.ID] = struct{}{}
		item, ok := s.items[fence.ID]
		if !ok {
			return nil, wrap(CodeInvalid, fence.ID, ErrNotFound)
		}
		if item.Fence != fence.Token {
			return nil, wrap(CodeFence, fence.ID, ErrFence)
		}
		if item.Status != Held && item.Status != Committed {
			return nil, wrap(CodeTransition, fence.ID, ErrInvalidTransition)
		}
		if item.Status == Held && !item.Request.ExpiresAt.After(now) {
			return nil, wrap(CodeInvalid, fence.ID, ErrExpired)
		}
		items[i] = item
	}
	for _, item := range items {
		used := Quantity{Scale: item.Request.Quantity.Scale}
		for _, other := range s.items {
			if _, inBatch := ids[other.ID]; inBatch {
				continue
			}
			if (other.Status != Held && other.Status != Committed) || other.Request.Resource != item.Request.Resource || other.Request.Version != item.Request.Version || !other.Request.Interval.Overlaps(interval) {
				continue
			}
			var err error
			used, err = used.Add(other.Request.Quantity)
			if err != nil {
				return nil, wrap(CodeCapacity, item.ID, err)
			}
		}
		for _, other := range items {
			if other.ID == item.ID || other.Request.Resource != item.Request.Resource || other.Request.Version != item.Request.Version {
				continue
			}
			var err error
			used, err = used.Add(other.Request.Quantity)
			if err != nil {
				return nil, wrap(CodeCapacity, item.ID, err)
			}
		}
		if used.Value > item.Capacity.Value-item.Request.Quantity.Value {
			return nil, wrap(CodeCapacity, item.ID, ErrCapacity)
		}
	}
	out := make([]Reservation, len(items))
	for i, item := range items {
		if item.Request.Interval != interval {
			from := item.Status
			item.Request.Interval = interval
			s.nextFence++
			item.Fence = s.nextFence
			item.UpdatedAt = now.UTC()
			s.seq++
			s.events[item.ID] = append(s.events[item.ID], Event{Sequence: s.seq, ReservationID: item.ID, From: from, To: from, Fence: item.Fence, At: now.UTC()})
		}
		out[i] = *item
	}
	return out, nil
}

// Expire transitions eligible holds to EXPIRED; it is explicit and caller
// clocked, so tests and recovery do not depend on a hidden wall clock.
func (s *Store) Expire(now time.Time) []Reservation {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Reservation
	for _, item := range s.items {
		if item.Status == Held && !item.Request.ExpiresAt.After(now) {
			item.Status, item.UpdatedAt = Expired, now.UTC()
			s.seq++
			s.events[item.ID] = append(s.events[item.ID], Event{Sequence: s.seq, ReservationID: item.ID, From: Held, To: Expired, Fence: item.Fence, At: now.UTC()})
			out = append(out, *item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out
}
func (s *Store) Get(id uuid.UUID) (Reservation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Reservation{}, false
	}
	return *item, true
}
func (s *Store) Events(id uuid.UUID) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Event(nil), s.events[id]...)
	return out
}
