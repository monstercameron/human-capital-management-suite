package agentpersonastore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTodo_AGENT_015_PersonaAgentPrincipalBindingIsExactAndTenantScoped(t *testing.T) {
	f := newFixture(t, "tenant-a", "tenant-b")
	ctx := context.Background()
	a := f.store(t, "tenant-a")
	b := f.store(t, "tenant-b")
	v := version("tenant-a", "persona:benefits", 4)
	owner, steward := draftOwners(v)
	if err := a.CreateDraft(ctx, v, owner, steward, "user:creator", f.when); err != nil {
		t.Fatal(err)
	}
	principalID := uuid.New()
	if err := a.RegisterPersonaAgentPrincipal(ctx, v.PersonaID, v.Version, principalID, f.when); err != nil {
		t.Fatal(err)
	}
	got, err := a.ResolvePersonaAgentPrincipal(ctx, v.PersonaID, v.Version)
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "tenant-a" || got.PersonaID != v.PersonaID || got.PersonaVersion != v.Version || got.PrincipalID != principalID || !got.ProvisionedAt.Equal(f.when) {
		t.Fatalf("binding = %+v", got)
	}
	if _, err := b.ResolvePersonaAgentPrincipal(ctx, v.PersonaID, v.Version); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant binding read = %v, want ErrNotFound", err)
	}
	if _, err := a.ResolvePersonaAgentPrincipal(ctx, v.PersonaID, v.Version+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nonexistent version binding read = %v, want ErrNotFound", err)
	}
	if err := a.RegisterPersonaAgentPrincipal(ctx, v.PersonaID, v.Version, uuid.New(), f.when.Add(time.Second)); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate exact-version binding error = %v, want ErrConflict", err)
	}
	if _, err := a.ResolvePersonaAgentPrincipal(ctx, v.PersonaID, v.Version); err != nil {
		t.Fatalf("duplicate binding changed original: %v", err)
	}
}

func TestTodo_AGENT_015_PersonaAgentPrincipalBindingRequiresPublishedVersionRow(t *testing.T) {
	f := newFixture(t, "tenant-a")
	store := f.store(t, "tenant-a")
	err := store.RegisterPersonaAgentPrincipal(context.Background(), "persona:missing", 1, uuid.New(), f.when)
	if err == nil {
		t.Fatal("binding without an immutable persona version was accepted")
	}
	if _, err := store.ResolvePersonaAgentPrincipal(context.Background(), "persona:missing", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing binding error = %v, want ErrNotFound", err)
	}
}
