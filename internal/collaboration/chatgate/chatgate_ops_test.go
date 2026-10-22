package chatgate

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

func (e *segEnv) tryJoin(person string, n int) error {
	_, err := e.s.Submit(e.t.Context(), e.cmd(person, fmt.Sprintf("try-%s-%d", person, n)), "1.0.0", map[string]json.RawMessage{"office": json.RawMessage(`"Denver"`)})
	return err
}

// TestTodo_CHATGATE_013: submissions are limited per person and per channel, a
// person who is declined over and over waits, an administrator reads metrics
// that name nobody, the review queue raises its alarms once each, and current
// members are asked only if the administrator chooses it.
func TestTodo_CHATGATE_013(t *testing.T) {
	e := newSegEnv(t, false)
	// Per person: five a minute, then a limit that lifts with the minute.
	for i := 0; i < MaxSubmissionsPerPersonPerMinute; i++ {
		if err := e.tryJoin("ana", i); err != nil {
			t.Fatalf("submission %d: %v", i, err)
		}
	}
	if err := e.tryJoin("ana", 99); !errors.Is(err, ErrLimited) {
		t.Fatalf("the sixth submission in a minute: %v", err)
	}
	if err := e.tryJoin("ben", 0); err != nil {
		t.Fatalf("another person was limited by ana: %v", err)
	}
	e.now = e.now.Add(61 * time.Second)
	if err := e.tryJoin("ana", 100); err != nil {
		t.Fatalf("after the minute: %v", err)
	}

	// Per channel.
	c := newSegEnv(t, false)
	for i := 0; i < MaxSubmissionsPerChannelPerMinute; i++ {
		if err := c.tryJoin(fmt.Sprintf("p%03d", i), 0); err != nil {
			t.Fatalf("person %d: %v", i, err)
		}
	}
	if err := c.tryJoin("one-too-many", 0); !errors.Is(err, ErrLimited) {
		t.Fatalf("the channel's limit: %v", err)
	}

	// Abuse: three declines in a day and the fourth request waits.
	r := newSegEnv(t, true)
	for i := 0; i < MaxDeclinesPerDay; i++ {
		r.now = r.now.Add(2 * time.Minute)
		sub, err := r.s.Submit(t.Context(), r.cmd("troll", fmt.Sprintf("t%d", i)), "1.0.0", map[string]json.RawMessage{"office": json.RawMessage(`"Denver"`)})
		if err != nil {
			t.Fatalf("troll %d: %v", i, err)
		}
		if _, err = r.s.Review(t.Context(), r.cmd("admin", fmt.Sprintf("d%d", i)), sub.ID, sub.Revision, false, "Not this channel"); err != nil {
			t.Fatal(err)
		}
	}
	r.now = r.now.Add(2 * time.Minute)
	if err := r.tryJoin("troll", 9); !errors.Is(err, ErrLimited) {
		t.Fatalf("a person declined three times today: %v", err)
	}
	r.now = r.now.Add(25 * time.Hour)
	if err := r.tryJoin("troll", 10); err != nil {
		t.Fatalf("the next day: %v", err)
	}

	// Metrics: counts and timings, no names, drop-off per question.
	m := newSegEnv(t, true)
	m.must(m.s.SaveDraft(t.Context(), m.cmd("drafty", "draft"), "1.0.0", map[string]json.RawMessage{"office": json.RawMessage(`"Austin"`)}))
	a, _ := m.s.Submit(t.Context(), m.cmd("ana", "a"), "1.0.0", map[string]json.RawMessage{"office": json.RawMessage(`"Denver"`), "shift": json.RawMessage(`"Day"`)})
	b, _ := m.s.Submit(t.Context(), m.cmd("ben", "b"), "1.0.0", map[string]json.RawMessage{"office": json.RawMessage(`"Austin"`)})
	m.now = m.now.Add(10 * time.Minute)
	if _, err := m.s.Review(t.Context(), m.cmd("admin", "ra"), a.ID, a.Revision, true, "ok"); err != nil {
		t.Fatal(err)
	}
	m.now = m.now.Add(20 * time.Minute)
	if _, err := m.s.Review(t.Context(), m.cmd("admin", "rb"), b.ID, b.Revision, false, "no"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.s.Submit(t.Context(), m.cmd("cy", "c"), "1.0.0", map[string]json.RawMessage{"office": json.RawMessage(`"Denver"`)}); err != nil {
		t.Fatal(err)
	}
	got, err := m.s.Metrics(t.Context(), Actor{"tenant", "admin"}, m.scope)
	if err != nil || got.Started != 4 || got.Submitted != 3 || got.Admitted != 1 || got.Declined != 1 || got.Waiting != 1 || got.MedianDecision != 20*time.Minute {
		t.Fatalf("metrics: %+v %v", got, err)
	}
	drop := map[string]int{}
	for _, q := range got.DropOff {
		drop[q.Field] = q.Dropped
	}
	if drop["office"] != 0 || drop["shift"] != 3 {
		t.Fatalf("drop-off per question: %+v", got.DropOff)
	}
	if _, err = m.s.Metrics(t.Context(), Actor{"tenant", "ben"}, m.scope); !errors.Is(err, ErrDenied) {
		t.Fatalf("a member read the metrics: %v", err)
	}

	// The queue: administrators first, then the workspace administrator, once.
	q := newSegEnv(t, true)
	q.must(q.s.SetQueueDeadlines(t.Context(), q.cmd("admin", "sla"), 24*time.Hour, 72*time.Hour))
	if _, err := q.s.Submit(t.Context(), q.cmd("ana", "a"), "1.0.0", map[string]json.RawMessage{"office": json.RawMessage(`"Denver"`)}); err != nil {
		t.Fatal(err)
	}
	admin := Actor{"tenant", "admin"}
	if o, x, _ := q.s.SweepQueue(t.Context(), admin, q.scope); o != 0 || x != 0 {
		t.Fatalf("a fresh request raised %d, %d", o, x)
	}
	q.now = q.now.Add(25 * time.Hour)
	if o, x, _ := q.s.SweepQueue(t.Context(), admin, q.scope); o != 1 || x != 0 {
		t.Fatalf("after a day: %d, %d", o, x)
	}
	if o, x, _ := q.s.SweepQueue(t.Context(), admin, q.scope); o != 0 || x != 0 {
		t.Fatalf("a second sweep raised %d, %d", o, x)
	}
	q.now = q.now.Add(48 * time.Hour)
	if o, x, _ := q.s.SweepQueue(t.Context(), admin, q.scope); o != 0 || x != 1 {
		t.Fatalf("after three days: %d, %d", o, x)
	}
	kinds := map[string]int{}
	for _, ev := range q.state().Events {
		kinds[ev.Kind]++
	}
	if kinds["review_overdue"] != 1 || kinds["review_escalated"] != 1 {
		t.Fatalf("events: %v", kinds)
	}
	if err := q.s.SetQueueDeadlines(t.Context(), q.cmd("admin", "bad"), time.Minute, time.Hour); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a one-minute deadline: %v", err)
	}
}

