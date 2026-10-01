package subscription

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

type subscriptionMemory struct {
	mu         sync.Mutex
	definition Definition
	events     map[string]Delivery
	receipts   map[string]agentrun.Record
	failAck    bool
}

func (m *subscriptionMemory) LoadSubscription(_ context.Context, tenant, id string) (Definition, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.definition
	return d, d.Declaration.TenantID == tenant && d.Declaration.ID == id, nil
}
func (m *subscriptionMemory) SaveSubscription(_ context.Context, d Definition, expected uint64, _ Audit) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.definition.Revision != expected {
		return ErrRevision
	}
	m.definition = d
	return nil
}
func (m *subscriptionMemory) ReserveEvent(_ context.Context, e Delivery, debounce time.Duration) (Delivery, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if prior, ok := m.events[e.Key]; ok {
		return prior, false, nil
	}
	if m.definition.Revision != e.Revision {
		return Delivery{}, false, ErrRevision
	}
	for _, event := range m.events {
		if e.At.Sub(event.At) < debounce {
			return Delivery{}, false, ErrDebounced
		}
	}
	m.events[e.Key] = e
	return e, true, nil
}
func (m *subscriptionMemory) GetEvent(_ context.Context, tenant, key string) (Delivery, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.events[key]
	return e, ok && e.TenantID == tenant, nil
}
func (m *subscriptionMemory) PendingEvents(_ context.Context, tenant string, _ int) ([]Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var events []Delivery
	for key, e := range m.events {
		if _, ok := m.receipts[key]; !ok && e.TenantID == tenant {
			events = append(events, e)
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Revision < events[j].Revision })
	return events, nil
}
func (m *subscriptionMemory) AcknowledgeEvent(_ context.Context, _ string, key string, r agentrun.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failAck {
		m.failAck = false
		return errors.New("event acknowledgement unavailable")
	}
	m.receipts[key] = r
	return nil
}

type sourceProjection struct {
	event EventProjection
	err   error
}

func (p *sourceProjection) Project(_ context.Context, _ string, id string, _ agentrun.AudienceScope) (EventProjection, error) {
	event := p.event
	event.ID = id
	return event, p.err
}
func (p *sourceProjection) ValidateClasses(context.Context, Definition) error { return p.err }

type currentPolicy struct {
	revoked        bool
	changedVersion bool
}

