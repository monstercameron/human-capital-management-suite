package chatgate

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CHATGATE-013: running a gate in production. Limits, abuse handling, the queue
// someone must watch, metrics an administrator reads, and the members who were
// in the channel before the gate was.

const (
	// MaxSubmissionsPerPersonPerMinute and MaxSubmissionsPerChannelPerMinute are
	// the joins-per-minute limits. A person fixing a typo does not reach the
	// first; a script does. The second keeps one busy channel's queue readable.
	MaxSubmissionsPerPersonPerMinute  = 5
	MaxSubmissionsPerChannelPerMinute = 120
	// MaxDeclinesPerDay is the abuse limit: a person declined this many times in
	// a day waits until the next, so a refused request cannot be sent over and
	// over to wear down the people who read the queue.
	MaxDeclinesPerDay = 3
)

// Existing members: what adding a gate to a channel with members does.
const (
	// ExistingNewOnly asks only people who join from now on.
	ExistingNewOnly = "new_only"
	// ExistingAsk asks current members to answer by a date, with reminders.
	ExistingAsk = "ask"
)

// Ops is a gate's operating settings and what it has already told people.
type Ops struct {
	ReviewAfter   time.Duration
	EscalateAfter time.Duration
	Notified      map[string]string
	Existing      ExistingMembers
}

// ExistingMembers is the choice made when a gate is added to a channel that has
// members. Nobody is removed by it: only an administrator's confirmed list is.
type ExistingMembers struct {
	Mode       string
	AnswerBy   time.Time
	RemindedOn string
}

// Roster is implemented by a Transaction that can list who is in the channel. A
// repository without it cannot offer the existing-member features, and a gate
// then asks nobody who is already in.
type Roster interface {
	Members(context.Context) ([]string, error)
	IsMember(context.Context, string) (bool, error)
}

// limits refuses a submission over a rate or abuse limit. Events are in time
// order, so the scan stops at the first one older than a day.
func (s *Service) limits(st *State, person string) error {
	now := s.now()
	perPerson, perChannel, declined := 0, 0, 0
	for i := len(st.Events) - 1; i >= 0; i-- {
		ev := st.Events[i]
		age := now.Sub(ev.At)
		if age > 24*time.Hour {
			break
		}
		switch ev.Kind {
		case "submitted", "answers_changed":
			if age <= time.Minute {
				perChannel++
				if ev.Person == person {
					perPerson++
				}
			}
		case "declined":
			if ev.Person == person {
				declined++
			}
		}
	}
	if perPerson >= MaxSubmissionsPerPersonPerMinute || perChannel >= MaxSubmissionsPerChannelPerMinute || declined >= MaxDeclinesPerDay {
		return ErrLimited
	}
	return nil
}

// QuestionDropOff is how many people who started the form answered one question.
type QuestionDropOff struct {
	Field             string
	Answered, Dropped int
}

// GateMetrics is what an administrator reads about a gate: counts, never people.
type GateMetrics struct {
	Started, Submitted, Admitted, Declined, Waiting int
	MedianDecision                                  time.Duration
	DropOff                                         []QuestionDropOff
	OldestWaiting                                   time.Duration
}

func (s *Service) Metrics(ctx context.Context, a Actor, scope Scope) (GateMetrics, error) {
	var out GateMetrics
	if s.Repository == nil {
		return out, ErrUnavailable
	}
	e := s.Repository.Transact(ctx, scope, func(tx Transaction) error {
		if e := s.access(ctx, a, scope, "admin"); e != nil {
			return e
		}
		st := tx.State()
		answered := map[string]map[string]bool{}
		note := func(person, field string) {
			if answered[person] == nil {
				answered[person] = map[string]bool{}
			}
			if field != "" {
				answered[person][field] = true
			}
		}
		current := map[string]string{}
		for _, sub := range st.Submissions {
			out.Submitted++
			note(sub.Person, "")
			if sub.Status != "superseded" && sub.Status != "withdrawn" {
				current[sub.Person] = sub.ID
			}
			switch sub.Status {
			case "admitted":
				out.Admitted++
			case "declined":
				out.Declined++
			case "review":
				out.Waiting++
				if age := s.now().Sub(sub.SubmittedAt); age > out.OldestWaiting {
					out.OldestWaiting = age
				}
			}
		}
		for _, x := range *tx.Answers() {
			if x.SubmissionID == "draft:"+x.Person || x.SubmissionID == current[x.Person] {
				note(x.Person, x.FieldID)
			}
		}
		out.Started = len(answered)
		if d, e := current0(st.Gate); e == nil {
			for _, f := range d.Fields {
				n := 0
				for _, fields := range answered {
					if fields[f.ID] {
						n++
					}
				}
				out.DropOff = append(out.DropOff, QuestionDropOff{Field: f.ID, Answered: n, Dropped: out.Started - n})
			}
		}
		times := []time.Duration{}
		for _, sub := range st.Submissions {
			if !sub.DecidedAt.IsZero() && !sub.DecidedAt.Before(sub.SubmittedAt) {
				times = append(times, sub.DecidedAt.Sub(sub.SubmittedAt))
			}
		}
		if len(times) > 0 {
			sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
			out.MedianDecision = times[len(times)/2]
			if len(times)%2 == 0 {
				out.MedianDecision = (times[len(times)/2-1] + times[len(times)/2]) / 2
			}
		}
		return nil
	})
	return out, e
}

