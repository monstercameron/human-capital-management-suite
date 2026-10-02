package chatgate

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestTodo_CHATGATE_006_ExportOwnRow: an export never includes a field that is
// not marked for administrators, also in the row of the administrator who
// exports. Reading one's own answers on the page still shows them all.
func TestTodo_CHATGATE_006_ExportOwnRow(t *testing.T) {
	s, c, d := fixture(t, "automatic")
	// A second question whose answer is for one consumer only.
	d.Fields = append(d.Fields, Field{ID: "badge", Kind: "short_text", KindVersion: "1.0.0", Label: "Badge number", Purpose: "Door access", DataClass: "INTERNAL", Visibility: Visibility{Consumers: []string{"door-access"}}, RetentionDays: 30})
	c.Key = "define-2"
	if e := s.Define(t.Context(), c, d); e != nil {
		t.Fatal(e)
	}
	c.Key, c.ExpectedRevision = "publish-2", c.ExpectedRevision+1
	if _, e := s.Publish(t.Context(), c, Version{1, 1, 0}); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision++
	answers := map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`), "badge": json.RawMessage(`"B-4411"`)}
	// The administrator answers the gate too, and so does a member.
	own := c
	own.Key = "admin-submit"
	if _, e := s.Submit(t.Context(), own, "1.1.0", answers); e != nil {
		t.Fatal(e)
	}
	c.ExpectedRevision++
	if _, e := s.Submit(t.Context(), applicant(c, "member-submit"), "1.1.0", answers); e != nil {
		t.Fatal(e)
	}

	all, e := s.ExportAllCSV(t.Context(), c.Actor, c.Scope)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(all, "B-4411") || strings.Contains(all, "badge") {
		t.Fatalf("the export of all answers carries a field marked for one consumer only: %s", all)
	}
	if !strings.Contains(all, "admin,team,Payroll") || !strings.Contains(all, "member,team,Payroll") {
		t.Fatalf("the export lost the fields administrators may see: %s", all)
	}
	one, e := s.ExportCSV(t.Context(), ReadRequest{Actor: c.Actor, Scope: c.Scope, Person: "admin", Purpose: "Export", Export: true})
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(one, "B-4411") || !strings.Contains(one, "team,Payroll") {
		t.Fatalf("the administrator's own exported row carries the consumer-only field: %s", one)
	}

	// On the page a person still reads all of their own answers.
	mine, e := s.ReadAnswers(t.Context(), ReadRequest{Actor: c.Actor, Scope: c.Scope, Person: "admin", Purpose: "My answers"})
	if e != nil || string(mine["badge"]) != `"B-4411"` || string(mine["team"]) != `"Payroll"` {
		t.Fatalf("a person does not read their own answers: %v %v", mine, e)
	}
	// And an administrator reading someone else sees the administrators' fields only.
	theirs, e := s.ReadAnswers(t.Context(), ReadRequest{Actor: c.Actor, Scope: c.Scope, Person: "member", Purpose: "Gate answers view"})
	if e != nil || len(theirs) != 1 || string(theirs["team"]) != `"Payroll"` {
		t.Fatalf("an administrator reads a member's answers as %v %v", theirs, e)
	}
}
