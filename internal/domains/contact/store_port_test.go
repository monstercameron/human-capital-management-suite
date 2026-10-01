package contact

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestStorePortMemoryStorePreservesCASAndDigestOnlyHistory(t *testing.T) {
	tenant := values.TenantId("tenant-memory")
	subject := values.EntityRef{Tenant: tenant, Kind: "worker", Id: "00000000-0000-4000-8000-000000000001"}
	endpoint, err := NewContactEndpointRevision(subject, "00000000-0000-4000-8000-000000000002", EndpointEmail, "Person@example.com", "recovery", 1, "profile")
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	ctx := context.Background()
	if err := store.PutEndpointRevision(ctx, tenant, endpoint); err != nil {
		t.Fatal(err)
	}
	if got := CodeOf(store.PutEndpointRevision(ctx, tenant, endpoint)); got != StoreDuplicateCode {
		t.Fatalf("duplicate endpoint code = %q", got)
	}
	verified, err := endpoint.MarkVerified()
	if err != nil {
		t.Fatal(err)
	}
	if got := CodeOf(store.PutEndpointRevision(ctx, tenant, verified, 0)); got != StoreStaleCASCode {
		t.Fatalf("out-of-sequence endpoint code = %q", got)
	}
	if got := CodeOf(store.PutEndpointRevision(ctx, tenant, verified)); got != StoreStaleCASCode {
		t.Fatalf("unfenced endpoint code = %q", got)
	}
	if err := store.PutEndpointRevision(ctx, tenant, verified, endpoint.Revision); err != nil {
		t.Fatalf("verified endpoint: %v", err)
	}
	foreign := verified
	foreign.Subject = values.EntityRef{Tenant: tenant, Kind: "worker", Id: "00000000-0000-4000-8000-000000000099"}
	foreign.Revision++
	foreign.SupersedesRevision = verified.Revision
	foreign.CanonicalDigest = foreign.computedDigest()
	if got := CodeOf(store.PutEndpointRevision(ctx, tenant, foreign, verified.Revision)); got != StoreInvalidCode {
		t.Fatalf("cross-subject endpoint code = %q", got)
	}
	challenge, _, err := IssueContactChallenge("00000000-0000-4000-8000-000000000003", subject, endpoint, "recovery", "123456", time.Unix(100, 0), time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutChallenge(ctx, tenant, challenge); err != nil {
		t.Fatal(err)
	}
	updated, _, err := challenge.Respond("wrong", time.Unix(200, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got := CodeOf(store.PutChallenge(ctx, tenant, updated, "sha256:stale")); got != StoreStaleCASCode {
		t.Fatalf("stale challenge code = %q", got)
	}
	if err := store.PutChallenge(ctx, tenant, updated, challenge.CanonicalDigest); err != nil {
		t.Fatal(err)
	}
	duplicateEvent := updated
	duplicateEvent.Events = append(append([]ContactChallengeEvent(nil), updated.Events...), updated.Events[1])
	duplicateEvent.CanonicalDigest = duplicateEvent.computedDigest()
	if got := CodeOf(store.PutChallenge(ctx, tenant, duplicateEvent, updated.CanonicalDigest)); got != StoreDuplicateEventCode {
		t.Fatalf("duplicate challenge event code = %q", got)
	}
	scopeChanged := updated
	scopeChanged.Purpose = "payroll"
	scopeChanged.CanonicalDigest = scopeChanged.computedDigest()
	if got := CodeOf(store.PutChallenge(ctx, tenant, scopeChanged, updated.CanonicalDigest)); got != StoreInvalidCode {
		t.Fatalf("changed challenge scope code = %q", got)
	}
	loaded, err := store.GetChallenge(ctx, tenant, challenge.ChallengeID)
	if err != nil || len(loaded.Events) != 2 {
		t.Fatalf("loaded challenge = %+v, err=%v", loaded, err)
	}
	if errors.Is(err, ErrStoreDatabase) {
		t.Fatal("memory store reported a database failure")
	}
}
