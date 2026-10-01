package runstate

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrNotFound = errors.New("runstate: run not found")
	ErrExists   = errors.New("runstate: run already exists")
)

// MemoryStore is a concurrency-safe reference store for tests and local
// composition. Production composition uses the tenant-scoped PostgreSQL store.
type MemoryStore struct {
	mu   sync.RWMutex
	runs map[string]Run
}

// NewMemoryStore creates an empty in-memory state store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{runs: make(map[string]Run)}
}

// Create inserts one run under its stable admission ID.
func (s *MemoryStore) Create(_ context.Context, run Run) error {
	if s == nil || run.ID == "" || run.ID != run.AdmissionID || run.Version != 1 {
		return fmt.Errorf("%w: invalid initial run", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.runs[run.ID]; exists {
		return ErrExists
	}
	s.runs[run.ID] = cloneRun(run)
	return nil
}

// Get returns a defensive copy of one run.
func (s *MemoryStore) Get(_ context.Context, id string) (Run, error) {
	if s == nil {
		return Run{}, ErrInvalid
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[id]
	if !ok {
		return Run{}, ErrNotFound
	}
	return cloneRun(run), nil
}

// Save replaces a run only when expectedVersion is current.
func (s *MemoryStore) Save(_ context.Context, run Run, expectedVersion uint64) error {
	if s == nil || run.ID == "" || run.Version != expectedVersion+1 {
		return fmt.Errorf("%w: invalid revision", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.runs[run.ID]
	if !ok {
		return ErrNotFound
	}
	if current.Version != expectedVersion {
		return ErrConflict
	}
	s.runs[run.ID] = cloneRun(run)
	return nil
}

var _ Store = (*MemoryStore)(nil)
