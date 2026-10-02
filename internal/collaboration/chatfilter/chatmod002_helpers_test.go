package chatfilter

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// memStore is a concurrency-safe in-memory Store that counts reads and keeps
// every record handed to RecordHits, per tenant.
type memStore struct {
	mu        sync.Mutex
	defs      map[string][]Definition
	enabled   map[string][]Enablement
	recorded  []Record
	defReads  int
	enReads   int
	recordRun int
}

func newMemStore() *memStore {
	return &memStore{defs: map[string][]Definition{}, enabled: map[string][]Enablement{}}
}

func (s *memStore) Definitions(_ context.Context, tenant string) ([]Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defReads++
	return append([]Definition(nil), s.defs[tenant]...), nil
}
func (s *memStore) CreateVersion(_ context.Context, tenant string, d Definition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defs[tenant] = append(s.defs[tenant], d)
	return nil
}
func (s *memStore) Enablements(_ context.Context, tenant string) ([]Enablement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enReads++
	return append([]Enablement(nil), s.enabled[tenant]...), nil
}
func (s *memStore) PutEnablement(_ context.Context, tenant string, e Enablement) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, old := range s.enabled[tenant] {
		if old.RuleID == e.RuleID && old.Channel == e.Channel {
			s.enabled[tenant][i] = e
			return nil
		}
	}
	s.enabled[tenant] = append(s.enabled[tenant], e)
	return nil
}
func (s *memStore) RecordHits(_ context.Context, _ string, records []Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordRun++
	s.recorded = append(s.recorded, records...)
	return nil
}
func (s *memStore) Hits(context.Context, string) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Record(nil), s.recorded...), nil
}

// rawEnable switches rules on behind the service's back, as another instance
// of the application would.
func (s *memStore) rawEnable(tenant string, rows ...Enablement) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled[tenant] = append(s.enabled[tenant], rows...)
}
func (s *memStore) reads() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.defReads, s.enReads
}
func (s *memStore) records() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Record(nil), s.recorded...)
}

type recordingDelivery struct {
	mu      sync.Mutex
	records []Record
}

func (d *recordingDelivery) DeliverFilterHit(_ context.Context, r Record) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.records = append(d.records, r)
	return nil
}

// allBuiltinsOn returns a service over a fresh store with all nine built-in
// lists enabled for tenant "t" at workspace scope.
func allBuiltinsOn(t testing.TB) (*Service, *memStore) {
	t.Helper()
	store := newMemStore()
	for _, d := range Builtins() {
		store.rawEnable("t", Enablement{RuleID: d.ID, Enabled: true})
	}
	return &Service{Store: store, Registry: NewRegistry(), Authority: fixtureAuthority{workspace: true}, Delivery: &recordingDelivery{}}, store
}

// compileBuiltins compiles the nine built-in lists with every action replaced
// by action, so a test can read the masked text.
func compileBuiltins(t testing.TB, action string) *Evaluator {
	t.Helper()
	defs := Builtins()
	for i := range defs {
		defs[i].Action = action
	}
	ev, err := NewRegistry().Compile(defs)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func builtinTerms(lang, category string) []string {
	for _, l := range builtinLists() {
		if l.Language == lang && l.Category == category {
			return l.Terms
		}
	}
	return nil
}

func allTerms() (out []builtinList) { return builtinLists() }

// hitSlices returns the original text each hit covers.
func hitSlices(body string, r Result) []string {
	var out []string
	for _, h := range r.Hits {
		out = append(out, body[h.Span.Start:h.Span.End])
	}
	return out
}

func hasSlice(body string, r Result, want string) bool {
	for _, s := range hitSlices(body, r) {
		if s == want {
			return true
		}
	}
	return false
}

func hasSpan(r Result, start, end int) bool {
	for _, h := range r.Hits {
		if h.Span.Start == start && h.Span.End == end {
			return true
		}
	}
	return false
}

func fixedClock() (func() time.Time, *time.Time) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	return func() time.Time { return now }, &now
}

func joinLetters(s, sep string) string {
	var letters []string
	for _, r := range s {
		if r == ' ' || r == '\'' {
			continue
		}
		letters = append(letters, string(r))
	}
	return strings.Join(letters, sep)
}
