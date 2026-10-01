package agentpersonastore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTodo_AGENTP_006_VersionDraftIntegration(t *testing.T) {
	f := newFixture(t, "version-draft-tenant", "other-version-tenant")
	ctx := context.Background()
	s := f.store(t, "version-draft-tenant")
	first := version("version-draft-tenant", "versioned-persona", 1)
	owner, steward := draftOwners(first)
	if err := s.CreateDraft(ctx, first, owner, steward, owner.PrincipalID, f.when); err != nil {
		t.Fatal(err)
	}

	second := version(first.TenantID, first.PersonaID, 2)
	second.ContentDigest = "sha256:persona-v2"
	at := f.when.Add(time.Hour)
	if err := s.CreateVersionDraft(ctx, second, steward.PrincipalID, at); err != nil {
		t.Fatalf("create next version: %v", err)
	}
	got, err := s.GetVersion(ctx, second.PersonaID, second.Version)
	if err != nil || got.ContentDigest != second.ContentDigest || !got.CreatedAt.Equal(at.UTC()) {
		t.Fatalf("stored version = %+v, %v", got, err)
	}
	state, err := s.Lifecycle(ctx, second.PersonaID, second.Version)
	if err != nil || state != StateDraft {
		t.Fatalf("new version lifecycle = %q, %v", state, err)
	}
	events, err := s.ListLifecycle(ctx, second.PersonaID, second.Version)
	if err != nil || len(events) != 1 || events[0].From != "" || events[0].To != StateDraft || events[0].ActorID != steward.PrincipalID || !events[0].OccurredAt.Equal(at.UTC()) {
		t.Fatalf("initial draft events = %+v, %v", events, err)
	}
	otherTenant := f.store(t, "other-version-tenant")
	if _, err := otherTenant.GetVersion(ctx, second.PersonaID, second.Version); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other tenant read version = %v, want ErrNotFound", err)
	}
}

func TestTodo_AGENTP_006_VersionDraftRejectsOwnerAndRevisionMismatch(t *testing.T) {
	f := newFixture(t, "version-draft-checks")
	ctx := context.Background()
	s := f.store(t, "version-draft-checks")
	first := version("version-draft-checks", "checked-persona", 1)
	owner, steward := draftOwners(first)
	if err := s.CreateDraft(ctx, first, owner, steward, owner.PrincipalID, f.when); err != nil {
		t.Fatal(err)
	}

	second := version(first.TenantID, first.PersonaID, 2)
	if err := s.CreateVersionDraft(ctx, second, "user:outsider", f.when.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-owner actor error = %v, want ErrNotFound", err)
	}
	gap := version(first.TenantID, first.PersonaID, 3)
	if err := s.CreateVersionDraft(ctx, gap, owner.PrincipalID, f.when.Add(time.Minute)); !errors.Is(err, ErrConflict) {
		t.Fatalf("version gap error = %v, want ErrConflict", err)
	}
	if _, err := s.GetVersion(ctx, first.PersonaID, second.Version); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rejected version persisted = %v, want ErrNotFound", err)
	}
	if _, err := s.Lifecycle(ctx, first.PersonaID, second.Version); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rejected lifecycle persisted = %v, want ErrNotFound", err)
	}
}
