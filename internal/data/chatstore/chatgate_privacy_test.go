package chatstore

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_CHATGATE_004_Security(t *testing.T) {
	store, service, c := chatgateFixture(t)
	e := store.RunTenantTx(t.Context(), "tenant-a", func(tx dbport.Tx) error {
		_, e := tx.Exec(t.Context(), `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id) VALUES('tenant-a','gated','tenant-a','bypass')`)
		return e
	})
	if e == nil {
		t.Fatal("direct add skipped gate")
	}
	c.Key = "override"
	if e = service.Override(t.Context(), c, "approved", "Administrator approved"); e != nil {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_005_Integration(t *testing.T) {
	_, service, c := chatgateFixture(t)
	c.Actor.Person = "worker"
	c.Key = "save1"
	if e := service.SaveDraft(t.Context(), c, "1.0.0", map[string]json.RawMessage{"intro": json.RawMessage(`"First"`)}); e != nil {
		t.Fatal(e)
	}
	c.Key = "save2"
	c.ExpectedRevision++
	if e := service.SaveDraft(t.Context(), c, "1.0.0", map[string]json.RawMessage{"intro": json.RawMessage(`"Changed"`)}); e != nil {
		t.Fatal(e)
	}
	answers, e := service.DraftAnswers(t.Context(), c.Actor, c.Scope)
	if e != nil || string(answers["intro"]) != `"Changed"` {
		t.Fatal(answers, e)
	}
}
func TestTodo_CHATGATE_007_LeaveHold(t *testing.T) {
	store, service, c := chatgateFixture(t)
	c.Actor.Person = "worker"
	c.Key = "submit"
	if _, e := service.Submit(t.Context(), c, "1.0.0", map[string]json.RawMessage{"intro": json.RawMessage(`"Hello"`)}); e != nil {
		t.Fatal(e)
	}
	e := store.RunTenantTx(t.Context(), "tenant-a", func(tx dbport.Tx) error {
		if _, e := tx.Exec(t.Context(), `UPDATE chat_gate_answer SET legal_hold=true WHERE tenant_id='tenant-a' AND person_id='worker'`); e != nil {
			return e
		}
		if _, e := tx.Exec(t.Context(), `UPDATE chat_membership SET state='left' WHERE tenant_id='tenant-a' AND member_id='worker'`); e != nil {
			return e
		}
		var held int
		if e := tx.QueryRow(t.Context(), `SELECT count(*) FROM chat_gate_answer WHERE tenant_id='tenant-a' AND legal_hold`).Scan(&held); e != nil {
			return e
		}
		if held != 1 {
			t.Fatalf("held answers=%d", held)
		}
		var protected int
		if e := tx.QueryRow(t.Context(), `SELECT count(*) FROM pg_class WHERE relnamespace=current_schema()::regnamespace AND relname LIKE 'chat_gate_%' AND relrowsecurity AND relforcerowsecurity`).Scan(&protected); e != nil {
			return e
		}
		if protected != 6 {
			t.Fatalf("RLS tables=%d want 6", protected)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}
