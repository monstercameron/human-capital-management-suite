package agentcost

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// memoryStore is a Store held in memory, so the gate's use of a store is tested
// without a database. The PostgreSQL form is tested in its own package.
type memoryStore struct {
	mu      sync.Mutex
	limits  map[limitKey]Limit
	audit   []AuditEvent
	runs    []Run
	failing bool
}

func newMemoryStore() *memoryStore { return &memoryStore{limits: map[limitKey]Limit{}} }

func (s *memoryStore) fail() error {
	if s.failing {
		return errors.New("store offline")
	}
	return nil
}

func (s *memoryStore) LoadLimits(tenant string) ([]Limit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.fail(); err != nil {
		return nil, err
	}
	var out []Limit
	for key, limit := range s.limits {
		if key.tenant == tenant {
			out = append(out, limit)
		}
	}
	return out, nil
}

func (s *memoryStore) SaveLimit(event AuditEvent, next Limit, removed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.fail(); err != nil {
		return err
	}
	if removed {
		delete(s.limits, next.key())
	} else {
		s.limits[next.key()] = next
	}
	s.audit = append(s.audit, event)
	return nil
}

func (s *memoryStore) LimitAudit(tenant, agentID string) ([]AuditEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []AuditEvent
	for _, event := range s.audit {
		if event.Tenant == tenant && event.Agent == agentID {
			out = append(out, event)
		}
	}
	return out, s.fail()
}

func (s *memoryStore) Usage(tenant, agentID, conversation string, since time.Time) (int64, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.fail(); err != nil {
		return 0, 0, err
	}
	var runs, micros int64
	for _, run := range s.runs {
		if run.TenantID == tenant && run.AgentID == agentID && (conversation == "" || run.ConversationID == conversation) && !run.At.Before(since) {
			runs++
			micros += run.SpendMicros
		}
	}
	return runs, micros, nil
}

func (s *memoryStore) AppendRun(run Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.fail(); err != nil {
		return err
	}
	for _, have := range s.runs {
		if have.TenantID == run.TenantID && have.RunID == run.RunID {
			return nil
		}
	}
	s.runs = append(s.runs, run)
	return nil
}

func (s *memoryStore) RunsSince(tenant string, since time.Time) ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.fail(); err != nil {
		return nil, err
	}
	var out []Run
	for _, run := range s.runs {
		if run.TenantID == tenant && !run.At.Before(since) {
			out = append(out, run)
		}
	}
	return out, nil
}

// A limit, its audit row and today's usage survive a restart: a new gate over
// the same store refuses the call the old gate would have refused.
func TestTodo_AGENTCOST_006_StoreSurvivesRestart(t *testing.T) {
	f := newFixture(t)
	store := newMemoryStore()
	gate := f.gate.WithStore(store)
	if err := gate.Set("olive", Limit{TenantID: "t1", AgentID: "policy", MaxRuns: 2}); err != nil {
		t.Fatal(err)
	}
	meter := Meter{Gate: gate, Ledger: (&Ledger{}).WithStore(store)}
	for i, id := range []string{"r1", "r2"} {
		subject := policy("general")
		if decision := meter.Admit(subject); !decision.Allowed {
			t.Fatalf("run %d refused: %+v", i, decision)
		}
		if err := meter.Finish(subject, Run{TenantID: "t1", AgentID: "policy", ConversationID: "general", RunID: id, At: *f.clock, Kind: KindAnswer, SpendMicros: 50_000, Answered: true}, "olive"); err != nil {
			t.Fatal(err)
		}
	}
	restarted, err := NewGate(f.owners, nil, time.UTC, func() time.Time { return *f.clock })
	if err != nil {
		t.Fatal(err)
	}
	restarted.WithStore(store)
	decision := restarted.Admit(policy("general"))
	if decision.Allowed || decision.Reached != "Policy Helper reached today's limit. It resets at 00:00." {
		t.Fatalf("after a restart: %+v", decision)
	}
	trail, err := restarted.AuditTrail("t1", "policy", "olive")
	if err != nil || len(trail) != 1 || trail[0].Actor != "olive" || trail[0].After == nil || trail[0].After.MaxRuns != 2 {
		t.Fatalf("audit = %+v %v", trail, err)
	}
	// A limit set after runs already happened counts them.
	late, _ := NewGate(f.owners, nil, time.UTC, func() time.Time { return *f.clock })
	late.WithStore(store)
	if err := late.Set("bob", Limit{TenantID: "t1", AgentID: "policy", ConversationID: "random", MaxRuns: 1}); err == nil {
		t.Fatal("a non-owner set a limit")
	}
	if err := late.Set("olive", Limit{TenantID: "t1", AgentID: "policy", MaxRuns: 1}); err != nil {
		t.Fatal(err)
	}
	if decision := late.Admit(policy("")); decision.Allowed {
		t.Fatalf("usage already spent today was not counted: %+v", decision)
	}
}

// When the store cannot be read the gate does not admit the call and does not
// claim a limit was reached; a change that cannot be stored is not made.
func TestTodo_AGENTCOST_006_StoreFailsClosed(t *testing.T) {
	f := newFixture(t)
	store := newMemoryStore()
	gate := f.gate.WithStore(store)
	if err := gate.Set("olive", Limit{TenantID: "t1", AgentID: "policy", MaxRuns: 3}); err != nil {
		t.Fatal(err)
	}
	store.failing = true
	other, _ := NewGate(f.owners, nil, time.UTC, func() time.Time { return *f.clock })
	other.WithStore(store)
	if decision := other.Admit(policy("")); decision.Allowed || !decision.Unavailable || decision.Reached != "" {
		t.Fatalf("an unreadable store admitted or misreported: %+v", decision)
	}
	if err := gate.Set("olive", Limit{TenantID: "t1", AgentID: "policy", MaxRuns: 9}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("an unstored change = %v", err)
	}
	store.failing = false
	limits, err := gate.Limits("t1", "policy", "olive")
	if err != nil || len(limits) != 1 || limits[0].MaxRuns != 3 {
		t.Fatalf("the change that was not stored took effect: %+v %v", limits, err)
	}
	ledger := (&Ledger{}).WithStore(store)
	store.failing = true
	if _, err := ledger.ReportFor(f.owners, "t1", "olive", *f.clock, time.UTC); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("an unreadable ledger drew a report: %v", err)
	}
}
