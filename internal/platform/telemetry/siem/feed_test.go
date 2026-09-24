package siem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
)

var testAt = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func signingRing(t *testing.T) *subscription.CredentialRing {
	t.Helper()
	ring, err := subscription.NewCredentialRing("endpoint:siem", func() time.Time { return testAt })
	if err != nil {
		t.Fatal(err)
	}
	provider, err := subscription.NewHMACProvider("kms:siem-key", []byte("test-only-key"))
	if err != nil {
		t.Fatal(err)
	}
	credential := subscription.SigningCredential{Destination: "endpoint:siem", Profile: "hmac-sha256", Version: "v1", KeyRef: "kms:siem-key", NotBefore: testAt.Add(-time.Hour), NotAfter: testAt.Add(time.Hour), Provider: provider}
	if err := ring.Add(credential); err != nil {
		t.Fatal(err)
	}
	if err := ring.Activate("v1", testAt); err != nil {
		t.Fatal(err)
	}
	return ring
}

func appendInput(tenant string, kind EventType, ref string) EventInput {
	in := EventInput{Tenant: tenant, Type: kind, OccurredAt: testAt, SourceRef: ref, EvidenceDigest: digest(ref)}
	if kind == EventAlertRule {
		in.RuleID = "security.break-glass-use"
		in.RuleVersion = 1
	}
	return in
}

