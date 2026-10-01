package scheduled

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
)

const testAgentDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
const testAudienceDigest = "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
const testContextDigest = "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

type scheduleStateFunc func(context.Context, Firing) error

func (f scheduleStateFunc) CheckCurrent(ctx context.Context, firing Firing) error {
	return f(ctx, firing)
}

type workFenceFunc func(context.Context, Receipt) error

func (f workFenceFunc) CheckBeforeWork(ctx context.Context, receipt Receipt) error {
	return f(ctx, receipt)
}

type receiptStore struct {
	mu       sync.Mutex
	records  map[string]Receipt
	failNext bool
}

func newReceiptStore() *receiptStore { return &receiptStore{records: make(map[string]Receipt)} }

func (s *receiptStore) Get(_ context.Context, key string) (Receipt, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[key]
	return record, ok, nil
}

func (s *receiptStore) CreateOrGet(_ context.Context, candidate Receipt) (Receipt, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext {
		s.failNext = false
		return Receipt{}, false, errors.New("receipt store unavailable")
	}
	if prior, ok := s.records[candidate.SourceKey]; ok {
		if prior != candidate {
			return Receipt{}, false, ErrReceiptConflict
		}
		return prior, false, nil
	}
	s.records[candidate.SourceKey] = candidate
	return candidate, true, nil
}

type testAuthority struct {
	mu    sync.Mutex
	calls int
}

func (a *testAuthority) VerifyAdmission(_ context.Context, request agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	a.mu.Lock()
	a.calls++
	a.mu.Unlock()
	return agentrun.AuthoritySnapshot{
		Agent: request.Agent, InstallationID: request.InstallationID, Principal: request.Principal,
		Audience: request.Audience, Context: request.Context, BudgetCeiling: request.Budget,
		GrantRef: "grant:current", PolicyDigest: testContextDigest,
	}, nil
}

func (a *testAuthority) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

func TestTodo_AGENT_031(t *testing.T) {
	ctx := context.Background()
	firing := testFiring()
	stateCalls := 0
	dispatcher, inbox, records, receipts := testDispatcher(t, func(context.Context, Firing) error {
		stateCalls++
		return nil
	})
	request := testRequest(firing)
	first, err := dispatcher.Dispatch(ctx, firing, request)
	if err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	second, err := dispatcher.Dispatch(ctx, firing, request)
	if err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	if first.Receipt.RunRequestID == "" || first.Duplicate || !second.Duplicate || second.Receipt != first.Receipt {
		t.Fatalf("first=%+v replay=%+v", first, second)
	}
	if stateCalls != 1 || inbox.authority.callCount() != 1 || !hasAdmission(t, records, request.Source) || len(receipts.records) != 1 {
		t.Fatalf("state checks=%d authority checks=%d admission present=%v receipts=%d", stateCalls, inbox.authority.callCount(), hasAdmission(t, records, request.Source), len(receipts.records))
	}
}

func TestTodo_AGENT_031_Race(t *testing.T) {
	dispatcher, inbox, records, receipts := testDispatcher(t, func(context.Context, Firing) error { return nil })
	firing, request := testFiring(), testRequest(testFiring())
	const workers = 20
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := dispatcher.Dispatch(context.Background(), firing, request); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent dispatch: %v", err)
	}
	if !hasAdmission(t, records, request.Source) || len(receipts.records) != 1 {
		t.Fatalf("admission present=%v receipts=%d", hasAdmission(t, records, request.Source), len(receipts.records))
	}
	if inbox.authority.callCount() < 1 {
		t.Fatal("no admission authority check occurred")
	}
}

