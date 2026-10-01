package agenttriggerstore

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/subscription"
)

type domainProjection struct{ event subscription.EventProjection }

func (p domainProjection) Project(_ context.Context, tenant, id string, audience agentrun.AudienceScope) (subscription.EventProjection, error) {
	e := p.event
	e.TenantID = tenant
	e.ID = id
	if audience != e.Audience {
		return subscription.EventProjection{}, subscription.ErrAudience
	}
	return e, nil
}
func (domainProjection) ValidateClasses(_ context.Context, d subscription.Definition) error {
	if len(d.Declaration.EventKinds) != 1 || d.Declaration.EventKinds[0] != "WORK_REVIEWED" {
		return subscription.ErrInvalid
	}
	return nil
}

type subscriptionPolicy struct{}

func (subscriptionPolicy) Authorize(_ context.Context, a subscription.Actor, _ string, d subscription.Definition) error {
	if a.TenantID != d.Declaration.TenantID {
		return subscription.ErrAudience
	}
	return nil
}
func (subscriptionPolicy) Resolve(_ context.Context, d subscription.Definition) (subscription.Subscription, error) {
	return d.Declaration, nil
}

func testSubscriptionStore(t *testing.T, store *Store) {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	declaration := subscription.Subscription{ID: "subscription", TenantID: "tenant-a", Revision: 1, CurrentRevision: 1, State: "ACTIVE", Agent: agentrun.VersionRef{AgentID: "agent", Version: "1", Digest: digest}, InstallationID: "installation", InstallationRevision: 1, CurrentInstallRevision: 1, InstallationActive: true, GrantActive: true, GrantRevision: 1, CurrentGrantRevision: 1, Purpose: "review", SponsorID: "sponsor", AgentPrincipalID: "agent-principal", Audience: agentrun.AudienceScope{ID: "audience", SnapshotID: "snapshot", Digest: digest}, EventKinds: []string{"WORK_REVIEWED"}, FieldsByKind: map[string][]string{"WORK_REVIEWED": {"summary"}}, MaximumClassification: "PUBLIC", Debounce: time.Hour, MaxCauseDepth: 3, BudgetCeiling: agentrun.Budget{MaxCostMicros: 100, MaxInputTokens: 100, MaxOutputTokens: 100}}
	definition := subscription.Definition{Declaration: declaration, OwnerID: "owner", Revision: 1, State: "ACTIVE", RunTimeout: time.Hour}
	if err := store.SaveSubscription(ctx, definition, 0, subscription.Audit{TenantID: "tenant-a", SubscriptionID: "subscription", ActorID: "reviewer", Revision: 1, Action: "PUBLISH", At: at}); err != nil {
		t.Fatal(err)
	}
	loaded, found, err := store.LoadSubscription(ctx, "tenant-a", "subscription")
	if err != nil || !found || loaded.Declaration.Agent != declaration.Agent {
		t.Fatalf("subscription reload=%+v found=%t err=%v", loaded, found, err)
	}
	projection := domainProjection{subscription.EventProjection{LegalEntity: "entity", Kind: "WORK_REVIEWED", SchemaVersion: "1", OccurredAt: at.Add(-time.Minute), Audience: declaration.Audience, Schema: []subscription.FieldRule{{Name: "summary", Classification: "PUBLIC"}}, Fields: []subscription.ProjectedField{{Name: "summary", Value: "review complete", Classification: "PUBLIC", AudienceIDs: []string{"audience"}}}}}
	inbox, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Store: agentrun.NewMemoryAdmissionStore(), Authority: currentAuthority{}, Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := subscription.NewOwner(store, projection, subscriptionPolicy{}, inbox)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	var created atomic.Int32
	var debounced atomic.Int32
	failures := make(chan error, 2)
	for _, id := range []string{"event-a", "event-b"} {
		wait.Add(1)
		go func(id string) {
			defer wait.Done()
			_, inserted, err := owner.Ingest(ctx, "tenant-a", "subscription", id, at)
			if errors.Is(err, subscription.ErrDebounced) {
				debounced.Add(1)
			} else if err != nil {
				failures <- err
			} else if inserted {
				created.Add(1)
			}
		}(id)
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if created.Load() != 1 || debounced.Load() != 1 {
		t.Fatalf("atomic debounce created=%d debounced=%d", created.Load(), debounced.Load())
	}
	pending, err := store.PendingEvents(ctx, "tenant-a", 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending events=%+v err=%v", pending, err)
	}
	delivery := pending[0]
	if err := owner.CheckRequest(ctx, delivery.Candidate.Request); err != nil {
		t.Fatal(err)
	}
	eventID := delivery.Candidate.Request.Context.ID[len("domain-event:"):]
	replayed, inserted, err := owner.Ingest(ctx, "tenant-a", "subscription", eventID, at.Add(time.Minute))
	if err != nil || inserted || replayed.Candidate.Request.Deadline != delivery.Candidate.Request.Deadline {
		t.Fatalf("duplicate event=%+v inserted=%t err=%v", replayed, inserted, err)
	}
	if done, err := owner.Replay(ctx, "tenant-a", 10); err != nil || done != 1 {
		t.Fatalf("event replay=%d err=%v", done, err)
	}
	if pending, err := store.PendingEvents(ctx, "tenant-a", 10); err != nil || len(pending) != 0 {
		t.Fatalf("receipt pending=%d err=%v", len(pending), err)
	}
	record, _, err := inbox.Admit(ctx, delivery.Candidate.Request)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AcknowledgeEvent(ctx, "tenant-a", delivery.Key, record); err != nil {
		t.Fatal(err)
	}
	record.ID = "forged"
	if err := store.AcknowledgeEvent(ctx, "tenant-a", delivery.Key, record); !errors.Is(err, subscription.ErrInvalid) {
		t.Fatalf("forged event receipt=%v", err)
	}
	if key, err := owner.ResolveSourceKey(ctx, delivery.Candidate.Request); err != nil || key != delivery.Key {
		t.Fatalf("restored key=%q err=%v", key, err)
	}
	_, err = owner.Save(ctx, subscription.Actor{TenantID: "tenant-a", UserID: "owner"}, definition, 1, "PAUSE", at)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.CheckRequest(ctx, delivery.Candidate.Request); !errors.Is(err, subscription.ErrInactive) {
		t.Fatalf("pause worker fence=%v", err)
	}
	if _, _, err := owner.Ingest(ctx, "tenant-a", "subscription", "event-c", at.Add(2*time.Hour)); !errors.Is(err, subscription.ErrInactive) {
		t.Fatalf("paused event admission=%v", err)
	}
	if _, _, err := store.LoadSubscription(ctx, "tenant-b", "subscription"); err == nil {
		t.Fatal("cross tenant read accepted")
	}
}
