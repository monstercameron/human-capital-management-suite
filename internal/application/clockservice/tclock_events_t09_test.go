package clockservice

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
)

type t09WebhookProvider struct {
	attempts []ClockWebhookAttempt
}

func (p *t09WebhookProvider) DeliverClockEvent(_ context.Context, attempt ClockWebhookAttempt) (subscription.ProviderReceipt, error) {
	p.attempts = append(p.attempts, attempt)
	return subscription.ProviderReceipt{
		ReceiptID:      "receipt-" + attempt.Delivery.Request.SubscriptionID,
		Provider:       "t09-test-provider",
		IdempotencyKey: attempt.Delivery.Request.IdempotencyKey,
		Destination:    attempt.Delivery.Request.Destination,
		Accepted:       true,
		ReceivedAt:     attempt.Delivery.AttemptedAt,
	}, nil
}

type t09ScopedEventSource struct {
	rows        []ScopedOutboxEvent
	listReads   int
	scopedReads int
}

func (s *t09ScopedEventSource) ListEvents(_ context.Context, _ string, after int64, limit int) ([]OutboxEvent, error) {
	s.listReads++
	return t09RowsAfter(s.rows, "", "", after, limit), nil
}

func (s *t09ScopedEventSource) ListScopedEvents(_ context.Context, _ string, organization, worker string, after int64, limit int) ([]ScopedOutboxEvent, error) {
	s.scopedReads++
	out := make([]ScopedOutboxEvent, 0, limit)
	for _, row := range s.rows {
		if row.Sequence <= after || (organization != "" && row.Organization != organization) || (worker != "" && row.Worker != worker) {
			continue
		}
		out = append(out, row)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func t09RowsAfter(rows []ScopedOutboxEvent, organization, worker string, after int64, limit int) []OutboxEvent {
	out := make([]OutboxEvent, 0, limit)
	for _, row := range rows {
		if row.Sequence <= after || (organization != "" && row.Organization != organization) || (worker != "" && row.Worker != worker) {
			continue
		}
		out = append(out, row.OutboxEvent)
		if len(out) == limit {
			break
		}
	}
	return out
}

func t09Feed(t *testing.T, source EventOutbox, revision subscription.EventSubscription, grants map[string]subscription.ScopeGrant, now time.Time) *ClockEventFeed {
	t.Helper()
	feed, err := NewClockEventFeed(ClockEventFeedConfig{
		Source: source, Authorizer: clockEventAuthFake{}, Revisions: []subscription.EventSubscription{revision}, Grants: grants,
		Signer: clockEventSigner(t, now), CursorKey: []byte("clock-cursor-key-01234567890123456789"), Destination: "endpoint:clock",
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return feed
}

func t09AcceptedRow(now time.Time, payload string) OutboxEvent {
	return OutboxEvent{Sequence: 1, EventType: ClockPunchAccepted, SchemaVersion: 1, CreatedAt: now, Payload: []byte(payload)}
}

func t09ScopedSubscription(t *testing.T, organization string) (subscription.EventSubscription, map[string]subscription.ScopeGrant) {
	t.Helper()
	draft, err := subscription.NewDraft(subscription.RevisionRequest{
		SubscriptionID: "sub-clock-t09", Requester: "requester", Subscriber: subscription.Subscriber{PrincipalRef: "partner-user"},
		EventKinds:          []subscription.EventKind{subscription.EventClockPunchAccepted},
		DeclaredFields:      map[subscription.EventKind][]string{subscription.EventClockPunchAccepted: {"receipt_id", "worker_id"}},
		DeliveryEndpointRef: "endpoint:clock", DeliveryGuarantee: subscription.GuaranteeAtLeastOnce, TenantScope: "tenant-a", OrganizationScopeRef: organization,
	})
	if err != nil {
		t.Fatal(err)
	}
	active, err := draft.Activate("requester", "approver")
	if err != nil {
		t.Fatal(err)
	}
	return active, map[string]subscription.ScopeGrant{active.SubscriptionID: {
		PrincipalRef: "partner-user", TenantScope: "tenant-a", Purpose: "payroll", EventKinds: []subscription.EventKind{subscription.EventClockPunchAccepted},
		Resources: []string{"tenant-a", organization}, Fields: map[subscription.EventKind][]string{subscription.EventClockPunchAccepted: {"receipt_id", "worker_id"}},
	}}
}

func TestTodo_TCLOCK_012(t *testing.T) {
	now := time.Unix(200, 0).UTC()
	revision, grants := clockEventSubscription(t)
	source := &clockEventOutboxFake{rows: []OutboxEvent{t09AcceptedRow(now, `{"receipt_id":"r1","worker_id":"w1","device_id":"hidden","photo":"hidden"}`)}}
	feed := t09Feed(t, source, revision, grants, now)
	provider := &t09WebhookProvider{}
	publisher, err := NewClockWebhookPublisher(feed, subscription.NewDeliveryJournal(func() time.Time { return now }), provider)
	if err != nil {
		t.Fatal(err)
	}
	page, err := publisher.Publish(context.Background(), ClockWebhookRequest{Tenant: "tenant-a", Limit: 10})
	if err != nil || len(page.Deliveries) != 1 || len(provider.attempts) != 1 {
		t.Fatalf("Publish deliveries=%d provider_calls=%d err=%v", len(page.Deliveries), len(provider.attempts), err)
	}
	if page.Deliveries[0].Result.Operation.State != subscription.OperationAcked {
		t.Fatalf("delivery state=%q", page.Deliveries[0].Result.Operation.State)
	}
	if err := VerifyClockEvent(provider.attempts[0].Event, feed.signer, now); err != nil {
		t.Fatalf("signed event verification=%v", err)
	}
	replay, err := publisher.Publish(context.Background(), ClockWebhookRequest{Tenant: "tenant-a", Cursor: "", Limit: 10})
	if err != nil || len(replay.Deliveries) != 1 || !replay.Deliveries[0].Result.Duplicate || len(provider.attempts) != 1 {
		t.Fatalf("cursor replay deliveries=%+v provider_calls=%d err=%v", replay.Deliveries, len(provider.attempts), err)
	}
}

func TestTodo_TCLOCK_012_Integration(t *testing.T) {
	now := time.Unix(300, 0).UTC()
	revision, grants := t09ScopedSubscription(t, "org-a")
	source := &t09ScopedEventSource{rows: []ScopedOutboxEvent{
		{OutboxEvent: t09AcceptedRow(now, `{"receipt_id":"r-a","worker_id":"w-a"}`), Organization: "org-a", Worker: "w-a"},
		{OutboxEvent: OutboxEvent{Sequence: 2, EventType: ClockPunchAccepted, SchemaVersion: 1, CreatedAt: now, Payload: []byte(`{"receipt_id":"r-b","worker_id":"w-b"}`)}, Organization: "org-b", Worker: "w-b"},
	}}
	feed := t09Feed(t, source, revision, grants, now)
	provider := &t09WebhookProvider{}
	publisher, err := NewClockWebhookPublisher(feed, subscription.NewDeliveryJournal(func() time.Time { return now }), provider)
	if err != nil {
		t.Fatal(err)
	}
	page, err := publisher.Publish(context.Background(), ClockWebhookRequest{Tenant: "tenant-a", Organization: "org-a", Limit: 10})
	if err != nil || len(page.Deliveries) != 1 || source.scopedReads != 1 || source.listReads != 0 {
		t.Fatalf("integration page=%+v scoped_reads=%d list_reads=%d err=%v", page, source.scopedReads, source.listReads, err)
	}
	if got := provider.attempts[0].Event.Payload["worker_id"]; got != "w-a" {
		t.Fatalf("worker payload=%v", got)
	}
	if _, err := publisher.Publish(context.Background(), ClockWebhookRequest{Tenant: "tenant-a", Organization: "org-b", Limit: 10}); err != nil {
		t.Fatal(err)
	} else if len(provider.attempts) != 1 {
		t.Fatal("foreign organization reached the webhook provider")
	}
}

func TestTodo_TCLOCK_012_Security(t *testing.T) {
	now := time.Unix(400, 0).UTC()
	revision, grants := t09ScopedSubscription(t, "org-a")
	source := &t09ScopedEventSource{rows: []ScopedOutboxEvent{{
		OutboxEvent:  t09AcceptedRow(now, `{"receipt_id":"r1","worker_id":"w1","device_id":"d1","photo":"photo","biometric":"bio","raw_location":"location"}`),
		Organization: "org-a", Worker: "w1",
	}}}
	feed := t09Feed(t, source, revision, grants, now)
	provider := &t09WebhookProvider{}
	publisher, err := NewClockWebhookPublisher(feed, subscription.NewDeliveryJournal(func() time.Time { return now }), provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(context.Background(), ClockWebhookRequest{Tenant: "tenant-a", Organization: "org-a", Worker: "w1", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if len(provider.attempts) != 1 {
		t.Fatalf("authorized attempts=%d", len(provider.attempts))
	}
	for _, forbidden := range []string{"device_id", "photo", "biometric", "raw_location"} {
		if _, disclosed := provider.attempts[0].Event.Payload[forbidden]; disclosed {
			t.Fatalf("forbidden field %q disclosed", forbidden)
		}
	}
	if _, err := publisher.Publish(context.Background(), ClockWebhookRequest{Tenant: "tenant-a", Organization: "org-b", Worker: "w1", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if len(provider.attempts) != 1 {
		t.Fatal("out-of-scope worker organization reached provider")
	}
}

func TestTodo_TCLOCK_012_Golden(t *testing.T) {
	now := time.Unix(500, 0).UTC()
	revision, grants := clockEventSubscription(t)
	source := &clockEventOutboxFake{rows: []OutboxEvent{t09AcceptedRow(now, `{"receipt_id":"r-golden","worker_id":"w-golden","device_id":"hidden"}`)}}
	feed := t09Feed(t, source, revision, grants, now)
	provider := &t09WebhookProvider{}
	publisher, err := NewClockWebhookPublisher(feed, subscription.NewDeliveryJournal(func() time.Time { return now }), provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(context.Background(), ClockWebhookRequest{Tenant: "tenant-a", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	golden, err := json.Marshal(provider.attempts[0].Event.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(golden), `{"receipt_id":"r-golden","worker_id":"w-golden"}`; got != want {
		t.Fatalf("redacted payload bytes=%s want=%s", got, want)
	}
}
