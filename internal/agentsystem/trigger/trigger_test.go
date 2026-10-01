package trigger

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

type fixture struct {
	service *Service
	store   *memoryStore
	access  *memoryAccess
	ledger  *memoryLedger
	sub     Subscription
	event   CommittedEvent
}

type memoryStore struct {
	mu            sync.Mutex
	sub           Subscription
	event         CommittedEvent
	resolveErr    error
	eventErr      error
	pauseErr      error
	paused        bool
	pauseRevision uint64
	revokeOnWrite bool
	resolveCalls  int
	eventCalls    int
	pauseCalls    int
}

func (m *memoryStore) ResolveSubscription(_ context.Context, tenantID, channelID, installationID string) (Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resolveCalls++
	if m.resolveErr != nil {
		return Subscription{}, m.resolveErr
	}
	if m.sub.TenantID != tenantID || m.sub.ChannelID != channelID || m.sub.InstallationID != installationID {
		return Subscription{}, errors.New("subscription not found")
	}
	return m.sub, nil
}

func (m *memoryStore) ResolveCommittedEvent(_ context.Context, _, outboxID string) (CommittedEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventCalls++
	if m.eventErr != nil {
		return CommittedEvent{}, m.eventErr
	}
	if m.event.OutboxID != outboxID {
		return CommittedEvent{}, errors.New("committed event not found")
	}
	return m.event, nil
}

func (m *memoryStore) SetTriggerPaused(_ context.Context, actor Actor, sub Subscription, paused bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pauseCalls++
	if m.pauseErr != nil {
		return m.pauseErr
	}
	if m.revokeOnWrite || actor.TenantID != sub.TenantID || actor.PrincipalID == "" {
		return ErrDenied
	}
	if sub.Revision != m.pauseRevision {
		return ErrConflict
	}
	m.paused = paused
	m.pauseRevision++
	return nil
}

type memoryAccess struct{ err error }

func (a *memoryAccess) CheckTriggerAccess(context.Context, Subscription, CommittedEvent) error {
	return a.err
}

type memoryPauseAuth struct{ err error }

func (a memoryPauseAuth) AuthorizeTriggerPause(context.Context, Actor, Subscription) error {
	return a.err
}

type memoryLedger struct {
	mu           sync.Mutex
	seen         map[string]struct{}
	reservations []Reservation
	err          error
}

func (l *memoryLedger) ReserveTrigger(_ context.Context, request Reservation) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return l.err
	}
	if l.seen == nil {
		l.seen = make(map[string]struct{})
	}
	if _, duplicate := l.seen[request.SourceKey]; duplicate {
		return ErrReplay
	}
	l.seen[request.SourceKey] = struct{}{}
	l.reservations = append(l.reservations, request)
	return nil
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	store := &memoryStore{sub: validSubscription(), event: validEvent(), pauseRevision: 7}
	access := &memoryAccess{}
	ledger := &memoryLedger{}
	service, err := New(store, store, access, ledger, memoryPauseAuth{}, store, func() time.Time { return now })
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &fixture{service: service, store: store, access: access, ledger: ledger, sub: store.sub, event: store.event}
}

func validSubscription() Subscription {
	channel := Budget{EventsPerMinute: 20, EventsPerDay: 500, CostMicrosPerMinute: 100_000, CostMicrosPerDay: 2_000_000, MaxConcurrent: 8, Cooldown: 15 * time.Second}
	share := Budget{EventsPerMinute: 4, EventsPerDay: 100, CostMicrosPerMinute: 20_000, CostMicrosPerDay: 400_000, MaxConcurrent: 2, Cooldown: time.Minute}
	return Subscription{
		TenantID: "tenant-a", ChannelID: "channel-a", InstallationID: "install-a", AgentID: "agent-a",
		AgentVersion: "v3", SponsorID: "sponsor-a", Purpose: "channel.assist", AudienceID: "audience-a",
		Revision: 7, Enabled: true, EventClasses: []EventClass{EventPostCreated}, MaxCauseDepth: 3,
		ChannelBudget: channel, FairShare: share,
	}
}

func validEvent() CommittedEvent {
	return CommittedEvent{OutboxID: "outbox-a", PostID: "post-a", TenantID: "tenant-a", ChannelID: "channel-a", Class: EventPostCreated, Origin: OriginHuman, EstimatedCostMicros: 100}
}

