package chatgate

import (
	"encoding/json"
	"testing"
)

// TestIssue3824_ReadsListsOnlyOtherPeople: the applicant's "who has seen my
// answers" list holds reads by other people, never the applicant's own.
func TestIssue3824_ReadsListsOnlyOtherPeople(t *testing.T) {
	s, c, _ := fixture(t, "review")
	ac := applicant(c, "submit")
	if _, e := s.Submit(t.Context(), ac, "1.0.0", map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DraftAnswers(t.Context(), ac.Actor, c.Scope); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ReadAnswers(t.Context(), ReadRequest{Actor: ac.Actor, Scope: c.Scope, Person: ac.Actor.Person, Purpose: "My answers"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ReadAnswers(t.Context(), ReadRequest{Actor: c.Actor, Scope: c.Scope, Person: ac.Actor.Person, Purpose: "Gate answers view"}); e != nil {
		t.Fatal(e)
	}
	reads, e := s.Reads(t.Context(), ac.Actor, c.Scope)
	if e != nil {
		t.Fatal(e)
	}
	if len(reads) != 1 || reads[0].Reader != c.Actor.Person {
		t.Fatalf("the list holds %+v, want only the administrator's read", reads)
	}
}
