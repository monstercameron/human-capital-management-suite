package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

func TestIssue3929_SavedSearchStore_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	me := chatsearch.Actor{TenantID: "host", HomeTenantID: "host", PersonID: "u-1"}
	colleague := chatsearch.Actor{TenantID: "host", HomeTenantID: "host", PersonID: "u-2"}
	elsewhere := chatsearch.Actor{TenantID: "other", HomeTenantID: "other", PersonID: "u-1"}

	saved, err := s.CreateSaved(ctx, me, chatsearch.SavedSearch{Name: "Onboarding", Query: "onboarding in:general has:file"}, 3)
	if err != nil || saved.ID == "" || saved.Query != "onboarding in:general has:file" || saved.UpdatedAt.IsZero() {
		t.Fatalf("create = %+v err=%v", saved, err)
	}
	if _, err := s.CreateSaved(ctx, me, chatsearch.SavedSearch{Name: "ONBOARDING", Query: "x"}, 3); !errors.Is(err, chatsearch.ErrSavedNameUsed) {
		t.Fatalf("duplicate name: %v", err)
	}
	// A colleague may use the same name; the shortcut is theirs alone.
	if _, err := s.CreateSaved(ctx, colleague, chatsearch.SavedSearch{Name: "Onboarding", Query: "other words"}, 3); err != nil {
		t.Fatalf("colleague create: %v", err)
	}

	// Privacy: no one else, in this workspace or another, reads, edits or removes it.
	for name, other := range map[string]chatsearch.Actor{"colleague": colleague, "other workspace": elsewhere} {
		rows, err := s.ListSaved(ctx, other)
		for _, r := range rows {
			if r.ID == saved.ID || r.Query == saved.Query {
				t.Fatalf("%s listed the shortcut: %+v err=%v", name, rows, err)
			}
		}
		if _, err := s.UpdateSaved(ctx, other, chatsearch.SavedSearch{ID: saved.ID, Name: "mine now", Query: "hijacked"}); !errors.Is(err, chatsearch.ErrSavedNotFound) {
			t.Fatalf("%s edited it: %v", name, err)
		}
		if err := s.DeleteSaved(ctx, other, saved.ID); !errors.Is(err, chatsearch.ErrSavedNotFound) {
			t.Fatalf("%s removed it: %v", name, err)
		}
	}

	edited, err := s.UpdateSaved(ctx, me, chatsearch.SavedSearch{ID: saved.ID, Name: "Onboarding Q", Query: "visa"})
	if err != nil || edited.Name != "Onboarding Q" || edited.Query != "visa" {
		t.Fatalf("edit = %+v err=%v", edited, err)
	}
	rows, err := s.ListSaved(ctx, me)
	if err != nil || len(rows) != 1 || rows[0].Query != "visa" {
		t.Fatalf("list = %+v err=%v", rows, err)
	}

	// The limit counts the owner own shortcuts only.
	for _, n := range []string{"b", "c"} {
		if _, err := s.CreateSaved(ctx, me, chatsearch.SavedSearch{Name: n, Query: n}, 3); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateSaved(ctx, me, chatsearch.SavedSearch{Name: "d", Query: "d"}, 3); !errors.Is(err, chatsearch.ErrSavedLimit) {
		t.Fatalf("limit: %v", err)
	}
	if _, err := s.UpdateSaved(ctx, me, chatsearch.SavedSearch{ID: saved.ID, Name: "B", Query: "x"}); !errors.Is(err, chatsearch.ErrSavedNameUsed) {
		t.Fatalf("rename into a used name: %v", err)
	}

	if err := s.DeleteSaved(ctx, me, saved.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSaved(ctx, me, saved.ID); !errors.Is(err, chatsearch.ErrSavedNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if rows, _ := s.ListSaved(ctx, colleague); len(rows) != 1 {
		t.Fatalf("colleague lost theirs: %+v", rows)
	}
}
