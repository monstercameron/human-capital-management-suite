package chatgate

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type recordedNotice struct{ ReviewNotice }

type recordingNotifier struct {
	got  []recordedNotice
	fail error
}

func (r *recordingNotifier) NotifyReview(_ context.Context, n ReviewNotice) error {
	if r.fail != nil {
		return r.fail
	}
	r.got = append(r.got, recordedNotice{n})
	return nil
}

func (r *recordingNotifier) levels() []string {
	out := []string{}
	for _, n := range r.got {
		out = append(out, n.Level)
	}
	return out
}

func answer(office string) map[string]json.RawMessage {
	return map[string]json.RawMessage{"office": json.RawMessage(`"` + office + `"`)}
}

// TestIssue3817_SubmitTellsReviewers: a request that waits for a person to
// decide it tells the channel's reviewers, once, and carries no answer text.
func TestIssue3817_SubmitTellsReviewers(t *testing.T) {
	e := newSegEnv(t, true)
	n := &recordingNotifier{}
	e.s.Notifier = n
	sub, err := e.s.Submit(t.Context(), e.cmd("ana", "a"), "1.0.0", answer("Denver"))
	if err != nil || sub.Status != "review" {
		t.Fatalf("submit: %+v %v", sub, err)
	}
	if len(n.got) != 1 || n.got[0].Level != ReviewNoticeNew || n.got[0].SubmissionID != sub.ID || n.got[0].Person != "ana" || n.got[0].Scope != e.scope {
		t.Fatalf("notices: %+v", n.got)
	}
}

// An automatic gate admits at once: nobody has a request to read.
func TestIssue3817_AutomaticGateTellsNobody(t *testing.T) {
	e := newSegEnv(t, false)
	n := &recordingNotifier{}
	e.s.Notifier = n
	if _, err := e.s.Submit(t.Context(), e.cmd("ana", "a"), "1.0.0", answer("Denver")); err != nil {
		t.Fatal(err)
	}
	if len(n.got) != 0 {
		t.Fatalf("an admitted person raised notices: %+v", n.got)
	}
}

// A failed notice never fails the person's request.
func TestIssue3817_SubmitSurvivesNotifierFailure(t *testing.T) {
	e := newSegEnv(t, true)
	e.s.Notifier = &recordingNotifier{fail: errors.New("down")}
	sub, err := e.s.Submit(t.Context(), e.cmd("ana", "a"), "1.0.0", answer("Denver"))
	if err != nil || sub.Status != "review" {
		t.Fatalf("a failed notice failed the request: %+v %v", sub, err)
	}
}

// TestIssue3817_SweepTellsAfterDeadlines: the overdue notice and the escalation
// notice are each sent once, to the right level.
func TestIssue3817_SweepTellsAfterDeadlines(t *testing.T) {
	e := newSegEnv(t, true)
	n := &recordingNotifier{}
	e.s.Notifier = n
	e.must(e.s.SetQueueDeadlines(t.Context(), e.cmd("admin", "sla"), 24*time.Hour, 72*time.Hour))
	if _, err := e.s.Submit(t.Context(), e.cmd("ana", "a"), "1.0.0", answer("Denver")); err != nil {
		t.Fatal(err)
	}
	admin := Actor{"tenant", "admin"}
	e.now = e.now.Add(25 * time.Hour)
	if o, x, err := e.s.SweepQueue(t.Context(), admin, e.scope); err != nil || o != 1 || x != 0 {
		t.Fatalf("sweep: %d %d %v", o, x, err)
	}
	if _, _, err := e.s.SweepQueue(t.Context(), admin, e.scope); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(48 * time.Hour)
	if o, x, err := e.s.SweepQueue(t.Context(), admin, e.scope); err != nil || o != 0 || x != 1 {
		t.Fatalf("second sweep: %d %d %v", o, x, err)
	}
	want := []string{ReviewNoticeNew, ReviewNoticeOverdue, ReviewNoticeEscalated}
	got := n.levels()
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("levels %v, want %v", got, want)
	}
}

// If the notice cannot be sent the request is not marked as told, so the next
// sweep tries again instead of losing it.
func TestIssue3817_SweepRetriesAFailedNotice(t *testing.T) {
	e := newSegEnv(t, true)
	n := &recordingNotifier{}
	e.s.Notifier = n
	e.must(e.s.SetQueueDeadlines(t.Context(), e.cmd("admin", "sla"), 24*time.Hour, 72*time.Hour))
	if _, err := e.s.Submit(t.Context(), e.cmd("ana", "a"), "1.0.0", answer("Denver")); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(25 * time.Hour)
	n.fail = errors.New("down")
	if _, _, err := e.s.SweepQueue(t.Context(), Actor{"tenant", "admin"}, e.scope); err == nil {
		t.Fatal("a failed notice was swallowed")
	}
	n.fail = nil
	if o, _, err := e.s.SweepQueue(t.Context(), Actor{"tenant", "admin"}, e.scope); err != nil || o != 1 {
		t.Fatalf("retry: %d %v", o, err)
	}
	if got := n.levels(); len(got) != 2 || got[1] != ReviewNoticeOverdue {
		t.Fatalf("levels %v", got)
	}
}