func TestTodo_AGENT_031_Fault(t *testing.T) {
	dispatcher, inbox, records, receipts := testDispatcher(t, func(context.Context, Firing) error { return nil })
	receipts.failNext = true
	firing, request := testFiring(), testRequest(testFiring())
	if _, err := dispatcher.Dispatch(context.Background(), firing, request); err == nil {
		t.Fatal("expected receipt persistence failure")
	}
	if !hasAdmission(t, records, request.Source) || len(receipts.records) != 0 {
		t.Fatalf("admission present=%v receipts=%d after receipt fault", hasAdmission(t, records, request.Source), len(receipts.records))
	}
	outcome, err := dispatcher.Dispatch(context.Background(), firing, request)
	if err != nil {
		t.Fatalf("redrive after receipt fault: %v", err)
	}
	if !outcome.Duplicate || !hasAdmission(t, records, request.Source) || len(receipts.records) != 1 {
		t.Fatalf("outcome=%+v admission present=%v receipts=%d", outcome, hasAdmission(t, records, request.Source), len(receipts.records))
	}
	if inbox.authority.callCount() != 2 {
		t.Fatalf("authority calls=%d; retry must return the durable inbox decision", inbox.authority.callCount())
	}
}

func TestTodo_AGENT_031_Recovery(t *testing.T) {
	ctx := context.Background()
	firing, request := testFiring(), testRequest(testFiring())
	inbox, records := testInbox()
	receipts := newReceiptStore()
	firstDispatcher, err := NewDispatcher(scheduleStateFunc(func(context.Context, Firing) error { return nil }), inbox, receipts, workFenceFunc(func(context.Context, Receipt) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	first, err := firstDispatcher.Dispatch(ctx, firing, request)
	if err != nil {
		t.Fatal(err)
	}
	// A fresh dispatcher models worker restart while both owner stores survive.
	restarted, err := NewDispatcher(scheduleStateFunc(func(context.Context, Firing) error { return nil }), inbox, receipts, workFenceFunc(func(context.Context, Receipt) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := restarted.Dispatch(ctx, firing, request)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Duplicate || replayed.Receipt != first.Receipt || !hasAdmission(t, records, request.Source) {
		t.Fatalf("first=%+v replay=%+v admission present=%v", first, replayed, hasAdmission(t, records, request.Source))
	}
}

func TestTodo_AGENT_031_Security(t *testing.T) {
	dispatcher, inbox, records, receipts := testDispatcher(t, func(context.Context, Firing) error { return nil })
	firing := testFiring()
	identity, err := firing.SourceIdentity()
	if err != nil {
		t.Fatal(err)
	}
	changed := firing
	changed.Occurrence.Trigger.Version = "revision-8"
	changedID, err := changed.SourceIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if identity.Key == changedID.Key || identity.TenantID != "tenant-7" || identity.Kind != agentrun.SourceSchedule {
		t.Fatalf("identity=%+v changed=%+v", identity, changedID)
	}
	base := testRequest(firing)
	for name, mutate := range map[string]func(*agentrun.Request){
		"agent":                    func(r *agentrun.Request) { r.Agent.Version = "4" },
		"sponsor":                  func(r *agentrun.Request) { r.Principal.SponsorID = "different-sponsor" },
		"purpose":                  func(r *agentrun.Request) { r.Purpose = "different-purpose" },
		"budget":                   func(r *agentrun.Request) { r.Budget.MaxCostMicros++ },
		"audience":                 func(r *agentrun.Request) { r.Audience.ID = "different-audience" },
		"borrowed-human-authority": func(r *agentrun.Request) { r.Principal.InvokerID = "human-1" },
	} {
		candidate := base
		mutate(&candidate)
		if _, err := dispatcher.Dispatch(context.Background(), firing, candidate); !errors.Is(err, ErrInvalidFiring) {
			t.Errorf("%s mismatch error=%v", name, err)
		}
	}
	if hasAdmission(t, records, agentrun.SourceIdentity{TenantID: "tenant-7", Kind: agentrun.SourceSchedule, Key: identity.Key}) || len(receipts.records) != 0 || inbox.authority.callCount() != 0 {
		t.Fatalf("mismatch caused side effects: receipts=%d authority=%d", len(receipts.records), inbox.authority.callCount())
	}
	denied, _, _, _ := testDispatcher(t, func(context.Context, Firing) error { return errors.New("schedule paused") })
	if _, err := denied.Dispatch(context.Background(), firing, testRequest(firing)); !errors.Is(err, ErrFiringRefused) {
		t.Fatalf("paused schedule error=%v", err)
	}
	revoked := false
	dispatcher.work = workFenceFunc(func(context.Context, Receipt) error {
		if revoked {
			return errors.New("sponsor grant revoked")
		}
		return nil
	})
	accepted, err := dispatcher.Dispatch(context.Background(), firing, testRequest(firing))
	if err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.CheckBeforeWork(context.Background(), accepted.Receipt); err != nil {
		t.Fatalf("current grant should permit work: %v", err)
	}
	revoked = true
	if err := dispatcher.CheckBeforeWork(context.Background(), accepted.Receipt); !errors.Is(err, ErrWorkRefused) {
		t.Fatalf("revoked work error=%v", err)
	}
	if err := dispatcher.CheckBeforeWork(context.Background(), Receipt{SourceKey: identity.Key, RunRequestID: "refused", RequestDigest: "digest", Decision: agentrun.DecisionRefused, RefusalCode: "AUTHORITY_REFUSED"}); !errors.Is(err, ErrWorkRefused) {
		t.Fatalf("refused run work gate error=%v", err)
	}
}

func TestFiring_SourceIdentityRejectsEventOccurrence(t *testing.T) {
	firing := testFiring()
	firing.Occurrence.Source = schedule.SourceEvent
	if _, err := firing.SourceIdentity(); !errors.Is(err, ErrInvalidFiring) {
		t.Fatalf("event occurrence error=%v", err)
	}
}

type testInboxAdapter struct {
	service   *agentrun.AdmissionService
	authority *testAuthority
}

func (a *testInboxAdapter) Admit(ctx context.Context, request agentrun.Request) (agentrun.Record, bool, error) {
	return a.service.Admit(ctx, request)
}

func testDispatcher(t *testing.T, check func(context.Context, Firing) error) (*Dispatcher, *testInboxAdapter, *agentrun.MemoryAdmissionStore, *receiptStore) {
	t.Helper()
	inbox, records := testInbox()
	receipts := newReceiptStore()
	dispatcher, err := NewDispatcher(scheduleStateFunc(check), inbox, receipts, workFenceFunc(func(context.Context, Receipt) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	return dispatcher, inbox, records, receipts
}

func testInbox() (*testInboxAdapter, *agentrun.MemoryAdmissionStore) {
	authority := &testAuthority{}
	store := agentrun.NewMemoryAdmissionStore()
	service, _ := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: authority, Store: store, Now: func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) }})
	return &testInboxAdapter{service: service, authority: authority}, store
}

func hasAdmission(t *testing.T, store *agentrun.MemoryAdmissionStore, source agentrun.SourceIdentity) bool {
	t.Helper()
	_, err := store.Get(source)
	return err == nil
}

func testFiring() Firing {
	return Firing{
		Occurrence: schedule.Occurrence{Trigger: schedule.TriggerRef{TenantID: "tenant-7", ID: "schedule-3", Version: "revision-7"}, Key: "occurrence:2026-09-29T12:00Z", Source: schedule.SourceCron},
		Target: schedule.AgentRunTarget{
			Agent:     schedule.AgentVersionRef{ID: "agent-2", Version: "3", Digest: testAgentDigest},
			SponsorID: "sponsor-9", Purpose: "weekly.digest",
			Budget:      schedule.AgentRunBudget{MaxCostMicros: 1000, MaxInputTokens: 2000, MaxOutputTokens: 500},
			Destination: schedule.AgentRunDestination{AudienceID: "audience-5", AudienceSnapshotID: "snapshot-2", AudienceDigest: testAudienceDigest},
		},
	}
}

func testRequest(firing Firing) agentrun.Request {
	source, _ := firing.SourceIdentity()
	return agentrun.Request{
		Source:      source,
		LegalEntity: "entity-west", Agent: targetAgentRef(firing.Target), InstallationID: "install-4",
		Principal: agentrun.PrincipalChain{Mode: agentrun.ModeSponsored, AgentPrincipalID: "principal-agent-2", SponsorID: firing.Target.SponsorID},
		Purpose:   firing.Target.Purpose, Audience: targetAudience(firing.Target.Destination),
		Context:  agentrun.ContextScope{ID: "schedule-context", SnapshotID: "context-snapshot-2", Digest: testContextDigest},
		Deadline: time.Date(2026, 9, 29, 13, 0, 0, 0, time.UTC), Budget: targetBudget(firing.Target.Budget), CauseID: "cause:schedule-3",
	}
}