// current0 is the version in force, or the last one when none is.
func current0(g Gate) (Definition, error) {
	if d, e := current(g); e == nil {
		return d, nil
	}
	if len(g.Versions) > 0 {
		return g.Versions[len(g.Versions)-1], nil
	}
	return Definition{}, ErrNotFound
}

// SetQueueDeadlines chooses how long a request may wait before the channel's
// administrators are told, and then the workspace administrator.
func (s *Service) SetQueueDeadlines(ctx context.Context, c Command, reviewAfter, escalateAfter time.Duration) error {
	_, e := s.mutate(ctx, c, "admin", []any{"deadlines", reviewAfter, escalateAfter}, func(tx Transaction) (any, error) {
		if reviewAfter < time.Hour || reviewAfter > 30*24*time.Hour || escalateAfter < reviewAfter || escalateAfter > 60*24*time.Hour {
			return nil, ErrInvalid
		}
		tx.State().Ops.ReviewAfter, tx.State().Ops.EscalateAfter = reviewAfter, escalateAfter
		return "set", nil
	})
	return e
}

// SweepQueue is run by the scheduler. A request waiting past the first deadline
// raises review_overdue (the channel's administrators); past the second,
// review_escalated (the workspace administrator). Each is raised once.
func (s *Service) SweepQueue(ctx context.Context, a Actor, scope Scope) (overdue, escalated int, err error) {
	if s.Repository == nil {
		return 0, 0, ErrUnavailable
	}
	err = s.Repository.Transact(ctx, scope, func(tx Transaction) error {
		if e := s.access(ctx, a, scope, "retention"); e != nil {
			return e
		}
		st := tx.State()
		if st.Ops.ReviewAfter <= 0 {
			return nil
		}
		if st.Ops.Notified == nil {
			st.Ops.Notified = map[string]string{}
		}
		day := s.now().Format("2006-01-02")
		c := Command{Scope: scope, Actor: a, Key: "sweep:" + day}
		for _, sub := range st.Submissions {
			if sub.Status != "review" {
				continue
			}
			age := s.now().Sub(sub.SubmittedAt)
			level := st.Ops.Notified[sub.ID]
			if age >= st.Ops.ReviewAfter && level == "" {
				st.Ops.Notified[sub.ID] = "administrators"
				s.event(st, c, "review_overdue", sub.Person, sub.ID, sub.Version)
				overdue++
				level = "administrators"
			}
			if st.Ops.EscalateAfter > 0 && age >= st.Ops.EscalateAfter && level != "workspace" {
				st.Ops.Notified[sub.ID] = "workspace"
				s.event(st, c, "review_escalated", sub.Person, sub.ID, sub.Version)
				escalated++
			}
		}
		return nil
	})
	return overdue, escalated, err
}

// SetExistingMembers records the choice made when a gate is added to a channel
// that already has members. It changes nothing for anybody now: a member is
// asked on their next visit only if the choice is "ask".
func (s *Service) SetExistingMembers(ctx context.Context, c Command, mode string, answerBy time.Time) error {
	_, e := s.mutate(ctx, c, "admin", []any{"existing", mode, answerBy}, func(tx Transaction) (any, error) {
		switch mode {
		case ExistingNewOnly:
			answerBy = time.Time{}
		case ExistingAsk:
			if !answerBy.After(s.now()) || answerBy.After(s.now().AddDate(0, 6, 0)) {
				return nil, ErrInvalid
			}
		default:
			return nil, ErrInvalid
		}
		tx.State().Ops.Existing = ExistingMembers{Mode: mode, AnswerBy: answerBy.UTC()}
		s.event(tx.State(), c, "existing_members_set", "", "", tx.State().Gate.Current)
		return mode, nil
	})
	return e
}

