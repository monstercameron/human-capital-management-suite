// REV-042-01 RED: the subscription closed vocabulary must include the four
// custom-object event kinds, scoped per tenant/custom-object-type, with an
// activated subscription matching a real custom-object outbox entry end to
// end over digests only.
package subscription

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/custom"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func rev042Definition(kind string) custom.CustomObjectDefinition {
	return custom.CustomObjectDefinition{Kind: kind, Namespace: "tenant.fleet", Version: 1, Fields: map[string]custom.FieldDefinition{
		"registration": {Type: "string", Classification: custom.FieldClassification{AuthZDomain: "worker.core", Classification: "INTERNAL", ResidencyRef: "NO_CONSTRAINT", RetentionClass: "OPERATIONAL"}},
		"active":       {Type: "bool", Classification: custom.FieldClassification{AuthZDomain: "worker.core", Classification: "INTERNAL", ResidencyRef: "NO_CONSTRAINT", RetentionClass: "OPERATIONAL"}},
	}}
}

func rev042Record(t *testing.T, id, kind, registration string) custom.CustomRecordRevision {
	t.Helper()
	at := values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	interval, err := values.NewOpenInstantInterval(at)
	if err != nil {
		t.Fatal(err)
	}
	return custom.CustomRecordRevision{ObjectID: id, ObjectKind: kind, Namespace: "tenant.fleet", DefinitionVersion: 1, Effective: interval,
		FieldValues: map[string]custom.TypedValue{
			"registration": {FieldName: "registration", Type: "string", Value: registration},
			"active":       {FieldName: "active", Type: "bool", Value: true},
		}}
}

