package chatgate

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestTodo_CHATGATE_003_Extension(t *testing.T) {
	s, c, d := fixture(t, "automatic")
	kind := Kind{ID: "office_code", Version: "1.0.0", Schema: `{"type":"string"}`, DataClass: "INTERNAL", Names: map[string]string{"en-US": "Office", "de-DE": "Büro", "ar": "مكتب"}, Validate: func(_ Field, v json.RawMessage) error {
		if string(v) != `"Denver"` {
			return ErrInvalid
		}
		return nil
	}, Render: func(f Field, locale string) Control { return Control{Type: "short_text", Name: f.Label} }}
	if e := s.Registry.RegisterKind(kind); e != nil {
		t.Fatal(e)
	}
	d.Fields[0].Kind = kind.ID
	d.Fields[0].Options = nil
	c.Key = "extension"
	if e := s.Define(t.Context(), c, d); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision++
	c.Key = "extension-publish"
	if _, e := s.Publish(t.Context(), c, Version{2, 0, 0}); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision++
	if controls := s.RenderControls(d, "ar"); controls["team"].Type != "short_text" {
		t.Fatal(controls)
	}
	ac := applicant(c, "extension-answer")
	if _, e := s.Submit(t.Context(), ac, "2.0.0", map[string]json.RawMessage{"team": json.RawMessage(`"Denver"`)}); e != nil {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_005(t *testing.T) {
	s, c, _ := fixture(t, "review")
	ac := applicant(c, "draft")
	if e := s.SaveDraft(t.Context(), ac, "1.0.0", map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)}); e != nil {
		t.Fatal(e)
	}
	draft, e := s.DraftAnswers(t.Context(), ac.Actor, ac.Scope)
	if e != nil || string(draft["team"]) != `"Payroll"` {
		t.Fatal(draft, e)
	}
	sub, e := s.MySubmission(t.Context(), ac.Actor, ac.Scope)
	if e != nil || sub != nil {
		t.Fatal("draft became submission", sub, e)
	}
	ac.ExpectedRevision++
	ac.Key = "submit"
	if _, e = s.Submit(t.Context(), ac, "1.0.0", draft); e != nil {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_006(t *testing.T) {
	s, c, d := fixture(t, "review")
	outcome, e := s.Try(t.Context(), c.Actor, c.Scope, d, map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)})
	if e != nil || outcome != "review" {
		t.Fatal(outcome, e)
	}
	list, e := s.ListSubmissions(t.Context(), c.Actor, c.Scope, "")
	if e != nil || len(list) != 0 {
		t.Fatal("preview mutated submissions")
	}
	c.Actor.Person = "worker"
	if _, e = s.Try(t.Context(), c.Actor, c.Scope, d, map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)}); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_004_Override(t *testing.T) {
	s, c, _ := fixture(t, "review")
	c.Key = "override"
	if e := s.Override(t.Context(), c, "new-member", "Approved invitation"); e != nil {
		t.Fatal(e)
	}
	if !s.Repository.(*MemoryRepository).Member(c.Scope, "new-member") {
		t.Fatal("override lost membership")
	}
	if e := s.CanJoin(t.Context(), Actor{"tenant", "new-member"}, c.Scope); e != nil {
		t.Fatal(e)
	}
	c.Key = "unauthorized"
	c.ExpectedRevision++
	c.Actor.Person = "member"
	if e := s.Override(t.Context(), c, "other", "Coerced"); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_007_HoldRetention(t *testing.T) {
	s, c, _ := fixture(t, "automatic")
	ac := applicant(c, "submit")
	sub, e := s.Submit(t.Context(), ac, "1.0.0", map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Repository.Transact(t.Context(), c.Scope, func(tx Transaction) error { (*tx.Answers())[0].Held = true; return nil }); e != nil {
		t.Fatal(e)
	}
	held, e := s.HasHold(t.Context(), ac.Actor, c.Scope)
	if e != nil || !held {
		t.Fatal(held, e)
	}
	ac.Key = "withdraw"
	ac.ExpectedRevision++
	if e = s.Withdraw(t.Context(), ac, sub.ID, 1); e != nil {
		t.Fatal(e)
	}
	if e = s.Repository.Transact(t.Context(), c.Scope, func(tx Transaction) error {
		if len(*tx.Answers()) != 1 {
			t.Fatal("hold erased")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReadAnswers(t.Context(), ReadRequest{Actor: ac.Actor, Scope: c.Scope, Person: ac.Actor.Person, Purpose: "Own answers"}); !errors.Is(e, ErrNotFound) {
		t.Fatal("held data readable after withdrawal", e)
	}
}