func TestTodo_AGENT_028(t *testing.T) {
	f := newFixture(t)
	got, err := f.service.Admit(context.Background(), "tenant-a", "channel-a", "install-a", "outbox-a")
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	wantSourceKey := canonicalKeyForTest(t, "tenant-a", "agent-a", "install-a", "outbox-a")
	if got.Mode != "SPONSORED" || got.TierCeiling != 2 || got.SponsorID != "sponsor-a" || got.AgentVersion != "v3" || got.CauseID != "outbox-a" || got.SourceKey != wantSourceKey || got.SourceKind != agentrun.SourceChat {
		t.Fatalf("autonomous admission did not pin its sponsor, mode, cause, and ceiling: %+v", got)
	}
	if got.ChannelBudget != f.sub.ChannelBudget || got.FairShare != f.sub.FairShare || got.SubscriptionRevision != f.sub.Revision {
		t.Fatalf("reservation lost its bounded policy snapshot: %+v", got)
	}
	if got.CauseDepth != 0 || got.OccurredAt.IsZero() || len(f.ledger.reservations) != 1 {
		t.Fatalf("unexpected reservation state: result=%+v ledger=%+v", got, f.ledger.reservations)
	}

	for _, test := range []struct {
		name string
		edit func(*Subscription)
		want error
	}{
		{name: "opt in is required", edit: func(s *Subscription) { s.Enabled = false }, want: ErrDisabled},
		{name: "pause stops admission", edit: func(s *Subscription) { s.Paused = true }, want: ErrPaused},
		{name: "missing sponsor refuses", edit: func(s *Subscription) { s.SponsorID = "" }, want: ErrDenied},
		{name: "unbounded depth refuses", edit: func(s *Subscription) { s.MaxCauseDepth = hardMaxCauseDepth + 1 }, want: ErrDenied},
		{name: "unsupported subscription filter refuses", edit: func(s *Subscription) { s.EventClasses = []EventClass{"MEMBER_JOINED"} }, want: ErrDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			test.edit(&f.store.sub)
			if _, err := f.service.Admit(context.Background(), "tenant-a", "channel-a", "install-a", "outbox-a"); !errors.Is(err, test.want) {
				t.Fatalf("Admit error = %v, want %v", err, test.want)
			}
			if len(f.ledger.reservations) != 0 {
				t.Fatalf("denied subscription reached ledger: %+v", f.ledger.reservations)
			}
		})
	}
}

func TestTodo_AGENT_028_Race(t *testing.T) {
	f := newFixture(t)
	const contenders = 40
	start := make(chan struct{})
	results := make(chan error, contenders)
	var workers sync.WaitGroup
	for i := 0; i < contenders; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := f.service.Admit(context.Background(), "tenant-a", "channel-a", "install-a", "outbox-a")
			results <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	accepted := 0
	replayed := 0
	for err := range results {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrReplay):
			replayed++
		default:
			t.Fatalf("concurrent admission returned unexpected error: %v", err)
		}
	}
	if accepted != 1 || replayed != contenders-1 || len(f.ledger.reservations) != 1 {
		t.Fatalf("duplicate outbox admission was not atomic: accepted=%d replayed=%d stored=%d", accepted, replayed, len(f.ledger.reservations))
	}
}

