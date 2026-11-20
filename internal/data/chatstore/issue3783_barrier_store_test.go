package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatbarrier"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func barrierRule(t *testing.T, id, a, b string) chatbarrier.Rule {
	t.Helper()
	r, err := chatbarrier.NewRule(id, a, b, "admin-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestIssue3783_BarrierStore_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()

	saved, err := s.AddBarrierRule(ctx, "host", barrierRule(t, "r1", "Sales", "Advisory"))
	if err != nil || saved.GroupA != "advisory" || saved.GroupB != "sales" || saved.CreatedAt.IsZero() {
		t.Fatalf("add = %+v err=%v", saved, err)
	}
	// The same pair, in either order or case, is one rule.
	if _, err := s.AddBarrierRule(ctx, "host", barrierRule(t, "r2", "ADVISORY", " sales ")); !errors.Is(err, chat.ErrAlreadyExists) {
		t.Fatalf("duplicate pair: %v", err)
	}
	rules, err := s.ListBarrierRules(ctx, "host")
	if err != nil || len(rules) != 1 || rules[0].LabelA != "Advisory" || rules[0].LabelB != "Sales" {
		t.Fatalf("list = %+v err=%v", rules, err)
	}
	if !rules.Separates("sales", "ADVISORY") {
		t.Fatal("the stored rule must separate the two groups")
	}

	// Another workspace neither sees nor revokes the rule, and is not bound by it.
	if other, err := s.ListBarrierRules(ctx, "other"); err != nil || len(other) != 0 {
		t.Fatalf("another workspace saw %+v err=%v", other, err)
	}
	if err := s.RemoveBarrierRule(ctx, "other", "r1", "admin-9"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("cross-tenant revoke: %v", err)
	}
	if again, _ := s.ListBarrierRules(ctx, "host"); len(again) != 1 {
		t.Fatalf("the rule was revoked from outside: %+v", again)
	}

	// Revoking keeps the history, frees the pair, and is audited.
	if err := s.RemoveBarrierRule(ctx, "host", "r1", "admin-2"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveBarrierRule(ctx, "host", "r1", "admin-2"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("second revoke: %v", err)
	}
	if left, _ := s.ListBarrierRules(ctx, "host"); len(left) != 0 {
		t.Fatalf("revoked rule still in force: %+v", left)
	}
	if _, err := s.AddBarrierRule(ctx, "host", barrierRule(t, "r3", "Sales", "Advisory")); err != nil {
		t.Fatalf("a revoked pair can be set again: %v", err)
	}
	var actions []string
	err = s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT action||':'||actor_id||':'||target_id FROM chat_audit_event WHERE tenant_id='host' AND target_type='chat_barrier' ORDER BY sequence`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				return err
			}
			actions = append(actions, line)
		}
		return rows.Err()
	})
	want := []string{"chat.barrier.add:admin-1:r1", "chat.barrier.remove:admin-2:r1", "chat.barrier.add:admin-1:r3"}
	if err != nil || len(actions) != len(want) {
		t.Fatalf("audit = %v err=%v", actions, err)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Fatalf("audit[%d] = %q, want %q", i, actions[i], want[i])
		}
	}
}

func TestIssue3783_BarrierStoreRefusesBadInput_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	bad := barrierRule(t, "r1", "Sales", "Advisory")
	bad.GroupA, bad.GroupB = bad.GroupB, bad.GroupA
	if _, err := s.AddBarrierRule(ctx, "host", bad); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("an unordered pair must be refused: %v", err)
	}
	if _, err := s.AddBarrierRule(ctx, "", barrierRule(t, "r1", "A", "B")); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("no tenant: %v", err)
	}
	if err := s.RemoveBarrierRule(ctx, "host", "", "admin"); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("no id: %v", err)
	}
}
