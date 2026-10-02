package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type chatgateAuthority struct{}

func (chatgateAuthority) Check(_ context.Context, a chatgate.Actor, _ chatgate.Scope, p string) error {
	if p == "admin" && a.Person != "owner" {
		return chatgate.ErrDenied
	}
	return nil
}
func (chatgateAuthority) Policy(context.Context, chatgate.Scope) (chatgate.Policy, error) {
	return chatgate.Policy{}, nil
}
func (chatgateAuthority) Facts(context.Context, chatgate.Actor, chatgate.Scope) (map[string]string, error) {
	return map[string]string{}, nil
}
func (chatgateAuthority) Reference(context.Context, chatgate.Actor, chatgate.Scope, chatgate.Field, json.RawMessage) error {
	return nil
}
func chatgateFixture(t *testing.T) (*Store, *chatgate.Service, chatgate.Command) {
	t.Helper()
	store, _ := chatFixture(t)
	seedConversationRow(t, store, Conversation{ID: "gated", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", Role: "manager", State: "active"}})
	repo := &GateRepository{Store: store, Membership: func(ctx context.Context, tx dbport.Tx, scope chatgate.Scope, a chatgate.Actor, _ string, admit bool) error {
		if admit {
			_, e := tx.Exec(ctx, `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id) VALUES($1,$2,$1,$3) ON CONFLICT(tenant_id,conversation_id,home_tenant_id,member_id) DO UPDATE SET state='active',left_at=NULL,revision=chat_membership.revision+1`, scope.Tenant, scope.Conversation, a.Person)
			return e
		}
		_, e := tx.Exec(ctx, `UPDATE chat_membership SET state='left',left_at=now(),revision=revision+1 WHERE tenant_id=$1 AND conversation_id=$2 AND member_id=$3`, scope.Tenant, scope.Conversation, a.Person)
		return e
	}}
	service := &chatgate.Service{Repository: repo, Registry: chatgate.NewRegistry(), Authority: chatgateAuthority{}, Clock: func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }}
	c := chatgate.Command{Scope: chatgate.Scope{Tenant: "tenant-a", Conversation: "gated"}, Actor: chatgate.Actor{Tenant: "tenant-a", Person: "owner"}, Key: "define"}
	d := chatgate.Definition{Mode: "automatic", Purpose: "Join discussion", Fields: []chatgate.Field{{ID: "intro", Kind: "short_text", KindVersion: "1.0.0", Label: "Introduction", Purpose: "Welcome", DataClass: "INTERNAL", Required: true, Visibility: chatgate.Visibility{Administrators: true}, RetentionDays: 30}}}
	if e := service.Define(t.Context(), c, d); e != nil {
		t.Fatal(e)
	}
	c.Key = "publish"
	c.ExpectedRevision = 1
	if _, e := service.Publish(t.Context(), c, chatgate.Version{Major: 1}); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision = 2
	return store, service, c
}
func TestTodo_CHATGATE_002_Integration(t *testing.T) {
	store, service, c := chatgateFixture(t)
	g, e := service.Get(t.Context(), c.Actor, c.Scope, true)
	if e != nil || g.Current != "1.0.0" {
		t.Fatalf("gate %+v %v", g, e)
	}
	e = store.RunTenantTx(t.Context(), "tenant-a", func(tx dbport.Tx) error {
		_, e := tx.Exec(t.Context(), `UPDATE chat_gate_version SET digest=digest WHERE tenant_id='tenant-a'`)
		return e
	})
	if e == nil {
		t.Fatal("published version allowed mutation")
	}
	other := c.Scope
	other.Tenant = "tenant-b"
	if _, e = service.Get(t.Context(), chatgate.Actor{Tenant: "tenant-b", Person: "owner"}, other, true); !errors.Is(e, chatgate.ErrNotFound) {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_004_Integration(t *testing.T) {
	store, service, c := chatgateFixture(t)
	c.Actor.Person = "worker"
	c.Key = "submit"
	sub, e := service.Submit(t.Context(), c, "1.0.0", map[string]json.RawMessage{"intro": json.RawMessage(`"Hello"`)})
	if e != nil || sub.Status != "admitted" {
		t.Fatalf("submission %+v %v", sub, e)
	}
	e = store.RunTenantTx(t.Context(), "tenant-a", func(tx dbport.Tx) error {
		var n int
		if err := tx.QueryRow(t.Context(), `SELECT count(*) FROM chat_membership m JOIN chat_gate_membership_basis b ON b.tenant_id=m.tenant_id AND b.conversation_id=m.conversation_id AND b.person_id=m.member_id WHERE m.tenant_id='tenant-a' AND m.member_id='worker' AND m.state='active' AND b.semantic_version='1.0.0' AND b.active`).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return errors.New("membership and basis must commit together")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}

func TestTodo_CHATGATE_004_Fault(t *testing.T) {
	store, service, c := chatgateFixture(t)
	repo := service.Repository.(*GateRepository)
	real := repo.Membership
	repo.Membership = func(ctx context.Context, tx dbport.Tx, scope chatgate.Scope, a chatgate.Actor, v string, b bool) error {
		if e := real(ctx, tx, scope, a, v, b); e != nil {
			return e
		}
		return chatgate.ErrUnavailable
	}
	c.Actor.Person = "worker"
	c.Key = "fail"
	if _, e := service.Submit(t.Context(), c, "1.0.0", map[string]json.RawMessage{"intro": json.RawMessage(`"Hello"`)}); !errors.Is(e, chatgate.ErrUnavailable) {
		t.Fatal(e)
	}
	if e := store.RunTenantTx(t.Context(), "tenant-a", func(tx dbport.Tx) error {
		var n int
		if e := tx.QueryRow(t.Context(), `SELECT count(*) FROM chat_membership WHERE tenant_id='tenant-a' AND member_id='worker'`).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			return errors.New("membership survived rollback")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_007_Integration(t *testing.T) {
	store, service, c := chatgateFixture(t)
	owner := c.Actor
	c.Actor.Person = "worker"
	c.Key = "submit"
	sub, e := service.Submit(t.Context(), c, "1.0.0", map[string]json.RawMessage{"intro": json.RawMessage(`"Hello"`)})
	if e != nil {
		t.Fatal(e)
	}
	values, e := service.ReadAnswers(t.Context(), chatgate.ReadRequest{Actor: owner, Scope: c.Scope, Person: "worker", Purpose: "Review"})
	if e != nil || string(values["intro"]) != `"Hello"` {
		t.Fatalf("read %v %v", values, e)
	}
	c.Key = "withdraw"
	c.ExpectedRevision++
	if e = service.Withdraw(t.Context(), c, sub.ID, 1); e != nil {
		t.Fatal(e)
	}
	if e = store.RunTenantTx(t.Context(), "tenant-a", func(tx dbport.Tx) error {
		var answers, audits int
		if e := tx.QueryRow(t.Context(), `SELECT count(*) FROM chat_gate_answer WHERE tenant_id='tenant-a'`).Scan(&answers); e != nil {
			return e
		}
		if e := tx.QueryRow(t.Context(), `SELECT count(*) FROM chat_gate_read_audit WHERE tenant_id='tenant-a'`).Scan(&audits); e != nil {
			return e
		}
		if answers != 0 || audits != 1 {
			return errors.New("erasure or audit missing")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_008_Integration(t *testing.T) {
	store, service, c := chatgateFixture(t)
	c.Actor.Person = "worker"
	c.Key = "submit"
	for i := 0; i < 2; i++ {
		if _, e := service.Submit(t.Context(), c, "1.0.0", map[string]json.RawMessage{"intro": json.RawMessage(`"Hello"`)}); e != nil {
			t.Fatal(e)
		}
	}
	if e := store.RunTenantTx(t.Context(), "tenant-a", func(tx dbport.Tx) error {
		var n int
		if e := tx.QueryRow(t.Context(), `SELECT count(*) FROM chat_outbox WHERE tenant_id='tenant-a' AND event_type IN ('gate.submitted','gate.admitted')`).Scan(&n); e != nil {
			return e
		}
		if n != 2 {
			return errors.New("replay multiplied outbox")
		}
		var leaked int
		if e := tx.QueryRow(t.Context(), `SELECT count(*) FROM chat_outbox WHERE payload::text LIKE '%Hello%'`).Scan(&leaked); e != nil {
			return e
		}
		if leaked != 0 {
			return errors.New("event leaked answer")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
