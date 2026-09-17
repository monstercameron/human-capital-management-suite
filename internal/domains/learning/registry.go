// Registry core: caller authorization, the effect journal, trusted
// providers and the shared stores behind the learning lifecycle.
package learning

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

var (
	ErrNotAuthorized = errors.New("learning: caller is not authorized")
	ErrUnknownCourse = errors.New("learning: unknown course or version")
)

// Caller carries the acting principal and the tenants it may touch.
type Caller struct {
	ID      string
	Tenants []string
}

// JournalEntry is bounded evidence of a successful state change.
type JournalEntry struct {
	Op     string
	Ref    string
	Detail string
}

// Rejection is the typed LEARN denial. Code is LEARN_001_REJECTED or
// LEARN_007_REJECTED depending on the boundary that refused.
type Rejection struct {
	Code    string
	Field   string
	State   string
	Version string
}

func (e *Rejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s", e.Code, e.Field, e.State, e.Version)
}

// AsRejection reports whether err is a *Rejection.
func AsRejection(err error) (*Rejection, bool) {
	var rej *Rejection
	if errors.As(err, &rej) {
		return rej, true
	}
	return nil, false
}

// Registry is the learning book of record.
type Registry struct {
	mu            sync.Mutex
	courses       CourseStore
	completions   CompletionLog
	paths         map[string]LearningPath
	latest        map[string]uint64
	assignments   map[string]Assignment
	enrollCounts  map[string]int
	equivalencies []EquivalencyRule
	providers     map[string]bool
	credentials   map[string]Credential
	renewed       map[string]bool
	warned        map[string]bool
	lmsObs        []LMSObservation
	journal       []JournalEntry
}

// NewRegistry returns an empty registry on in-memory stores.
func NewRegistry() *Registry {
	return &Registry{
		courses:      NewMemoryCourseStore(),
		completions:  NewMemoryCompletionLog(),
		paths:        map[string]LearningPath{},
		latest:       map[string]uint64{},
		assignments:  map[string]Assignment{},
		enrollCounts: map[string]int{},
		providers:    map[string]bool{},
		credentials:  map[string]Credential{},
		renewed:      map[string]bool{},
		warned:       map[string]bool{},
	}
}

// NewRegistryWithCourseStore shares one course store across registries.
func NewRegistryWithCourseStore(store CourseStore) *Registry {
	r := NewRegistry()
	r.courses = store
	return r
}

// NewRegistryWithCompletionLog shares one completion log across registries.
func NewRegistryWithCompletionLog(log CompletionLog) *Registry {
	r := NewRegistry()
	r.completions = log
	return r
}

// NewRegistryWithStores shares one course store and one completion log
// across registries.
func NewRegistryWithStores(store CourseStore, log CompletionLog) *Registry {
	r := NewRegistry()
	r.courses = store
	r.completions = log
	return r
}

// Journal returns the effect evidence recorded so far.
func (r *Registry) Journal() []JournalEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]JournalEntry(nil), r.journal...)
}

func (r *Registry) authorize(caller Caller, tenant string) error {
	if strings.TrimSpace(caller.ID) == "" || strings.TrimSpace(tenant) == "" {
		return fmt.Errorf("%w: missing caller or tenant", ErrNotAuthorized)
	}
	for _, t := range caller.Tenants {
		if t == tenant {
			return nil
		}
	}
	// Generic denial: never name which tenants exist.
	return fmt.Errorf("%w: scope denied", ErrNotAuthorized)
}

// RegisterProvider trusts one completion source authority.
func (r *Registry) RegisterProvider(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[strings.TrimSpace(id)] = true
}

// trustedProvider reports whether id is a trusted source. The caller must
// hold r.mu: every call site already does, so this never locks.
func (r *Registry) trustedProvider(id string) bool {
	return r.providers[id]
}
