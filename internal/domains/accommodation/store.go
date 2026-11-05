package accommodation

import (
	"fmt"
	"sync"
)

// Store is the in-memory case registry the capability handlers run against.
// Every boundary copies and verifies: Create and Update store verified
// clones, and Get returns a verified clone, so callers can neither alias
// stored state nor persist a tampered case.
type Store struct {
	mu    sync.RWMutex
	cases map[string]Case
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{cases: make(map[string]Case)}
}

// Create opens a case in the store. Reopening an identity fails: case
// history is append-only, never replaced.
func (s *Store) Create(c Case) error {
	if s == nil {
		return fmt.Errorf("%w: nil store", ErrInvalidRecord)
	}
	if c.ID == "" {
		return fmt.Errorf("%w: case id is required", ErrInvalidRecord)
	}
	if err := c.Verify(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.cases[c.ID]; dup {
		return fmt.Errorf("%w: %q", ErrCaseExists, c.ID)
	}
	s.cases[c.ID] = c.Clone()
	return nil
}

// Get returns a verified deep copy of the stored case. A broken seal fails
// the read: tampered state never leaves the store, even to writers.
func (s *Store) Get(id string) (Case, bool) {
	if s == nil {
		return Case{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.cases[id]
	if !ok {
		return Case{}, false
	}
	if err := c.Verify(); err != nil {
		return Case{}, false
	}
	return c.Clone(), true
}

// Update replaces the stored case after verifying every seal. It refuses
// unknown identities: updates never invent cases.
func (s *Store) Update(c Case) error {
	if s == nil {
		return fmt.Errorf("%w: nil store", ErrInvalidRecord)
	}
	if err := c.Verify(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.cases[c.ID]; !ok {
		return fmt.Errorf("%w: %q", ErrUnknownCase, c.ID)
	}
	s.cases[c.ID] = c.Clone()
	return nil
}
