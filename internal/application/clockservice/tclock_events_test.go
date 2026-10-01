package clockservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
)

type clockEventOutboxFake struct {
	rows  []OutboxEvent
	reads int
}

func (f *clockEventOutboxFake) ListEvents(_ context.Context, _ string, after int64, limit int) ([]OutboxEvent, error) {
	f.reads++
	out := []OutboxEvent{}
	for _, row := range f.rows {
		if row.Sequence > after {
			out = append(out, row)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

type clockEventAuthFake struct{}

func (clockEventAuthFake) AuthorizeClockEvents(context.Context, string, string, string) error {
	return nil
}

func clockEventSigner(t *testing.T, now time.Time) *subscription.CredentialRing {
	t.Helper()
	provider, err := subscription.NewHMACProvider("key-1", []byte("clock-event-test-signing-key-0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	ring, err := subscription.NewCredentialRing("endpoint:clock", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.Add(subscription.SigningCredential{Destination: "endpoint:clock", Profile: "clock-v1", Version: "v1", KeyRef: "key-1", NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), Provider: provider}); err != nil {
		t.Fatal(err)
	}
	if err := ring.Activate("v1", now); err != nil {
		t.Fatal(err)
	}
	return ring
}

func clockEventSubscription(t *testing.T) (subscription.EventSubscription, map[string]subscription.ScopeGrant) {
	t.Helper()
	draft, err := subscription.NewDraft(subscription.RevisionRequest{SubscriptionID: "sub-clock", Requester: "requester", Subscriber: subscription.Subscriber{PrincipalRef: "partner-user"}, EventKinds: []subscription.EventKind{subscription.EventClockPunchAccepted}, DeclaredFields: map[subscription.EventKind][]string{subscription.EventClockPunchAccepted: {"receipt_id", "worker_id"}}, DeliveryEndpointRef: "endpoint:clock", DeliveryGuarantee: subscription.GuaranteeAtLeastOnce, TenantScope: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	active, err := draft.Activate("requester", "approver")
	if err != nil {
		t.Fatal(err)
	}
	return active, map[string]subscription.ScopeGrant{"sub-clock": {PrincipalRef: "partner-user", TenantScope: "tenant-a", Purpose: "payroll", EventKinds: []subscription.EventKind{subscription.EventClockPunchAccepted}, Resources: []string{"tenant-a"}, Fields: map[subscription.EventKind][]string{subscription.EventClockPunchAccepted: {"receipt_id", "worker_id"}}}}
}

func TestTodo_TCLOCK_012_SignedProjectionTamper(t *testing.T) {
	now := time.Unix(200, 0).UTC()
	sub, grants := clockEventSubscription(t)
	source := &clockEventOutboxFake{rows: []OutboxEvent{{Sequence: 1, EventType: ClockPunchAccepted, SchemaVersion: 1, CreatedAt: now, Payload: []byte(`{"receipt_id":"r1","worker_id":"w1","device_id":"device-hidden-by-mask","photo":"secret"}`)}}}
	feed, err := NewClockEventFeed(ClockEventFeedConfig{Source: source, Authorizer: clockEventAuthFake{}, Revisions: []subscription.EventSubscription{sub}, Grants: grants, Signer: clockEventSigner(t, now), CursorKey: []byte("clock-cursor-key-01234567890123456789"), Destination: "endpoint:clock", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	page, err := feed.Pull(context.Background(), ClockEventRequest{Tenant: "tenant-a", Limit: 10})
	if err != nil || len(page.Events) != 1 {
		t.Fatalf("Pull events=%d err=%v", len(page.Events), err)
	}
	event := page.Events[0]
	if _, disclosed := event.Payload["device_id"]; disclosed {
		t.Fatal("undeclared device field escaped the subscription mask")
	}
	if err := VerifyClockEvent(event, feed.signer, now); err != nil {
		t.Fatalf("VerifyClockEvent=%v", err)
	}
	for _, mutation := range []func(*ClockEvent){
		func(e *ClockEvent) { e.Tenant = "foreign" },
		func(e *ClockEvent) { e.Sequence++ },
		func(e *ClockEvent) { e.EventType = ClockPunchRejected },
		func(e *ClockEvent) { e.SchemaVersion++ },
	} {
		changed := event
		mutation(&changed)
		if !errors.Is(VerifyClockEvent(changed, feed.signer, now), ErrClockEventInvalid) {
			t.Fatal("unsigned outer event metadata was accepted")
		}
	}
	event.Payload["worker_id"] = "other"
	if !errors.Is(VerifyClockEvent(event, feed.signer, now), ErrClockEventInvalid) {
		t.Fatal("tampered projection verified")
	}
}

func TestTodo_TCLOCK_012_CursorBindsTenantAndScope(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	f := &ClockEventFeed{cursorKey: []byte("01234567890123456789012345678901"), now: func() time.Time { return now }, cursorTTL: time.Minute}
	token, err := f.IssueClockEventCursor(ClockEventCursor{Tenant: "tenant-a", Organization: "org-a", Worker: "worker-a", Sequence: 17})
	if err != nil || token == "" {
		t.Fatalf("IssueClockEventCursor() token=%q err=%v", token, err)
	}
	decoded, err := f.decodeCursor(token, "tenant-a", "org-a", "worker-a")
	if err != nil || decoded.Sequence != 17 {
		t.Fatalf("decodeCursor()=%+v err=%v", decoded, err)
	}
	if _, err := f.decodeCursor(token, "tenant-b", "org-a", "worker-a"); err != ErrClockCursorInvalid {
		t.Fatalf("foreign tenant err=%v, want ErrClockCursorInvalid", err)
	}
	if _, err := f.decodeCursor(token+"x", "tenant-a", "org-a", "worker-a"); err != ErrClockCursorInvalid {
		t.Fatalf("forged cursor err=%v, want ErrClockCursorInvalid", err)
	}
}

func TestTodo_TCLOCK_012_CursorRejectsExpiredAndInvalid(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	f := &ClockEventFeed{cursorKey: []byte("01234567890123456789012345678901"), now: func() time.Time { return now }, cursorTTL: time.Minute}
	if _, err := f.IssueClockEventCursor(ClockEventCursor{Tenant: "", Sequence: 0}); err != ErrClockCursorInvalid {
		t.Fatalf("empty tenant err=%v, want ErrClockCursorInvalid", err)
	}
	if _, err := f.IssueClockEventCursor(ClockEventCursor{Tenant: "tenant-a", Sequence: -1}); err != ErrClockCursorInvalid {
		t.Fatalf("negative sequence err=%v, want ErrClockCursorInvalid", err)
	}
	token, err := f.IssueClockEventCursor(ClockEventCursor{Tenant: "tenant-a", Sequence: 1, ExpiresAt: 99})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.decodeCursor(token, "tenant-a", "", ""); err != ErrClockCursorInvalid {
		t.Fatalf("expired cursor err=%v, want ErrClockCursorInvalid", err)
	}
}

func TestTodo_TCLOCK_012_WhitelistAndSubjectReferences(t *testing.T) {
	registry := ClockEventSchemas()
	if registry == nil {
		t.Fatal("ClockEventSchemas returned nil")
	}
	for _, kind := range []string{ClockPunchAccepted, ClockPunchRejected, ClockSessionOpened, ClockSessionClosed, ClockExceptionRaised, ClockTimecardApproved, ClockTimecardReopened, ClockDeviceOffline} {
		if schema, ok := registry.Schema(subscription.EventKind(kind), 1); !ok || schema.Digest == "" {
			t.Fatalf("schema %q missing or undigested: %+v", kind, schema)
		}
	}
	payload := map[string]any{
		"worker_id": "worker-a", "session_id": "session-a", "device_id": "device-a",
		"photo": "must never cross", "biometric": "must never cross", "raw_location": "must never cross",
	}
	refs := subjectRefs(payload)
	if len(refs) != 3 || refs[0] != "device_id:device-a" || refs[1] != "session_id:session-a" || refs[2] != "worker_id:worker-a" {
		t.Fatalf("subjectRefs=%v", refs)
	}
	if _, ok := schemaFor("clock.unknown", 1); ok {
		t.Fatal("unknown event schema accepted")
	}
}

func TestTodo_TCLOCK_012_ImmutableSubscriptionConfiguration(t *testing.T) {
	now := time.Unix(200, 0).UTC()
	revision, grants := clockEventSubscription(t)
	source := &clockEventOutboxFake{rows: []OutboxEvent{{Sequence: 1, EventType: ClockPunchAccepted, SchemaVersion: 1, CreatedAt: now, Payload: []byte(`{"receipt_id":"r1","worker_id":"w1"}`)}}}
	feed, err := NewClockEventFeed(ClockEventFeedConfig{Source: source, Authorizer: clockEventAuthFake{}, Revisions: []subscription.EventSubscription{revision}, Grants: grants, Signer: clockEventSigner(t, now), CursorKey: []byte("clock-cursor-key-01234567890123456789"), Destination: "endpoint:clock", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	revision.EventKinds[0] = subscription.EventClockPunchRejected
	revision.DeclaredFields[subscription.EventClockPunchAccepted][0] = "device_id"
	page, err := feed.Pull(context.Background(), ClockEventRequest{Tenant: "tenant-a", Limit: 10})
	if err != nil || len(page.Events) != 1 || page.Events[0].Payload["receipt_id"] != "r1" {
		t.Fatalf("configuration aliased caller memory: page=%+v err=%v", page, err)
	}
}

func TestTodo_TCLOCK_012_RejectsMalformedFieldValues(t *testing.T) {
	cases := []struct {
		name  string
		kind  subscription.FieldType
		value any
		valid bool
	}{
		{"valid instant", subscription.TypeInstant, "2026-09-28T10:00:00Z", true},
		{"invalid instant", subscription.TypeInstant, "tomorrow", false},
		{"empty ref", subscription.TypeRef, "", false},
		{"safe integer", subscription.TypeInteger, float64(42), true},
		{"fractional integer", subscription.TypeInteger, float64(1.5), false},
		{"unsafe integer", subscription.TypeInteger, float64(9007199254740992), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validClockValue(tc.kind, tc.value); got != tc.valid {
				t.Fatalf("validClockValue=%v want=%v", got, tc.valid)
			}
		})
	}
}

type oversizedClockEvents struct{ rows []OutboxEvent }

func (s oversizedClockEvents) ListEvents(context.Context, string, int64, int) ([]OutboxEvent, error) {
	return s.rows, nil
}
func TestTodo_TCLOCK_012_RejectsOversizedSourcePage(t *testing.T) {
	now := time.Unix(200, 0).UTC()
	revision, grants := clockEventSubscription(t)
	row := OutboxEvent{Sequence: 1, EventType: ClockPunchAccepted, SchemaVersion: 1, CreatedAt: now, Payload: []byte(`{"receipt_id":"r1","worker_id":"w1"}`)}
	second := row
	second.Sequence = 2
	feed, err := NewClockEventFeed(ClockEventFeedConfig{Source: oversizedClockEvents{[]OutboxEvent{row, second}}, Authorizer: clockEventAuthFake{}, Revisions: []subscription.EventSubscription{revision}, Grants: grants, Signer: clockEventSigner(t, now), CursorKey: []byte("clock-cursor-key-01234567890123456789"), Destination: "endpoint:clock", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = feed.Pull(context.Background(), ClockEventRequest{Tenant: "tenant-a", Limit: 1}); !errors.Is(err, ErrClockEventInvalid) {
		t.Fatalf("oversized source err=%v", err)
	}
}
func TestTodo_TCLOCK_012_RefreshesCursorExpiryAfterProgress(t *testing.T) {
	now := time.Unix(200, 0).UTC()
	revision, grants := clockEventSubscription(t)
	source := &clockEventOutboxFake{rows: []OutboxEvent{{Sequence: 2, EventType: ClockPunchAccepted, SchemaVersion: 1, CreatedAt: now, Payload: []byte(`{"receipt_id":"r2","worker_id":"w1"}`)}}}
	feed, err := NewClockEventFeed(ClockEventFeedConfig{Source: source, Authorizer: clockEventAuthFake{}, Revisions: []subscription.EventSubscription{revision}, Grants: grants, Signer: clockEventSigner(t, now), CursorKey: []byte("clock-cursor-key-01234567890123456789"), Destination: "endpoint:clock", CursorTTL: time.Minute, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	old, err := feed.IssueClockEventCursor(ClockEventCursor{Tenant: "tenant-a", Sequence: 1, ExpiresAt: now.Add(time.Second).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	page, err := feed.Pull(context.Background(), ClockEventRequest{Tenant: "tenant-a", Limit: 10, Cursor: old})
	if err != nil {
		t.Fatal(err)
	}
	next, err := feed.decodeCursor(page.NextCursor, "tenant-a", "", "")
	if err != nil || next.ExpiresAt != now.Add(time.Minute).Unix() {
		t.Fatalf("next cursor=%+v err=%v", next, err)
	}
}
