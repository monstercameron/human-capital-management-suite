package agentpersonastore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_008_Integration_ChatIdentityExactBinding(t *testing.T) {
	f := newFixture(t, "chat-identity-tenant", "other-tenant")
	s := f.store(t, "chat-identity-tenant")
	ctx := context.Background()
	registered := f.when
	if err := s.RegisterPersonaChatIdentity(ctx, "agent:comp", "persona.comp", registered); err != nil {
		t.Fatal(err)
	}
	got, err := s.LookupPersonaChatIdentity(ctx, "agent:comp")
	if err != nil || got.TenantID != "chat-identity-tenant" || got.AgentID != "agent:comp" || got.PersonaID != "persona.comp" || !got.Active || !got.RegisteredAt.Equal(registered) {
		t.Fatalf("identity = %+v, %v", got, err)
	}
	if _, err := s.LookupPersonaChatIdentity(ctx, "persona.comp"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("persona id lookup = %v, want ErrNotFound", err)
	}
	if err := s.RegisterPersonaChatIdentity(ctx, "agent:comp", "persona.other", registered); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate agent binding = %v, want ErrConflict", err)
	}
	if err := s.RegisterPersonaChatIdentity(ctx, "agent:other", "persona.comp", registered); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate persona binding = %v, want ErrConflict", err)
	}
	if err := s.RevokePersonaChatIdentity(ctx, "agent:comp", registered.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err = s.LookupPersonaChatIdentity(ctx, "agent:comp")
	if err != nil || got.Active || got.RevokedAt == nil {
		t.Fatalf("revoked identity = %+v, %v", got, err)
	}
	if err := s.RevokePersonaChatIdentity(ctx, "agent:comp", registered.Add(2*time.Minute)); !errors.Is(err, ErrConflict) {
		t.Fatalf("repeat revoke = %v, want ErrConflict", err)
	}
}

func TestTodo_AGENTP_008_Security_ChatIdentityTenantIsolation(t *testing.T) {
	f := newFixture(t, "identity-a", "identity-b")
	a, b := f.store(t, "identity-a"), f.store(t, "identity-b")
	if err := a.RegisterPersonaChatIdentity(context.Background(), "agent:same", "persona:a", f.when); err != nil {
		t.Fatal(err)
	}
	if _, err := b.LookupPersonaChatIdentity(context.Background(), "agent:same"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign identity lookup = %v, want ErrNotFound", err)
	}
	if err := b.RegisterPersonaChatIdentity(context.Background(), "agent:same", "persona:b", f.when); err != nil {
		t.Fatalf("tenant-local identity registration = %v", err)
	}
	if err := b.RevokePersonaChatIdentity(context.Background(), "agent:same", f.when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err := a.LookupPersonaChatIdentity(context.Background(), "agent:same")
	if err != nil || !got.Active || got.PersonaID != "persona:a" {
		t.Fatalf("tenant A identity changed through tenant B = %+v, %v", got, err)
	}
}

func TestTodo_AGENTP_019_Integration_ListActiveChatIdentitiesIsBounded(t *testing.T) {
	f := newFixture(t, "list-identities", "list-other")
	s := f.store(t, "list-identities")
	ctx := context.Background()
	for _, item := range []struct{ agent, persona string }{
		{"agent:z", "persona:z"}, {"agent:a", "persona:a"}, {"agent:m", "persona:m"},
	} {
		if err := s.RegisterPersonaChatIdentity(ctx, item.agent, item.persona, f.when); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RevokePersonaChatIdentity(ctx, "agent:m", f.when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListActivePersonaChatIdentities(ctx, 2)
	if err != nil || len(got) != 2 || got[0].AgentID != "agent:a" || got[1].AgentID != "agent:z" {
		t.Fatalf("bounded active identities = %+v, %v", got, err)
	}
	for _, item := range got {
		if !item.Active || item.TenantID != "list-identities" || item.RevokedAt != nil {
			t.Fatalf("listed identity is not active and tenant-scoped: %+v", item)
		}
	}
	root, err := New(f.db.NewConn(t), func(id values.TenantId) uuid.UUID { return f.ids[id] })
	if err != nil {
		t.Fatal(err)
	}
	all, err := root.ListPersonaChatIdentities(ctx, "list-identities")
	if err != nil || len(all) != 2 {
		t.Fatalf("root active identity list = %+v, %v", all, err)
	}
	if _, err := root.ListPersonaChatIdentities(ctx, "list-other"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListActivePersonaChatIdentities(ctx, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero limit = %v, want ErrInvalid", err)
	}
	if _, err := s.ListActivePersonaChatIdentities(ctx, maxPersonaChatIdentityList+1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized limit = %v, want ErrInvalid", err)
	}
}

func TestTodo_AGENTP_008_InvalidChatIdentityInputs(t *testing.T) {
	f := newFixture(t, "identity-invalid")
	s := f.store(t, "identity-invalid")
	ctx := context.Background()
	for name, fn := range map[string]func() error{
		"empty agent":      func() error { return s.RegisterPersonaChatIdentity(ctx, "", "persona", f.when) },
		"empty persona":    func() error { return s.RegisterPersonaChatIdentity(ctx, "agent", "", f.when) },
		"zero time":        func() error { return s.RegisterPersonaChatIdentity(ctx, "agent", "persona", time.Time{}) },
		"revoke zero time": func() error { return s.RevokePersonaChatIdentity(ctx, "agent", time.Time{}) },
		"lookup empty":     func() error { _, err := s.LookupPersonaChatIdentity(ctx, ""); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := fn(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}
}