func (p *currentPolicy) Authorize(_ context.Context, a Actor, _ string, d Definition) error {
	if p.revoked || a.UserID == "outsider" || a.TenantID != d.Declaration.TenantID {
		return ErrRevoked
	}
	return nil
}
func (p *currentPolicy) Resolve(_ context.Context, d Definition) (Subscription, error) {
	s := d.Declaration
	if p.revoked {
		s.GrantActive = false
	}
	if p.changedVersion {
		s.Agent.Version = "99"
	}
	return s, nil
}
func ownerTestFixture(t *testing.T) (*Owner, *subscriptionMemory, *sourceProjection, *currentPolicy, Definition, time.Time) {
	t.Helper()
	input := validInput()
	d := Definition{Declaration: input.Subscription, OwnerID: "owner", State: "DRAFT", RunTimeout: time.Minute}
	d.Declaration.Revision = 0
	d.Declaration.CurrentRevision = 0
	d.Declaration.State = "DRAFT"
	store := &subscriptionMemory{events: map[string]Delivery{}, receipts: map[string]agentrun.Record{}}
	source := &sourceProjection{event: input.Event}
	policy := &currentPolicy{}
	inbox, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Store: agentrun.NewMemoryAdmissionStore(), Authority: &eventAdmissionAuthority{}, Now: func() time.Time { return input.At }})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(store, source, policy, inbox)
	if err != nil {
		t.Fatal(err)
	}
	return owner, store, source, policy, d, input.At
}
func TestTodo_AGENT_032_OwnerRecovery(t *testing.T) {
	ctx := context.Background()
	owner, store, _, policy, d, at := ownerTestFixture(t)
	draft, err := owner.Save(ctx, Actor{"tenant-a", "owner"}, d, 0, "DRAFT", at)
	if err != nil {
		t.Fatal(err)
	}
	published, err := owner.Save(ctx, Actor{"tenant-a", "reviewer"}, draft, 1, "PUBLISH", at)
	if err != nil || published.State != "ACTIVE" {
		t.Fatalf("publish=%+v err=%v", published, err)
	}
	first, created, err := owner.Ingest(ctx, "tenant-a", published.Declaration.ID, "event-42", at)
	if err != nil || !created || len(first.Candidate.Fields) != 1 || first.Candidate.Fields[0].Name != "status" {
		t.Fatalf("owner ingest=%+v created=%t err=%v", first, created, err)
	}
	duplicate, created, err := owner.Ingest(ctx, "tenant-a", published.Declaration.ID, "event-42", at.Add(time.Second))
	if err != nil || created || duplicate.Candidate.Request.Deadline != first.Candidate.Request.Deadline {
		t.Fatalf("event replay=%+v created=%t err=%v", duplicate, created, err)
	}
	store.failAck = true
	if _, err := owner.Replay(ctx, "tenant-a", 10); err == nil {
		t.Fatal("expected acknowledgement failure")
	}
	if done, err := owner.Replay(ctx, "tenant-a", 10); err != nil || done != 1 || len(store.receipts) != 1 {
		t.Fatalf("redrive done=%d err=%v receipts=%d", done, err, len(store.receipts))
	}
	if key, err := owner.ResolveSourceKey(ctx, first.Candidate.Request); err != nil || key != first.Key {
		t.Fatalf("restore key=%q err=%v", key, err)
	}
	policy.revoked = true
	if err := owner.CheckRequest(ctx, first.Candidate.Request); !errors.Is(err, ErrRevoked) {
		t.Fatalf("grant revoked after enqueue=%v", err)
	}
	policy.revoked = false
	policy.changedVersion = true
	if _, _, err := owner.Ingest(ctx, "tenant-a", published.Declaration.ID, "next", at.Add(time.Hour)); !errors.Is(err, ErrRevoked) {
		t.Fatalf("silent latest version=%v", err)
	}
	policy.changedVersion = false
	paused, err := owner.Save(ctx, Actor{"tenant-a", "owner"}, published, 2, "PAUSE", at)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.CheckRequest(ctx, first.Candidate.Request); !errors.Is(err, ErrInactive) {
		t.Fatalf("pause worker fence=%v", err)
	}
	resumed, err := owner.Save(ctx, Actor{"tenant-a", "owner"}, paused, 3, "RESUME", at)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := owner.Ingest(ctx, "tenant-a", resumed.Declaration.ID, "event-44", at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	retired, err := owner.Save(ctx, Actor{"tenant-a", "owner"}, resumed, 4, "RETIRE", at)
	if err != nil || retired.State != "RETIRED" {
		t.Fatalf("retire=%+v err=%v", retired, err)
	}
}
func TestTodo_AGENT_032_OwnerSecurity(t *testing.T) {
	ctx := context.Background()
	owner, store, source, _, d, at := ownerTestFixture(t)
	if _, err := owner.Save(ctx, Actor{"tenant-a", "outsider"}, d, 0, "DRAFT", at); !errors.Is(err, ErrRevoked) || store.definition.Revision != 0 {
		t.Fatalf("unauthorized save=%v state=%+v", err, store.definition)
	}
	draft, err := owner.Save(ctx, Actor{"tenant-a", "owner"}, d, 0, "DRAFT", at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Save(ctx, Actor{"tenant-a", "owner"}, draft, 1, "PUBLISH", at); !errors.Is(err, ErrRevoked) {
		t.Fatalf("self publication=%v", err)
	}
	pub, err := owner.Save(ctx, Actor{"tenant-a", "reviewer"}, draft, 1, "PUBLISH", at)
	if err != nil {
		t.Fatal(err)
	}
	source.event.Fields = append(source.event.Fields, ProjectedField{Name: "unpublished", Value: "secret", Classification: "RESTRICTED", AudienceIDs: []string{"audience-a"}})
	if _, _, err := owner.Ingest(ctx, "tenant-a", pub.Declaration.ID, "forbidden", at); !errors.Is(err, ErrForbiddenField) || len(store.events) != 0 {
		t.Fatalf("forbidden source field=%v events=%d", err, len(store.events))
	}
	if _, err := owner.Save(ctx, Actor{"tenant-a", "owner"}, pub, 1, "PAUSE", at); !errors.Is(err, ErrRevision) {
		t.Fatalf("stale mutation=%v", err)
	}
	if _, err := NewOwner(nil, source, &currentPolicy{}, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing source owner ports=%v", err)
	}
}

func TestTodo_AGENT_032_ReplayContinuesAfterRevisionChange(t *testing.T) {
	ctx := context.Background()
	owner, store, _, _, d, at := ownerTestFixture(t)
	draft, err := owner.Save(ctx, Actor{"tenant-a", "owner"}, d, 0, "DRAFT", at)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := owner.Save(ctx, Actor{"tenant-a", "reviewer"}, draft, 1, "PUBLISH", at)
	if err != nil {
		t.Fatal(err)
	}
	old, _, err := owner.Ingest(ctx, "tenant-a", pub.Declaration.ID, "old", at)
	if err != nil {
		t.Fatal(err)
	}
	paused, err := owner.Save(ctx, Actor{"tenant-a", "owner"}, pub, 2, "PAUSE", at)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := owner.Save(ctx, Actor{"tenant-a", "owner"}, paused, 3, "RESUME", at)
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := owner.Ingest(ctx, "tenant-a", resumed.Declaration.ID, "current", at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if done, err := owner.Replay(ctx, "tenant-a", 10); done != 1 || !errors.Is(err, ErrInactive) {
		t.Fatalf("done=%d err=%v", done, err)
	}
	if len(store.receipts) != 1 || store.receipts[current.Key].Decision != agentrun.DecisionAccepted {
		t.Fatalf("receipts=%+v", store.receipts)
	}
	remaining, _ := store.PendingEvents(ctx, "tenant-a", 10)
	if len(remaining) != 1 || remaining[0].Key != old.Key || remaining[0].Candidate.Request.Deadline != old.Candidate.Request.Deadline {
		t.Fatalf("unresolved evidence=%+v", remaining)
	}
}