// unanswered lists current members with no answers or override for this
// version's major number, in a stable order.
func (s *Service) unanswered(ctx context.Context, tx Transaction) ([]string, error) {
	roster, ok := tx.(Roster)
	if !ok {
		return nil, ErrUnavailable
	}
	members, e := roster.Members(ctx)
	if e != nil {
		return nil, e
	}
	st := tx.State()
	live, e := ParseVersion(st.Gate.Current)
	if e != nil {
		return nil, nil
	}
	covered := map[string]bool{}
	for _, sub := range st.Submissions {
		if v, e := ParseVersion(sub.Version); e == nil && sub.Status == "admitted" && v.Major == live.Major {
			covered[sub.Person] = true
		}
	}
	for _, o := range st.Overrides {
		if o.Version == st.Gate.Current {
			covered[o.Person] = true
		}
	}
	out := []string{}
	for _, m := range members {
		if !covered[m] {
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Unanswered is the list an administrator confirms before anybody is removed.
func (s *Service) Unanswered(ctx context.Context, a Actor, scope Scope) ([]string, error) {
	var out []string
	if s.Repository == nil {
		return nil, ErrUnavailable
	}
	e := s.Repository.Transact(ctx, scope, func(tx Transaction) error {
		if e := s.access(ctx, a, scope, "admin"); e != nil {
			return e
		}
		var e error
		out, e = s.unanswered(ctx, tx)
		return e
	})
	return out, e
}

// RemindUnanswered raises one reminder per unanswered member, at most once a
// day, while the answer-by date has not passed.
func (s *Service) RemindUnanswered(ctx context.Context, a Actor, scope Scope) (int, error) {
	n := 0
	if s.Repository == nil {
		return 0, ErrUnavailable
	}
	e := s.Repository.Transact(ctx, scope, func(tx Transaction) error {
		if e := s.access(ctx, a, scope, "retention"); e != nil {
			return e
		}
		st := tx.State()
		day := s.now().Format("2006-01-02")
		if st.Ops.Existing.Mode != ExistingAsk || !s.now().Before(st.Ops.Existing.AnswerBy) || st.Ops.Existing.RemindedOn == day {
			return nil
		}
		people, e := s.unanswered(ctx, tx)
		if e != nil {
			return e
		}
		c := Command{Scope: scope, Actor: a, Key: "remind:" + day}
		for _, p := range people {
			s.event(st, c, "answer_reminder", p, "", st.Gate.Current)
			n++
		}
		st.Ops.Existing.RemindedOn = day
		return nil
	})
	return n, e
}

// RemoveUnanswered removes the members an administrator confirmed, and only
// them: after the answer-by date, and only people who are still unanswered. If
// the list moved since it was confirmed the call is refused with ErrConflict and
// nobody is removed. Nobody is ever removed by the passing of a date alone.
func (s *Service) RemoveUnanswered(ctx context.Context, c Command, confirmed []string) error {
	_, e := s.mutate(ctx, c, "admin", []any{"remove", confirmed}, func(tx Transaction) (any, error) {
		st := tx.State()
		if st.Ops.Existing.Mode != ExistingAsk || s.now().Before(st.Ops.Existing.AnswerBy) {
			return nil, ErrConflict
		}
		if len(confirmed) == 0 || len(confirmed) > 500 {
			return nil, ErrInvalid
		}
		live, e := s.unanswered(ctx, tx)
		if e != nil {
			return nil, e
		}
		for _, p := range confirmed {
			if strings.TrimSpace(p) == "" || !contains(live, p) {
				return nil, ErrConflict
			}
		}
		for _, p := range confirmed {
			if e := tx.Membership(ctx, Actor{Tenant: c.Scope.Tenant, Person: p}, st.Gate.Current, false); e != nil {
				return nil, e
			}
			s.erase(tx, p, false)
			s.applySegments(tx, c, p, false)
			s.event(st, c, "removed_unanswered", p, "", st.Gate.Current)
		}
		return strconv.Itoa(len(confirmed)), nil
	})
	return e
}

// StateReader is implemented by a repository that can hand over a gate's state
// for reading without the lock, the copy and the answers a change needs. A list
// of ten thousand submissions is read this way. The function must not keep or
// change what it is given.
type StateReader interface {
	ReadState(context.Context, Scope, func(State) error) error
}

// view reads the state through the repository's read path when it has one.
func (s *Service) view(ctx context.Context, scope Scope, fn func(State) error) error {
	if r, ok := s.Repository.(StateReader); ok {
		return r.ReadState(ctx, scope, fn)
	}
	return s.Repository.Transact(ctx, scope, func(tx Transaction) error { return fn(*tx.State()) })
}

// Operating is the gate's operating settings, for the administrator's page.
func (s *Service) Operating(ctx context.Context, a Actor, scope Scope) (Ops, error) {
	var out Ops
	if s.Repository == nil {
		return out, ErrUnavailable
	}
	e := s.view(ctx, scope, func(st State) error {
		if e := s.access(ctx, a, scope, "admin"); e != nil {
			return e
		}
		out = st.Ops
		out.Notified = nil
		return nil
	})
	return out, e
}

// SubmissionLister is implemented by a repository that can list a gate's
// submissions without reading its whole state: the rows of the submission log,
// the latest revision of each, without the questions each carries. The caller has
// already been authorized.
type SubmissionLister interface {
	ListSubmissionRows(ctx context.Context, scope Scope, status string) ([]Submission, error)
}
