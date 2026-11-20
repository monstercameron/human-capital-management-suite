package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func caseSeed(t *testing.T, s *Store, tenant, id, kind string, members ...string) {
	t.Helper()
	err := s.RunTenantTx(context.Background(), tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(context.Background(), `INSERT INTO chat_conversation(id,tenant_id,kind,name,owner_id) VALUES($1,$2,$3,'Case thread','mgr')`, id, tenant, kind); err != nil {
			return err
		}
		for _, m := range members {
			if _, err := tx.Exec(context.Background(), `INSERT INTO chat_membership(tenant_id,conversation_id,member_id,home_tenant_id) VALUES($1,$2,$3,$1)`, tenant, id, m); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestIssue3812_CaseStore_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	caseSeed(t, s, "host", "thread", "GROUP", "mgr", "hr", "emp")
	caseSeed(t, s, "other", "thread-other", "GROUP", "mgr", "hr")

	if _, found, err := s.ActiveCase(ctx, "host", "thread"); err != nil || found {
		t.Fatalf("before binding: found=%v err=%v", found, err)
	}
	bound, err := s.BindCase(ctx, "host", "b1", chat.CaseBinding{ConversationID: "thread", CaseRef: " ER-2041 ", OwnerID: "hr", Restricted: true, BoundBy: "admin-1"})
	if err != nil || bound.CaseRef != "ER-2041" || bound.BoundAt.IsZero() {
		t.Fatalf("bind = %+v err=%v", bound, err)
	}
	got, found, err := s.ActiveCase(ctx, "host", "thread")
	if err != nil || !found || got.OwnerID != "hr" || !got.Restricted || got.CaseRef != "ER-2041" || got.BoundBy != "admin-1" {
		t.Fatalf("active = %+v found=%v err=%v", got, found, err)
	}
	// Another workspace neither sees the binding nor lifts it.
	if _, found, err := s.ActiveCase(ctx, "other", "thread"); err != nil || found {
		t.Fatalf("a binding leaked across workspaces: found=%v err=%v", found, err)
	}
	if err := s.UnbindCase(ctx, "other", "thread", "admin-9"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("cross-tenant unbind: %v", err)
	}
	if _, found, _ := s.ActiveCase(ctx, "host", "thread"); !found {
		t.Fatal("the binding was lifted from outside")
	}

	// Binding again replaces the one in force, keeping both in the history.
	if _, err := s.BindCase(ctx, "host", "b2", chat.CaseBinding{ConversationID: "thread", CaseRef: "ER-2041", OwnerID: "mgr", Restricted: false, BoundBy: "admin-2"}); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.ActiveCase(ctx, "host", "thread"); got.OwnerID != "mgr" || got.Restricted {
		t.Fatalf("after rebind = %+v", got)
	}
	if err := s.UnbindCase(ctx, "host", "thread", "admin-3"); err != nil {
		t.Fatal(err)
	}
	if err := s.UnbindCase(ctx, "host", "thread", "admin-3"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("second unbind: %v", err)
	}
	if _, found, _ := s.ActiveCase(ctx, "host", "thread"); found {
		t.Fatal("a lifted binding is still in force")
	}

	var rows []string
	err = s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		r, err := tx.Query(ctx, `SELECT action||':'||actor_id||':'||target_id FROM chat_audit_event WHERE tenant_id='host' AND target_type='chat_case' ORDER BY sequence`)
		if err != nil {
			return err
		}
		defer r.Close()
		for r.Next() {
			var line string
			if err := r.Scan(&line); err != nil {
				return err
			}
			rows = append(rows, line)
		}
		return r.Err()
	})
	want := []string{"chat.case.bind:admin-1:thread", "chat.case.unbind:admin-2:thread", "chat.case.bind:admin-2:thread", "chat.case.unbind:admin-3:thread"}
	if err != nil || len(rows) != len(want) {
		t.Fatalf("audit = %v err=%v", rows, err)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("audit[%d] = %q, want %q", i, rows[i], want[i])
		}
	}
	var history int
	_ = s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM chat_conversation_case WHERE tenant_id='host'`).Scan(&history)
	})
	if history != 2 {
		t.Fatalf("history rows = %d; a binding must be lifted, never deleted", history)
	}
}

func TestIssue3812_CaseStoreRefusesBadBindings_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	caseSeed(t, s, "host", "thread", "GROUP", "mgr", "hr")
	caseSeed(t, s, "host", "town", "PUBLIC_CHANNEL", "mgr", "hr")
	b := chat.CaseBinding{ConversationID: "thread", CaseRef: "ER-1", OwnerID: "hr", Restricted: true, BoundBy: "admin"}
	for name, mutate := range map[string]func(*chat.CaseBinding){
		"no case":              func(b *chat.CaseBinding) { b.CaseRef = "  " },
		"too long a case":      func(b *chat.CaseBinding) { b.CaseRef = string(make([]rune, MaxCaseRefRunes+1)) },
		"no owner":             func(b *chat.CaseBinding) { b.OwnerID = "" },
		"owner not a member":   func(b *chat.CaseBinding) { b.OwnerID = "stranger" },
		"public channel":       func(b *chat.CaseBinding) { b.ConversationID = "town" },
		"conversation missing": func(b *chat.CaseBinding) { b.ConversationID = "nowhere" },
	} {
		c := b
		mutate(&c)
		_, err := s.BindCase(ctx, "host", "x-"+name, c)
		if name == "conversation missing" {
			if !errors.Is(err, chat.ErrNotFound) {
				t.Fatalf("%s: %v", name, err)
			}
			continue
		}
		if !errors.Is(err, chat.ErrInvalidArgument) {
			t.Fatalf("%s: err = %v; want invalid argument", name, err)
		}
	}
	if _, found, _ := s.ActiveCase(ctx, "host", "thread"); found {
		t.Fatal("a refused binding was stored")
	}
	if _, err := s.BindCase(ctx, "", "x", b); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("no tenant: %v", err)
	}
}
