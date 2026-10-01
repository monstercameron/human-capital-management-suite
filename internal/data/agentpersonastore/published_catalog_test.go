package agentpersonastore

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_004_PublishedCatalogLifecycle(t *testing.T) {
	f := newFixture(t, "catalog-tenant")
	s := f.store(t, "catalog-tenant")
	ctx := context.Background()

	published := createCatalogPersona(t, s, "catalog-tenant", "published", StatePublished)
	suspended := createCatalogPersona(t, s, "catalog-tenant", "suspended", StateSuspended)
	retired := createCatalogPersona(t, s, "catalog-tenant", "retired", StateRetired)
	draft := createCatalogPersona(t, s, "catalog-tenant", "draft", StateDraft)
	inReview := createCatalogPersona(t, s, "catalog-tenant", "in-review", StateInReview)

	got, err := s.ListPublished(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertPublishedIDs(t, got, published.PersonaID)

	if err := appendLifecycleForTest(t, s, ctx, lifecycle(suspended.TenantID, suspended.PersonaID, suspended.Version, "resume-suspended", StateSuspended, StatePublished)); err != nil {
		t.Fatal(err)
	}
	got, err = s.ListPublished(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertPublishedIDs(t, got, published.PersonaID, suspended.PersonaID)

	if err := appendLifecycleForTest(t, s, ctx, lifecycle(published.TenantID, published.PersonaID, published.Version, "suspend-published", StatePublished, StateSuspended)); err != nil {
		t.Fatal(err)
	}
	got, err = s.ListPublished(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertPublishedIDs(t, got, suspended.PersonaID)

	// Terminal retirement remains excluded, while drafts and review candidates
	// never leak into the invocable catalog.
	for _, id := range []string{retired.PersonaID, draft.PersonaID, inReview.PersonaID} {
		for _, item := range got {
			if item.PersonaID == id {
				t.Fatalf("non-published persona %q appeared in catalog: %+v", id, item)
			}
		}
	}
}

func TestTodo_AGENTP_004_PublishedCatalogTenantIsolation(t *testing.T) {
	f := newFixture(t, "catalog-owner", "catalog-other")
	owner := f.store(t, "catalog-owner")
	other := f.store(t, "catalog-other")
	persona := createCatalogPersona(t, owner, "catalog-owner", "tenant-private", StatePublished)

	got, err := other.ListPublished(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("other tenant observed published personas: %+v", got)
	}

	// RLS must also fail closed if a caller queries without a tenant setting;
	// the store's explicit tenant predicate is defense in depth, not a bypass.
	conn := f.db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM persona_versions WHERE tenant_id=$1`, f.ids[persona.TenantID]).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatalf("tenant RLS exposed %d persona versions without a tenant scope", visible)
	}
}

func createCatalogPersona(t *testing.T, s *TenantStore, tenant values.TenantId, id string, target LifecycleState) PersonaVersion {
	t.Helper()
	v := version(tenant, id, 1)
	owner, steward := draftOwners(v)
	if err := s.CreateDraft(context.Background(), v, owner, steward, "user:creator", v.CreatedAt); err != nil {
		t.Fatal(err)
	}
	state := StateDraft
	for _, next := range []LifecycleState{StateInReview, StatePublished, StateSuspended, StateRetired} {
		if state == target {
			break
		}
		if !validTransition(state, next) {
			continue
		}
		eventID := id + "-" + string(next)
		if err := appendLifecycleForTest(t, s, context.Background(), lifecycle(tenant, id, v.Version, eventID, state, next)); err != nil {
			t.Fatal(err)
		}
		state = next
	}
	if state != target {
		t.Fatalf("test fixture reached %q, want %q", state, target)
	}
	return v
}

func assertPublishedIDs(t *testing.T, got []PersonaVersion, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("published catalog has %d personas, want %d: %+v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].PersonaID != id || got[i].TenantID == "" || got[i].Version != 1 {
			t.Fatalf("published catalog[%d] = %+v, want persona %q version 1", i, got[i], id)
		}
	}
}
