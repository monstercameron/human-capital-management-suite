package chatgate

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestTodo_CHATGATE_006_Bulk(t *testing.T) {
	s, c, _ := fixture(t, "review")
	decisions := []ReviewDecision{}
	for _, person := range []string{"one", "two"} {
		ac := applicant(c, "submit-"+person)
		ac.Actor.Person = person
		sub, e := s.Submit(t.Context(), ac, "1.0.0", map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)})
		if e != nil {
			t.Fatal(e)
		}
		c.ExpectedRevision++
		decisions = append(decisions, ReviewDecision{SubmissionID: sub.ID, Revision: sub.Revision, Admit: true})
	}
	c.Key = "bulk"
	if e := s.ReviewMany(t.Context(), c, decisions, "Welcome"); e != nil {
		t.Fatal(e)
	}
	if !s.Repository.(*MemoryRepository).Member(c.Scope, "one") || !s.Repository.(*MemoryRepository).Member(c.Scope, "two") {
		t.Fatal("bulk admission incomplete")
	}
	csv, e := s.ExportAllCSV(t.Context(), c.Actor, c.Scope)
	if e != nil || !strings.Contains(csv, "one,team,Payroll") || !strings.Contains(csv, "two,team,Payroll") {
		t.Fatal(csv, e)
	}
	events := map[string]bool{}
	if e = s.Repository.Transact(t.Context(), c.Scope, func(tx Transaction) error {
		for _, event := range tx.State().Events {
			if events[event.ID] {
				t.Fatal("duplicate event identity")
			}
			events[event.ID] = true
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_006_BulkSecurity(t *testing.T) {
	s, c, _ := fixture(t, "review")
	ac := applicant(c, "submit")
	sub, e := s.Submit(t.Context(), ac, "1.0.0", map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)})
	if e != nil {
		t.Fatal(e)
	}
	c.Key = "bad-bulk"
	c.ExpectedRevision++
	decisions := []ReviewDecision{{SubmissionID: sub.ID, Revision: sub.Revision, Admit: true}, {SubmissionID: "forged", Revision: 1, Admit: true}}
	if e = s.ReviewMany(t.Context(), c, decisions, "Welcome"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s.Repository.(*MemoryRepository).Member(c.Scope, "member") {
		t.Fatal("bulk committed before validation")
	}
	values, e := s.ReadAnswers(t.Context(), ReadRequest{Actor: ac.Actor, Scope: c.Scope, Person: "member", Purpose: "Check"})
	if e != nil || len(values) != 1 {
		t.Fatal(values, e)
	}
	s.Authority.(*fixtureAuthority).deny["read_answers"] = true
	if _, e = s.ReadAnswers(t.Context(), ReadRequest{Actor: ac.Actor, Scope: c.Scope, Person: "member", Purpose: "Former member"}); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_003_FieldError(t *testing.T) {
	s, c, _ := fixture(t, "automatic")
	_, e := s.Submit(t.Context(), applicant(c, "missing"), "1.0.0", nil)
	var field FieldError
	if !errors.As(e, &field) || field.Field != "team" || !errors.Is(e, ErrRequired) || !strings.Contains(e.Error(), "team") {
		t.Fatal(e)
	}
}
func TestTodo_CHATGATE_002_Bumps(t *testing.T) {
	_, _, base := fixture(t, "automatic")
	base.Version = Version{1, 2, 3}
	for _, test := range []struct {
		name string
		edit func(*Definition)
		want Version
	}{{"wording", func(d *Definition) { d.Fields[0].Label = "Your team" }, Version{1, 2, 4}}, {"kind", func(d *Definition) { d.Fields[0].Kind = "short_text" }, Version{2, 0, 0}}, {"optional option", func(d *Definition) { d.Fields[0].Options = append(d.Fields[0].Options, "People") }, Version{1, 3, 0}}, {"removed option", func(d *Definition) { d.Fields[0].Options = d.Fields[0].Options[:1] }, Version{2, 0, 0}}, {"required", func(d *Definition) { d.Fields = append(d.Fields, Field{ID: "new", Required: true}) }, Version{2, 0, 0}}} {
		t.Run(test.name, func(t *testing.T) {
			b, _ := json.Marshal(base)
			var next Definition
			_ = json.Unmarshal(b, &next)
			test.edit(&next)
			if bump := RequiredBump(base, next); bump != test.want {
				t.Fatal(bump, test.want)
			}
		})
	}
}