func TestTodo_AGENT_028_Fault(t *testing.T) {
	for _, test := range []struct {
		name string
		set  func(*fixture)
	}{
		{name: "subscription resolver", set: func(f *fixture) { f.store.resolveErr = errors.New("store offline") }},
		{name: "outbox resolver", set: func(f *fixture) { f.store.eventErr = errors.New("outbox offline") }},
		{name: "current access", set: func(f *fixture) { f.access.err = errors.New("membership unavailable") }},
		{name: "atomic ledger", set: func(f *fixture) { f.ledger.err = errors.New("reservation unavailable") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			test.set(f)
			if _, err := f.service.Admit(context.Background(), "tenant-a", "channel-a", "install-a", "outbox-a"); err == nil {
				t.Fatal("injected owner or ledger fault admitted work")
			}
			if len(f.ledger.reservations) != 0 {
				t.Fatalf("fault path recorded work: %+v", f.ledger.reservations)
			}
		})
	}

	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.service.Admit(ctx, "tenant-a", "channel-a", "install-a", "outbox-a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled admission = %v, want context.Canceled", err)
	}
	f.store.mu.Lock()
	resolveCalls := f.store.resolveCalls
	f.store.mu.Unlock()
	if len(f.ledger.reservations) != 0 || resolveCalls != 0 {
		t.Fatalf("canceled admission crossed owner boundary: calls=%d ledger=%d", resolveCalls, len(f.ledger.reservations))
	}

	f = newFixture(t)
	if err := f.service.SetPaused(context.Background(), Actor{TenantID: "tenant-a", PrincipalID: "manager-a"}, "tenant-a", "channel-a", "install-a", 7, true); err != nil {
		t.Fatalf("manager pause: %v", err)
	}
	if !f.store.paused || f.store.pauseCalls != 1 {
		t.Fatalf("pause not stored: paused=%v calls=%d", f.store.paused, f.store.pauseCalls)
	}
}

func TestTodo_AGENT_028_Security(t *testing.T) {
	for _, test := range []struct {
		name      string
		editSub   func(*Subscription)
		editEvent func(*CommittedEvent)
		args      [4]string
		want      error
	}{
		{name: "cross tenant event", editEvent: func(e *CommittedEvent) { e.TenantID = "tenant-b" }, want: ErrDenied},
		{name: "cross channel event", editEvent: func(e *CommittedEvent) { e.ChannelID = "channel-b" }, want: ErrDenied},
		{name: "unsubscribed event class", editEvent: func(e *CommittedEvent) { e.Class = "MEMBER_JOINED" }, want: ErrDenied},
		{name: "system event inert", editEvent: func(e *CommittedEvent) { e.Origin = "SYSTEM" }, want: ErrDenied},
		{name: "agent event requires chain", editEvent: func(e *CommittedEvent) { e.Origin = OriginAgent; e.AuthorInstallationID = "install-peer" }, want: ErrDenied},
		{name: "agent author is last cause", editEvent: func(e *CommittedEvent) {
			e.Origin = OriginAgent
			e.AuthorInstallationID = "install-peer"
			e.CauseChain = []string{"different"}
		}, want: ErrDenied},
		{name: "agent-to-agent cycle", editEvent: func(e *CommittedEvent) {
			e.Origin = OriginAgent
			e.AuthorInstallationID = "install-peer"
			e.CauseChain = []string{"install-peer", "install-a"}
		}, want: ErrLoop},
		{name: "cause depth cap", editSub: func(s *Subscription) { s.MaxCauseDepth = 2 }, editEvent: func(e *CommittedEvent) {
			e.Origin = OriginAgent
			e.AuthorInstallationID = "install-peer"
			e.CauseChain = []string{"install-peer", "install-older"}
		}, want: ErrLoop},
		{name: "duplicate cause entry", editEvent: func(e *CommittedEvent) {
			e.Origin = OriginAgent
			e.AuthorInstallationID = "install-peer"
			e.CauseChain = []string{"install-peer", "install-peer"}
		}, want: ErrLoop},
		{name: "cost estimate over share", editEvent: func(e *CommittedEvent) { e.EstimatedCostMicros = 20_001 }, want: ErrDenied},
		{name: "caller cannot choose another tenant", args: [4]string{"tenant-b", "channel-a", "install-a", "outbox-a"}, want: ErrUnavailable},
		{name: "caller cannot choose another outbox reference", args: [4]string{"tenant-a", "channel-a", "install-a", "outbox-forged"}, want: ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			if test.editSub != nil {
				test.editSub(&f.store.sub)
			}
			if test.editEvent != nil {
				test.editEvent(&f.store.event)
			}
			args := test.args
			if args == [4]string{} {
				args = [4]string{"tenant-a", "channel-a", "install-a", "outbox-a"}
			}
			if _, err := f.service.Admit(context.Background(), args[0], args[1], args[2], args[3]); !errors.Is(err, test.want) {
				t.Fatalf("Admit error = %v, want %v", err, test.want)
			}
			if len(f.ledger.reservations) != 0 {
				t.Fatalf("security denial reached ledger: %+v", f.ledger.reservations)
			}
		})
	}

	f := newFixture(t)
	f.access.err = errors.New("external recipient is outside audience")
	if _, err := f.service.Admit(context.Background(), "tenant-a", "channel-a", "install-a", "outbox-a"); !errors.Is(err, ErrDenied) {
		t.Fatalf("audience failure = %v, want ErrDenied", err)
	}
}