func rev042Commit(t *testing.T, store *custom.EventStore, tenant values.TenantId, kind, id, registration string) custom.CommitReceipt {
	t.Helper()
	receipt, err := store.Commit(context.Background(), custom.MutationRequest{
		Tenant: tenant, ObjectID: id, Definition: rev042Definition(kind), Record: rev042Record(t, id, kind, registration),
		Operation: custom.OperationCreate, ExpectedHead: 0, ExpectedRevision: 0,
		Actor: "actor-1", Purpose: "fleet.operations", EvidenceDigest: "evidence-1",
		GrantedDomains: map[string]bool{"worker.core": true},
		OccurredAt:     time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		EffectiveAt:    time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("custom Commit: %v", err)
	}
	return receipt
}

func rev042SubscriptionRequest(id, tenant string) RevisionRequest {
	return RevisionRequest{
		SubscriptionID: id,
		Requester:      "requester-1",
		Subscriber:     Subscriber{PartnerRef: "partner:acme"},
		EventKinds:     []EventKind{EventCustomObjectCreated},
		DeclaredFields: map[EventKind][]string{
			EventCustomObjectCreated: {CustomObjectKindField},
		},
		Filter: Filter{Predicates: []Predicate{
			{Field: CustomObjectKindField, Operator: OperatorEquals, Value: "Vehicle"},
		}},
		DeliveryEndpointRef: "endpoint:webhook-1",
		DeliveryGuarantee:   GuaranteeAtLeastOnce,
		TenantScope:         tenant,
	}
}

func rev042Activate(t *testing.T, id, tenant string) EventSubscription {
	t.Helper()
	draft, err := NewDraft(rev042SubscriptionRequest(id, tenant))
	if err != nil {
		t.Fatalf("NewDraft: %v", err)
	}
	active, err := draft.Activate("requester-2", "approver-1")
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	return active
}

// TestTodo_REV_042_01 is the PRIMARY contract: the four custom-object event
// kinds belong to the closed subscription vocabulary, scoped per
// tenant/custom-object-type, matched over digests only.
func TestTodo_REV_042_01(t *testing.T) {
	for _, kind := range []EventKind{
		EventCustomObjectCreated, EventCustomObjectChanged,
		EventCustomObjectCorrected, EventCustomObjectRetired,
	} {
		if !kind.Valid() {
			t.Fatalf("custom-object event kind %q is not subscribable", kind)
		}
	}
	if EventKind("CUSTOM_OBJECT_DELETED").Valid() {
		t.Fatal("undeclared custom-object kind must stay outside the vocabulary")
	}

	active := rev042Activate(t, "sub-custom-1", "tenant-a")

	matching, err := CustomObjectEventDigest("tenant-a", "Vehicle", EventCustomObjectCreated, "payload-digest-1")
	if err != nil {
		t.Fatalf("CustomObjectEventDigest: %v", err)
	}
	matches, err := MatchActive(matching, []EventSubscription{active})
	if err != nil {
		t.Fatalf("MatchActive: %v", err)
	}
	if len(matches) != 1 || matches[0].SubscriptionID != "sub-custom-1" {
		t.Fatalf("matches = %+v, want exactly sub-custom-1", matches)
	}

	// A different custom-object type under the same tenant must not match:
	// scoping is per tenant AND per custom-object type.
	otherType, err := CustomObjectEventDigest("tenant-a", "Badge", EventCustomObjectCreated, "payload-digest-2")
	if err != nil {
		t.Fatalf("CustomObjectEventDigest: %v", err)
	}
	matches, err = MatchActive(otherType, []EventSubscription{active})
	if err != nil {
		t.Fatalf("MatchActive: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("cross-type matches = %+v, want none", matches)
	}

	// A different tenant must not match either.
	otherTenant, err := CustomObjectEventDigest("tenant-b", "Vehicle", EventCustomObjectCreated, "payload-digest-1")
	if err != nil {
		t.Fatalf("CustomObjectEventDigest: %v", err)
	}
	matches, err = MatchActive(otherTenant, []EventSubscription{active})
	if err != nil {
		t.Fatalf("MatchActive: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("cross-tenant matches = %+v, want none", matches)
	}

	// Digest-only proof: the subscription and the event carry digests, never
	// the raw custom-object payload or undeclared fields.
	blob := fmt.Sprintf("%+v", active) + fmt.Sprintf("%+v", matching)
	for _, raw := range []string{"ABC-123", "registration", "fleet.operations"} {
		if strings.Contains(blob, raw) {
			t.Fatalf("raw custom-object content %q leaked into subscription matching", raw)
		}
	}
	if got := matching.FieldDigests[CustomObjectKindField]; got != DigestValue("Vehicle") {
		t.Fatalf("object-type field digest = %q, want digest-only %q", got, DigestValue("Vehicle"))
	}

	// A filter naming an undeclared field is still refused.
	bad := rev042SubscriptionRequest("sub-custom-bad", "tenant-a")
	bad.Filter.Predicates[0].Field = "custom.raw_payload"
	if _, err := NewDraft(bad); !errors.Is(err, ErrUndeclaredField) {
		t.Fatalf("undeclared filter field error = %v, want ErrUndeclaredField", err)
	}

	// The bridge refuses non-custom kinds so worker events cannot be
	// mis-scoped as custom-object events.
	if _, err := CustomObjectEventDigest("tenant-a", "Vehicle", EventWorkerChanged, "payload-digest-1"); !errors.Is(err, ErrInvalidEventKind) {
		t.Fatalf("non-custom bridge kind error = %v, want ErrInvalidEventKind", err)
	}
}

// TestTodo_REV_042_01_Integration matches a REAL custom-object outbox entry,
// committed through the custom domain, against an activated subscription.
func TestTodo_REV_042_01_Integration(t *testing.T) {
	store := custom.NewEventStore()
	tenant := values.TenantId("tenant-a")
	receipt := rev042Commit(t, store, tenant, "Vehicle", "vehicle-9", "ABC-123")
	if len(store.Outbox()) != 1 {
		t.Fatalf("outbox count = %d, want 1", len(store.Outbox()))
	}
	if receipt.Outbox.PayloadDigest == "" {
		t.Fatal("real outbox entry carries no payload digest")
	}

	event, err := CustomObjectEventDigest(
		receipt.Event.Tenant.String(),
		receipt.Event.Kind,
		EventKind(receipt.Event.Type),
		receipt.Outbox.PayloadDigest,
	)
	if err != nil {
		t.Fatalf("CustomObjectEventDigest: %v", err)
	}

	active := rev042Activate(t, "sub-custom-live", "tenant-a")
	matches, err := MatchActive(event, []EventSubscription{active})
	if err != nil {
		t.Fatalf("MatchActive: %v", err)
	}
	if len(matches) != 1 || matches[0].SubscriptionID != "sub-custom-live" {
		t.Fatalf("matches = %+v, want exactly sub-custom-live", matches)
	}

	// Determinism pin: the same real outbox entry always projects the same
	// digest-only event.
	again, err := CustomObjectEventDigest(
		receipt.Event.Tenant.String(),
		receipt.Event.Kind,
		EventKind(receipt.Event.Type),
		receipt.Outbox.PayloadDigest,
	)
	if err != nil {
		t.Fatalf("CustomObjectEventDigest: %v", err)
	}
	digestOf := func(e EventDigest) string {
		return fmt.Sprintf("%s|%s|%s|%v", e.TenantScope, e.Kind, e.Digest, e.FieldDigests)
	}
	if digestOf(event) != digestOf(again) {
		t.Fatalf("same outbox entry projected differently:\n%v\n%v", event, again)
	}

	// The raw payload value from the committed record appears nowhere in the
	// matching inputs.
	blob := fmt.Sprintf("%+v", active) + fmt.Sprintf("%+v", event)
	if strings.Contains(blob, "ABC-123") {
		t.Fatal("raw custom-object payload leaked into subscription matching")
	}
}