func TestTodo_REV_099_05(t *testing.T) {
	stream := NewStream()
	for _, input := range []EventInput{
		appendInput("tenant-a", EventAlertRule, "alert:1"),
		appendInput("tenant-a", EventDLP, "dlp:1"),
		appendInput("tenant-b", EventAdmin, "admin:1"),
	} {
		if _, err := stream.Append(input); err != nil {
			t.Fatal(err)
		}
	}
	first, err := stream.Read("tenant-a", Cursor{Tenant: "tenant-a"}, 1, testAt, signingRing(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(first, "tenant-a", testAt, signingRing(t)); err != nil {
		t.Fatalf("valid feed: %v", err)
	}
	if len(first.Events) != 1 || first.Events[0].Type != EventAlertRule || first.Next.Sequence != 1 || first.Signature.MessageDigest != first.Digest {
		t.Fatalf("feed = %+v", first)
	}
	second, err := stream.Read("tenant-a", first.Next, 10, testAt, signingRing(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Events) != 1 || second.Events[0].Type != EventDLP || second.Next.Sequence != 2 {
		t.Fatalf("resumed feed = %+v", second)
	}
	if _, err := stream.Read("tenant-b", first.Next, 10, testAt, signingRing(t)); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("cross-tenant cursor error = %v", err)
	}
	if len(EventsOfType()) != 4 {
		t.Fatalf("event vocabulary = %v", EventsOfType())
	}
}

func TestTodo_REV_099_05_Golden(t *testing.T) {
	stream := NewStream()
	event, err := stream.Append(appendInput("tenant-a", EventDLP, "dlp:1"))
	if err != nil {
		t.Fatal(err)
	}
	const want = "siem/v1|1|tenant-a|DLP_SIGNAL|2026-09-23T12:00:00Z|dlp:1|c1f99ad890dc982767e399c7d88d780602700b36c1f8e570a13a8bd0efa07aee||0|"
	if got := canonical(event); got != want {
		t.Fatalf("canonical event bytes = %q, want %q", got, want)
	}
}

func TestTodo_REV_099_05_Security(t *testing.T) {
	stream := NewStream()
	if _, err := stream.Append(appendInput("tenant-a", EventDLP, "dlp:1")); err != nil {
		t.Fatal(err)
	}
	ring := signingRing(t)
	feed, err := stream.Read("tenant-a", Cursor{Tenant: "tenant-a"}, 10, testAt, ring)
	if err != nil {
		t.Fatal(err)
	}
	feed.Events[0].Tenant = "tenant-b"
	if err := Verify(feed, "tenant-a", testAt, ring); !errors.Is(err, ErrInvalidFeed) {
		t.Fatalf("mutated tenant error = %v", err)
	}
	feed, err = stream.Read("tenant-a", Cursor{Tenant: "tenant-a"}, 10, testAt, ring)
	if err != nil {
		t.Fatal(err)
	}
	feed.Events[0].EvidenceDigest = digest("other")
	if err := Verify(feed, "tenant-a", testAt, ring); !errors.Is(err, ErrInvalidFeed) {
		t.Fatalf("mutated evidence error = %v", err)
	}
	if _, err := stream.Append(EventInput{Tenant: "tenant-a", Type: EventType("RAW_PAYLOAD"), OccurredAt: testAt, SourceRef: "x", EvidenceDigest: digest("x")}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("unknown type error = %v", err)
	}
	if _, err := stream.Append(EventInput{Tenant: "tenant-a", Type: EventAdmin, OccurredAt: testAt, SourceRef: "x", EvidenceDigest: "raw-secret"}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("non-digest evidence error = %v", err)
	}
}

func TestTodo_REV_099_05_Integration(t *testing.T) {
	at := testAt
	journal := subscription.NewDeliveryJournal(func() time.Time { return at })
	provider := subscription.DeliveryProviderFunc(func(_ context.Context, attempt subscription.DeliveryAttempt) (subscription.ProviderReceipt, error) {
		return subscription.ProviderReceipt{ReceiptID: "receipt-1", Provider: "siem", IdempotencyKey: attempt.Request.IdempotencyKey, Destination: attempt.Request.Destination, Accepted: true, ReceivedAt: at}, nil
	})
	request := subscription.DeliveryRequest{
		SubscriptionID: "siem-sub", SubscriptionRevision: 1, Destination: "endpoint:siem", OrderingKey: "tenant-a", OrderingMode: subscription.OrderingIndependent,
		Envelope: subscription.CanonicalEnvelope{Tenant: "tenant-a", Kind: subscription.EventWorkerChanged, SchemaVersion: 1, SubjectRefs: []string{"security-event:1"}, EffectiveAt: at, KnownAt: at, PayloadDigest: digest("event"), ProvenanceRef: "siem-feed:1", Sequence: 1},
	}
	if _, err := journal.DeliverNow(provider, request); err != nil {
		t.Fatal(err)
	}
	report, err := Reconcile(journal, subscription.CompletenessExpectation{SubscriptionID: "siem-sub", Tenant: "tenant-a", OrderingKey: "tenant-a", FromSequence: 1, ThroughSequence: 1}, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Complete() {
		t.Fatalf("delivery completeness = %+v", report)
	}
	if _, err := Reconcile(journal, subscription.CompletenessExpectation{SubscriptionID: "siem-sub", Tenant: "tenant-b", OrderingKey: "tenant-a", FromSequence: 1, ThroughSequence: 1}, "tenant-a"); !errors.Is(err, subscription.ErrInvalidCompleteness) {
		t.Fatalf("wrong tenant expectation error = %v", err)
	}
}

func TestTodo_REV_099_05_Mutation(t *testing.T) {
	stream := NewStream()
	if _, err := stream.Append(appendInput("tenant-a", EventDLP, "dlp:1")); err != nil {
		t.Fatal(err)
	}
	ring := signingRing(t)
	feed, err := stream.Read("tenant-a", Cursor{Tenant: "tenant-a"}, 10, testAt, ring)
	if err != nil {
		t.Fatal(err)
	}
	feed.Next.Sequence++
	if err := Verify(feed, "tenant-a", testAt, ring); !errors.Is(err, ErrInvalidFeed) {
		t.Fatalf("mutated cursor error = %v", err)
	}
	if _, err := stream.Read("tenant-a", Cursor{Tenant: "tenant-a", Sequence: 1, Digest: "forged"}, 1, testAt, ring); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("forged resume anchor error = %v", err)
	}
}
