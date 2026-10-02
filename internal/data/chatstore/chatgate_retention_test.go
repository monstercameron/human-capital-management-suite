package chatstore

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_CHATGATE_007_Security(t *testing.T) {
	store, service, c := chatgateFixture(t)
	c.Actor.Person = "worker"
	c.Key = "submit"
	if _, e := service.Submit(t.Context(), c, "1.0.0", map[string]json.RawMessage{"intro": json.RawMessage(`"Hello"`)}); e != nil {
		t.Fatal(e)
	}
	if e := store.RunTenantTx(t.Context(), "tenant-a", func(tx dbport.Tx) error {
		_, e := tx.Exec(t.Context(), `UPDATE chat_membership SET state='removed' WHERE tenant_id='tenant-a' AND member_id='worker'`)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if e := service.CanJoin(t.Context(), c.Actor, c.Scope); !errors.Is(e, chatgate.ErrRequired) {
		t.Fatal("old grant survived removal", e)
	}
	if _, e := service.ReadAnswers(t.Context(), chatgate.ReadRequest{Actor: c.Actor, Scope: c.Scope, Person: "worker", Purpose: "Former member"}); !errors.Is(e, chatgate.ErrNotFound) {
		t.Fatal("former member read", e)
	}
}
func TestTodo_CHATGATE_007_Property(t *testing.T) {
	store, service, c := chatgateFixture(t)
	c.Actor.Person = "worker"
	c.Key = "submit"
	if _, e := service.Submit(t.Context(), c, "1.0.0", map[string]json.RawMessage{"intro": json.RawMessage(`"Hello"`)}); e != nil {
		t.Fatal(e)
	}
	now := service.Clock()
	service.Clock = func() time.Time { return now.AddDate(0, 0, 31) }
	c.Key = "expire"
	c.ExpectedRevision++
	c.Actor.Person = "owner"
	if e := service.Erase(t.Context(), c, "worker", true); e != nil {
		t.Fatal(e)
	}
	if e := store.RunTenantTx(t.Context(), "tenant-a", func(tx dbport.Tx) error {
		var n int
		if e := tx.QueryRow(t.Context(), `SELECT count(*) FROM chat_gate_answer WHERE tenant_id='tenant-a' AND person_id='worker'`).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			return errors.New("expired answer retained")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
