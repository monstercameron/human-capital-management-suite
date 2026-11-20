package chatgate

import (
	"encoding/json"
	"errors"
	"testing"
)

// Issue 3819: a decision without a reason names the reason field, so the page can
// tie the sentence to that input instead of showing applicant copy.
func TestIssue3819_BlankReasonNamesTheReasonField(t *testing.T) {
	s, c, _ := fixture(t, "review")
	sub, e := s.Submit(t.Context(), applicant(c, "submit"), "1.0.0", map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)})
	if e != nil {
		t.Fatal(e)
	}
	c.Key = "no-reason"
	c.ExpectedRevision++
	_, e = s.Review(t.Context(), c, sub.ID, sub.Revision, true, "  ")
	var field FieldError
	if !errors.Is(e, ErrInvalid) || !errors.As(e, &field) || field.Field != "reason" {
		t.Fatalf("single decision: %v", e)
	}
	c.Key = "no-reason-bulk"
	e = s.ReviewMany(t.Context(), c, []ReviewDecision{{SubmissionID: sub.ID, Revision: sub.Revision, Admit: true}}, "")
	field = FieldError{}
	if !errors.Is(e, ErrInvalid) || !errors.As(e, &field) || field.Field != "reason" {
		t.Fatalf("bulk decision: %v", e)
	}
}
