package chatgate

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fixtureDelivery struct{ envelopes []ConsumerEnvelope }

func (d *fixtureDelivery) DeliverSignedGateEvent(_ context.Context, e ConsumerEnvelope) error {
	d.envelopes = append(d.envelopes, e)
	return nil
}
func TestTodo_CHATGATE_008_Events(t *testing.T) {
	s, c, d := fixture(t, "automatic")
	if e := s.Registry.RegisterConsumer(Consumer{ID: "welcome", Version: "1.0.0", Kinds: []string{"single_choice"}, Effect: "welcome", Permission: "install"}); e != nil {
		t.Fatal(e)
	}
	d.Fields[0].Visibility.Consumers = []string{"welcome"}
	c.Key = "form"
	if e := s.Define(t.Context(), c, d); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision++
	c.Key = "publish2"
	if _, e := s.Publish(t.Context(), c, Version{2, 0, 0}); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision++
	c.Key = "install"
	if e := s.Install(t.Context(), c, Installation{ConsumerID: "welcome", Version: "1.0.0", Mapping: map[string]string{"team": "team"}}); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision++
	ac := applicant(c, "submit")
	if _, e := s.Submit(t.Context(), ac, "2.0.0", map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)}); e != nil {
		t.Fatal(e)
	}
	var id string
	if e := s.Repository.Transact(t.Context(), c.Scope, func(tx Transaction) error {
		for _, event := range tx.State().Events {
			if event.Kind == "admitted" {
				id = event.ID
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	delivery := &fixtureDelivery{}
	actor := Actor{"tenant", "installed-agent"}
	if e := s.DeliverEvent(t.Context(), actor, c.Scope, id, "welcome", "1.0.0", delivery); e != nil {
		t.Fatal(e)
	}
	if len(delivery.envelopes) != 1 || string(delivery.envelopes[0].Values["team"]) != `"Payroll"` {
		t.Fatal(delivery)
	}
	if e := s.DeliverEvent(t.Context(), actor, c.Scope, id, "unknown", "1.0.0", delivery); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e := s.DeliverEvent(t.Context(), actor, c.Scope, "forged", "welcome", "1.0.0", delivery); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if e := s.DeliverEvent(t.Context(), actor, c.Scope, id, "welcome", "1.0.0", UnavailableConsumerDelivery{}); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_008_AdditiveWire(t *testing.T) {
	var d Definition
	if e := json.Unmarshal([]byte(`{"Mode":"review","FutureControl":{"enabled":true}}`), &d); e != nil {
		t.Fatal(e)
	}
	if string(d.Extensions["FutureControl"]) != `{"enabled":true}` {
		t.Fatal(d)
	}
	b, e := json.Marshal(d)
	if e != nil || !strings.Contains(string(b), `"FutureControl":{"enabled":true}`) {
		t.Fatal(string(b), e)
	}
	if e = json.Unmarshal([]byte(`{"mode":"review","future":true}`), &d); e != nil {
		t.Fatal(e)
	}
	d.Mode = "automatic"
	d.Extensions["mode"] = json.RawMessage(`"forged"`)
	b, e = json.Marshal(d)
	if e != nil || strings.Contains(string(b), "forged") || !strings.Contains(string(b), `"Mode":"automatic"`) {
		t.Fatal(string(b), e)
	}
}
func TestTodo_CHATGATE_007_Inventory(t *testing.T) {
	s, c, _ := fixture(t, "automatic")
	rows, e := s.Inventory(t.Context(), c.Actor)
	if e != nil || len(rows) != 1 || rows[0].Fields[0].RetentionDays != 30 || rows[0].Scope.Conversation != "room" {
		t.Fatal(rows, e)
	}
	fields, e := s.SearchGateQuestions(t.Context(), c.Actor, c.Scope, "Team")
	if e != nil || len(fields) != 1 {
		t.Fatal(fields, e)
	}
	c.Actor.Tenant = "other"
	rows, e = s.Inventory(t.Context(), c.Actor)
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
}
func TestTodo_CHATGATE_006_Export(t *testing.T) {
	s, c, _ := fixture(t, "automatic")
	ac := applicant(c, "submit")
	if _, e := s.Submit(t.Context(), ac, "1.0.0", map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)}); e != nil {
		t.Fatal(e)
	}
	csv, e := s.ExportCSV(t.Context(), ReadRequest{Actor: c.Actor, Scope: c.Scope, Person: "member", Purpose: "Export"})
	if e != nil || !strings.HasPrefix(csv, "field,answer\nteam,") {
		t.Fatal(csv, e)
	}
	reads, e := s.Reads(t.Context(), ac.Actor, c.Scope)
	if e != nil || len(reads) != 1 || !reads[0].Export {
		t.Fatal(reads, e)
	}
	if s.CanExport(t.Context(), ac.Actor, c.Scope) {
		t.Fatal("unprivileged export")
	}
}
func TestTodo_CHATGATE_004_Decline(t *testing.T) {
	s, c, _ := fixture(t, "review")
	ac := applicant(c, "submit")
	sub, e := s.Submit(t.Context(), ac, "1.0.0", map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)})
	if e != nil {
		t.Fatal(e)
	}
	c.Key = "decline"
	c.ExpectedRevision++
	declined, e := s.Review(t.Context(), c, sub.ID, sub.Revision, false, "Not eligible yet")
	if e != nil || declined.Status != "declined" || declined.Reason != "Not eligible yet" || s.Repository.(*MemoryRepository).Member(c.Scope, "member") {
		t.Fatal(declined, e)
	}
	c.ExpectedRevision++
	c.Key = "replay"
	if _, e = s.Review(t.Context(), c, sub.ID, sub.Revision, true, "Forged replay"); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
}
