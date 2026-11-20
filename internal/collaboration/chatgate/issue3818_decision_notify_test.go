package chatgate

import (
	"context"
	"errors"
	"testing"
)

type recordingDecisions struct {
	got  []DecisionNotice
	fail error
}

func (r *recordingDecisions) NotifyDecision(_ context.Context, n DecisionNotice) error {
	if r.fail != nil {
		return r.fail
	}
	r.got = append(r.got, n)
	return nil
}

// TestIssue3818_ReviewTellsApplicant: an administrator's decision on a waiting
// request tells the applicant, once, with the reason they were given.
func TestIssue3818_ReviewTellsApplicant(t *testing.T) {
	for _, admit := range []bool{true, false} {
		e := newSegEnv(t, true)
		d := &recordingDecisions{}
		e.s.Decisions = d
		sub, err := e.s.Submit(t.Context(), e.cmd("ana", "a"), "1.0.0", answer("Denver"))
		if err != nil {
			t.Fatal(err)
		}
		decided, err := e.s.Review(t.Context(), e.cmd("admin", "r"), sub.ID, sub.Revision, admit, "Team is full")
		if err != nil {
			t.Fatal(err)
		}
		if len(d.got) != 1 {
			t.Fatalf("admit=%v: notices %+v", admit, d.got)
		}
		n := d.got[0]
		if n.Person != "ana" || n.Admitted != admit || n.SubmissionID != sub.ID || n.Revision != decided.Revision || n.Reason != "Team is full" || n.Scope != e.scope {
			t.Fatalf("admit=%v: notice %+v", admit, n)
		}
	}
}

// TestIssue3818_ReviewManyTellsEachApplicant: a bulk decision tells every
// applicant in it.
func TestIssue3818_ReviewManyTellsEachApplicant(t *testing.T) {
	e := newSegEnv(t, true)
	d := &recordingDecisions{}
	e.s.Decisions = d
	var decisions []ReviewDecision
	for _, p := range []string{"ana", "bo"} {
		sub, err := e.s.Submit(t.Context(), e.cmd(p, p), "1.0.0", answer("Denver"))
		if err != nil {
			t.Fatal(err)
		}
		decisions = append(decisions, ReviewDecision{SubmissionID: sub.ID, Revision: sub.Revision, Admit: p == "ana"})
	}
	if err := e.s.ReviewMany(t.Context(), e.cmd("admin", "bulk"), decisions, "Batch"); err != nil {
		t.Fatal(err)
	}
	if len(d.got) != 2 || d.got[0].Person != "ana" || !d.got[0].Admitted || d.got[1].Person != "bo" || d.got[1].Admitted {
		t.Fatalf("notices %+v", d.got)
	}
}

// A refused decision and an automatic admission tell nobody; a failed notice
// never undoes the decision.
func TestIssue3818_DecisionNoticeRules(t *testing.T) {
	e := newSegEnv(t, true)
	d := &recordingDecisions{fail: errors.New("down")}
	e.s.Decisions = d
	sub, err := e.s.Submit(t.Context(), e.cmd("ana", "a"), "1.0.0", answer("Denver"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.s.Review(t.Context(), e.cmd("ana", "self"), sub.ID, sub.Revision, true, "me"); err == nil {
		t.Fatal("an applicant decided their own request")
	}
	got, err := e.s.Review(t.Context(), e.cmd("admin", "r"), sub.ID, sub.Revision, true, "ok")
	if err != nil || got.Status != "admitted" {
		t.Fatalf("a failed notice undid the decision: %+v %v", got, err)
	}
	auto := newSegEnv(t, false)
	d2 := &recordingDecisions{}
	auto.s.Decisions = d2
	if _, err := auto.s.Submit(t.Context(), auto.cmd("ana", "a"), "1.0.0", answer("Denver")); err != nil || len(d2.got) != 0 {
		t.Fatalf("automatic admission raised %+v %v", d2.got, err)
	}
}