func TestTodo_AGENT_028_Golden(t *testing.T) {
	got := canonicalKeyForTest(t, "tenant-a", "agent-a", "install-a", "outbox-a")
	const want = "a179232ba580c1c7a96bb35cb1477dd56e138ab80117a98f5603152c4b00b852"
	if got != want {
		t.Fatalf("source key = %q, want %q", got, want)
	}
	if canonicalKeyForTest(t, "tenant-a", "agent-a", "install-a", "outbox-a") != got || canonicalKeyForTest(t, "tenant-a", "agent-a", "install-b", "outbox-a") == got || canonicalKeyForTest(t, "tenant-a", "agent-b", "install-a", "outbox-a") == got {
		t.Fatal("source key is not deterministic and installation-scoped")
	}
}

func canonicalKeyForTest(t *testing.T, tenantID, agentID, installationID, outboxID string) string {
	t.Helper()
	source, err := (agentrun.CanonicalSourceConverter{}).ConvertSource(context.Background(), agentrun.Request{
		Source: agentrun.SourceIdentity{TenantID: tenantID, Kind: agentrun.SourceChat, Ref: outboxID},
		Agent:  agentrun.VersionRef{AgentID: agentID}, InstallationID: installationID,
	})
	if err != nil {
		t.Fatalf("canonical source: %v", err)
	}
	return source.Key
}

func TestSetPaused_FailsClosed(t *testing.T) {
	for _, test := range []struct {
		name      string
		actor     Actor
		revision  uint64
		authority error
		want      error
	}{
		{name: "wrong tenant", actor: Actor{TenantID: "tenant-b", PrincipalID: "manager-a"}, revision: 7, want: ErrDenied},
		{name: "stale revision", actor: Actor{TenantID: "tenant-a", PrincipalID: "manager-a"}, revision: 6, want: ErrConflict},
		{name: "not manager", actor: Actor{TenantID: "tenant-a", PrincipalID: "member-a"}, revision: 7, authority: errors.New("not manager"), want: ErrDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			f.service.pauseAuth = memoryPauseAuth{err: test.authority}
			err := f.service.SetPaused(context.Background(), test.actor, "tenant-a", "channel-a", "install-a", test.revision, true)
			if !errors.Is(err, test.want) {
				t.Fatalf("SetPaused error = %v, want %v", err, test.want)
			}
			if f.store.pauseCalls != 0 {
				t.Fatalf("denied or stale pause reached writer %d times", f.store.pauseCalls)
			}
		})
	}

	f := newFixture(t)
	f.service.pauseAuth = memoryPauseAuth{err: errors.New("revoked manager grant")}
	err := f.service.SetPaused(context.Background(), Actor{TenantID: "tenant-a", PrincipalID: "manager-a"}, "tenant-a", "channel-a", "install-a", 7, true)
	if !errors.Is(err, ErrDenied) || f.store.pauseCalls != 0 {
		t.Fatalf("revoked manager authority error=%v writes=%d", err, f.store.pauseCalls)
	}

	f = newFixture(t)
	f.store.pauseErr = fmt.Errorf("%w: concurrent revision", ErrConflict)
	err = f.service.SetPaused(context.Background(), Actor{TenantID: "tenant-a", PrincipalID: "manager-a"}, "tenant-a", "channel-a", "install-a", 7, true)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("writer revision conflict was not preserved: %v", err)
	}

	f = newFixture(t)
	f.store.revokeOnWrite = true
	err = f.service.SetPaused(context.Background(), Actor{TenantID: "tenant-a", PrincipalID: "manager-a"}, "tenant-a", "channel-a", "install-a", 7, true)
	if !errors.Is(err, ErrDenied) || f.store.paused || f.store.pauseCalls != 1 {
		t.Fatalf("atomic authorization recheck error=%v paused=%v calls=%d", err, f.store.paused, f.store.pauseCalls)
	}
}