// TestTodo_CHATGATE_013_Existing: adding a gate to a channel with members locks
// nobody out; "ask" gives members until a date, with one reminder a day, and
// only an administrator's confirmed list is ever removed.
func TestTodo_CHATGATE_013_Existing(t *testing.T) {
	e := newSegEnv(t, false)
	mem := e.s.Repository.(*MemoryRepository)
	mem.AddExisting(e.scope, "old1", "old2", "old3")
	// A person who was already in is not stopped by the gate when their
	// membership changes (a promotion, a re-add); a stranger is.
	if err := e.s.CanJoin(t.Context(), Actor{"tenant", "old1"}, e.scope); err != nil {
		t.Fatalf("an existing member was locked out: %v", err)
	}
	if err := e.s.CanJoin(t.Context(), Actor{"tenant", "new"}, e.scope); !errors.Is(err, ErrRequired) {
		t.Fatalf("a newcomer was not asked: %v", err)
	}
	// "Only new members": nobody is asked, nobody unanswered is removable.
	e.must(e.s.SetExistingMembers(t.Context(), e.cmd("admin", "new-only"), ExistingNewOnly, time.Time{}))
	if err := e.s.RemoveUnanswered(t.Context(), e.cmd("admin", "rm"), []string{"old1"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("removal under \"only new members\": %v", err)
	}
	// "Ask by a date".
	if err := e.s.SetExistingMembers(t.Context(), e.cmd("admin", "past"), ExistingAsk, e.now.Add(-time.Hour)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a date in the past: %v", err)
	}
	by := e.now.AddDate(0, 0, 14)
	e.must(e.s.SetExistingMembers(t.Context(), e.cmd("admin", "ask"), ExistingAsk, by))
	admin := Actor{"tenant", "admin"}
	list, err := e.s.Unanswered(t.Context(), admin, e.scope)
	if err != nil || len(list) != 3 {
		t.Fatalf("unanswered: %v %v", list, err)
	}
	// One reminder a day, to each, until the date.
	if n, _ := e.s.RemindUnanswered(t.Context(), admin, e.scope); n != 3 {
		t.Fatalf("first reminder reached %d", n)
	}
	if n, _ := e.s.RemindUnanswered(t.Context(), admin, e.scope); n != 0 {
		t.Fatalf("a second reminder the same day reached %d", n)
	}
	// old1 answers and is no longer on the list.
	sub, err := e.s.Submit(t.Context(), e.cmd("old1", "old1"), "1.0.0", map[string]json.RawMessage{"office": json.RawMessage(`"Denver"`)})
	if err != nil || sub.Status != "admitted" {
		t.Fatalf("old1: %+v %v", sub, err)
	}
	if list, _ = e.s.Unanswered(t.Context(), admin, e.scope); len(list) != 2 {
		t.Fatalf("after old1 answered: %v", list)
	}
	// Before the date nobody is removed, even from a confirmed list.
	if err = e.s.RemoveUnanswered(t.Context(), e.cmd("admin", "early"), []string{"old2"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("removal before the date: %v", err)
	}
	// The date passing removes nobody by itself.
	e.now = by.Add(time.Hour)
	if !mem.Member(e.scope, "old2") || !mem.Member(e.scope, "old3") {
		t.Fatal("the date passed and members were removed without a list")
	}
	// A list that includes somebody who answered, or a stranger, is refused whole.
	if err = e.s.RemoveUnanswered(t.Context(), e.cmd("admin", "stale"), []string{"old2", "old1"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("a stale list: %v", err)
	}
	if !mem.Member(e.scope, "old2") {
		t.Fatal("a refused list removed somebody")
	}
	// The confirmed list is removed, and only it.
	e.must(e.s.RemoveUnanswered(t.Context(), e.cmd("admin", "confirmed"), []string{"old2"}))
	if mem.Member(e.scope, "old2") || !mem.Member(e.scope, "old3") || !mem.Member(e.scope, "old1") {
		t.Fatal("the wrong people were removed")
	}
	// Reminders stop at the date.
	if n, _ := e.s.RemindUnanswered(t.Context(), admin, e.scope); n != 0 {
		t.Fatalf("a reminder after the date reached %d", n)
	}
}

// TestTodo_CHATGATE_013_Fault: with the membership path unavailable, then the
// outbox unavailable, a submission is never lost and never admits twice: the
// failed attempt leaves no trace, and the retry under the same key admits once.
func TestTodo_CHATGATE_013_Fault(t *testing.T) {
	for _, fault := range []string{"membership", "outbox"} {
		t.Run(fault, func(t *testing.T) {
			e := newSegEnv(t, false)
			mem := e.s.Repository.(*MemoryRepository)
			down := errors.New(fault + " unavailable")
			if fault == "membership" {
				mem.FailMembership = down
			} else {
				mem.FailOutbox = down
			}
			cmd := e.cmd("ana", "join-once")
			answers := map[string]json.RawMessage{"office": json.RawMessage(`"Denver"`)}
			if _, err := e.s.Submit(t.Context(), cmd, "1.0.0", answers); !errors.Is(err, down) {
				t.Fatalf("the fault was not reported: %v", err)
			}
			if mem.Member(e.scope, "ana") || len(e.state().Submissions) != 0 || len(*memAnswers(t, e)) != 0 {
				t.Fatal("a failed attempt left a submission, an answer or a member behind")
			}
			mem.FailMembership, mem.FailOutbox = nil, nil
			first, err := e.s.Submit(t.Context(), cmd, "1.0.0", answers)
			if err != nil || first.Status != "admitted" {
				t.Fatalf("the retry: %+v %v", first, err)
			}
			// A third try under the same key replays the first result.
			again, err := e.s.Submit(t.Context(), cmd, "1.0.0", answers)
			if err != nil || again.ID != first.ID {
				t.Fatalf("the replay: %+v %v", again, err)
			}
			admitted := 0
			for _, ev := range e.state().Events {
				if ev.Kind == "admitted" && ev.Person == "ana" {
					admitted++
				}
			}
			if admitted != 1 || len(e.state().Submissions) != 1 || !mem.Member(e.scope, "ana") {
				t.Fatalf("admitted %d times, %d submissions", admitted, len(e.state().Submissions))
			}
		})
	}
}

func memAnswers(t *testing.T, e *segEnv) *[]Answer {
	t.Helper()
	var out []Answer
	e.must(e.s.Repository.Transact(t.Context(), e.scope, func(tx Transaction) error { out = append(out, *tx.Answers()...); return nil }))
	return &out
}

// TestTodo_CHATGATE_013_Performance: a gate with 10,000 submissions lists and
// filters within 300 ms.
func TestTodo_CHATGATE_013_Performance(t *testing.T) {
	e := newSegEnv(t, true)
	e.must(e.s.Repository.Transact(t.Context(), e.scope, func(tx Transaction) error {
		st := tx.State()
		d, _ := current(st.Gate)
		for i := 0; i < 10000; i++ {
			status := []string{"review", "admitted", "declined"}[i%3]
			st.Submissions = append(st.Submissions, Submission{ID: fmt.Sprintf("s%05d", i), Person: fmt.Sprintf("p%05d", i), Version: "1.0.0", Status: status, Revision: 1, SubmittedAt: e.now, Fields: d.Fields[:1]})
		}
		return nil
	}))
	started := time.Now()
	all, err := e.s.ListSubmissions(t.Context(), Actor{"tenant", "admin"}, e.scope, "")
	waiting, err2 := e.s.ListSubmissions(t.Context(), Actor{"tenant", "admin"}, e.scope, "review")
	if err != nil || err2 != nil || len(all) != 10000 || len(waiting) != 3334 {
		t.Fatalf("%d %d %v %v", len(all), len(waiting), err, err2)
	}
	if took := time.Since(started); took > 300*time.Millisecond {
		t.Fatalf("listing and filtering 10,000 submissions took %v", took)
	}
}
