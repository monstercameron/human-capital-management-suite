package chatgate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// segEnv is a published gate with questions that feed labels, segments and an
// agent, a clock the test can move, and the revision bookkeeping every change
// to a gate needs.
type segEnv struct {
	t     *testing.T
	s     *Service
	scope Scope
	now   time.Time
	auth  *segAuthority
}

type segAuthority struct {
	fixtureAuthority
	private bool
}

func (a *segAuthority) Policy(context.Context, Scope) (Policy, error) {
	return Policy{Ceiling: "INTERNAL", Private: a.private}, nil
}

// Check lets everybody read their own and discover; administration is the
// person named "admin", as in the other fixtures. Every other permission the
// service asks for (agent_run, retention, member) is open.
func (a *segAuthority) Check(ctx context.Context, p Actor, scope Scope, action string) error {
	return a.fixtureAuthority.Check(ctx, p, scope, action)
}

func segFields() []Field {
	both := Visibility{Administrators: true, Members: true, Consumers: []string{ConsumerSegments, ConsumerAgent}}
	return []Field{
		{ID: "office", Kind: "single_choice", KindVersion: "1.0.0", Label: "Which office are you in?", Purpose: "Place you in your office's group", DataClass: "INTERNAL", Required: true, Options: []string{"Denver", "Austin"}, Visibility: both, RetentionDays: 90},
		{ID: "shift", Kind: "single_choice", KindVersion: "1.0.0", Label: "Which shift?", Purpose: "Place you in your shift's group", DataClass: "INTERNAL", Options: []string{"Night", "Day"}, Visibility: Visibility{Administrators: true, Members: true, Consumers: []string{ConsumerSegments}}, RetentionDays: 90},
		{ID: "note", Kind: "short_text", KindVersion: "1.0.0", Label: "Anything we should know?", Purpose: "Help the administrator", DataClass: "INTERNAL", Visibility: Visibility{Administrators: true, Consumers: []string{ConsumerSegments, ConsumerAgent}}, RetentionDays: 30},
		{ID: "region", Kind: "single_choice", KindVersion: "1.0.0", Label: "Which region?", Purpose: "Help the administrator", DataClass: "INTERNAL", Options: []string{"North", "South"}, Visibility: Visibility{Administrators: true, Consumers: []string{ConsumerSegments}}, RetentionDays: 30},
	}
}

func newSegEnv(t *testing.T, private bool) *segEnv {
	t.Helper()
	e := &segEnv{t: t, scope: Scope{"tenant", "room"}, now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), auth: &segAuthority{fixtureAuthority: fixtureAuthority{deny: map[string]bool{}}, private: private}}
	e.s = &Service{Repository: NewMemoryRepository(), Authority: e.auth, Registry: NewRegistry(), Clock: func() time.Time { return e.now }}
	mode := "automatic"
	if private {
		mode = "review"
	}
	e.must(e.s.Define(t.Context(), e.cmd("admin", "define"), Definition{Mode: mode, Purpose: "Join the payroll discussion", Fields: segFields()}))
	_, err := e.s.Publish(t.Context(), e.cmd("admin", "publish"), Version{1, 0, 0})
	e.must(err)
	return e
}

func (e *segEnv) must(err error) {
	e.t.Helper()
	if err != nil {
		e.t.Fatal(err)
	}
}

// cmd is a command at the gate's current revision, so tests do not count it.
func (e *segEnv) cmd(person, key string) Command {
	g, err := e.s.Get(e.t.Context(), Actor{"tenant", "admin"}, e.scope, true)
	if err != nil {
		e.t.Fatal(err)
	}
	return Command{Scope: e.scope, Actor: Actor{"tenant", person}, Key: key, ExpectedRevision: g.Revision}
}

func (e *segEnv) join(person, office, shift string) Submission {
	e.t.Helper()
	answers := map[string]json.RawMessage{"office": json.RawMessage(fmt.Sprintf("%q", office))}
	if shift != "" {
		answers["shift"] = json.RawMessage(fmt.Sprintf("%q", shift))
	}
	sub, err := e.s.Submit(e.t.Context(), e.cmd(person, "join-"+person+"-"+office+shift+fmt.Sprint(e.now.UnixNano())), "1.0.0", answers)
	e.must(err)
	return sub
}

func (e *segEnv) rules(reason string, rules ...SegmentRule) error {
	return e.s.SetSegments(e.t.Context(), e.cmd("admin", "rules-"+reason), rules, reason)
}

func (e *segEnv) mine(person string) []Placement {
	e.t.Helper()
	got, err := e.s.MySegments(e.t.Context(), Actor{"tenant", person}, e.scope)
	e.must(err)
	return got
}

func names(ps []Placement, kind string) string {
	out := []string{}
	for _, p := range ps {
		if p.Kind == kind {
			out = append(out, p.Name)
		}
	}
	return strings.Join(out, ",")
}

var officeRules = []SegmentRule{
	{ID: "r1", Field: "office", Value: "Denver", Label: "Denver crew", Segment: "Denver office"},
	{ID: "r2", Field: "shift", Value: "Night", Label: "On call", Segment: "Night shift"},
}

// TestTodo_CHATGATE_009: an administrator maps answers to labels and segments;
// they are applied when a join is admitted, re-applied when the rules change
// (versioned, audited, reversible), updated when an answer changes, removed when
// the person leaves, and a person can see why they hold each.
func TestTodo_CHATGATE_009(t *testing.T) {
	e := newSegEnv(t, false)
	e.must(e.rules("first", officeRules...))
	e.join("ana", "Denver", "Night")
	e.join("ben", "Austin", "Night")
	e.join("cy", "Denver", "")

	ana := e.mine("ana")
	if names(ana, PlacementLabel) != "Denver crew,On call" || names(ana, PlacementSegment) != "Denver office,Night shift" {
		t.Fatalf("ana's placements: %+v", ana)
	}
	// Why: the question as it was asked and the answer given.
	for _, p := range ana {
		if p.Name == "Denver office" && (p.Field != "office" || p.FieldLabel != "Which office are you in?" || p.Value != "Denver" || p.RuleID != "r1") {
			t.Fatalf("the reason for a placement is incomplete: %+v", p)
		}
	}
	if names(e.mine("ben"), PlacementSegment) != "Night shift" || names(e.mine("cy"), PlacementSegment) != "Denver office" {
		t.Fatalf("ben %+v cy %+v", e.mine("ben"), e.mine("cy"))
	}

	// An answer changes: the person is placed again.
	e.join("ben", "Denver", "Day")
	if names(e.mine("ben"), PlacementSegment) != "Denver office" || names(e.mine("ben"), PlacementLabel) != "Denver crew" {
		t.Fatalf("after ben changed his answers: %+v", e.mine("ben"))
	}

	// The rules change: everybody is re-evaluated, the old version is kept.
	e.must(e.rules("austin", SegmentRule{ID: "r1", Field: "office", Value: "Austin", Segment: "Austin office", Label: "Austin crew"}))
	if len(e.mine("ana")) != 0 || names(e.mine("ben"), PlacementSegment) != "" {
		t.Fatalf("denver people kept their placements under rules that no longer name them: %+v %+v", e.mine("ana"), e.mine("ben"))
	}
	overview, err := e.s.SegmentOverview(t.Context(), Actor{"tenant", "admin"}, e.scope)
	if err != nil || overview.Rules.Version != 2 || len(overview.History) != 1 || overview.History[0].Version != 1 || overview.Rules.Reason != "austin" {
		t.Fatalf("the rules are not versioned: %+v %v", overview, err)
	}

	// Reverting restores the first rules as a third version.
	e.must(e.s.RevertSegments(t.Context(), e.cmd("admin", "revert"), 1, "wrong office"))
	overview, _ = e.s.SegmentOverview(t.Context(), Actor{"tenant", "admin"}, e.scope)
	if overview.Rules.Version != 3 || !strings.Contains(overview.Rules.Reason, "Reverted to version 1") || len(overview.History) != 2 {
		t.Fatalf("revert: %+v", overview)
	}
	if names(e.mine("ana"), PlacementSegment) != "Denver office,Night shift" {
		t.Fatalf("revert did not re-apply: %+v", e.mine("ana"))
	}
	// Counts, never people.
	counted := map[string]int{}
	for _, s := range overview.Segments {
		counted[s.Name] = s.People
	}
	if counted["Denver office"] != 3 || counted["Night shift"] != 1 {
		t.Fatalf("counts: %+v", overview.Segments)
	}

	// Every use of an answer is in the person's own list of who has read it.
	reads, err := e.s.Reads(t.Context(), Actor{"tenant", "ana"}, e.scope)
	if err != nil {
		t.Fatal(err)
	}
	read := 0
	for _, r := range reads {
		if r.Consumer == ConsumerSegments && r.Purpose != "" && len(r.Fields) > 0 {
			read++
		}
	}
	if read < 2 {
		t.Fatalf("the segment reads are not audited for the person: %+v", reads)
	}
	var kinds []string
	st := e.state()
	for _, ev := range st.Events {
		kinds = append(kinds, ev.Kind)
	}
	if !strings.Contains(strings.Join(kinds, ","), "segments_rules_changed") || !strings.Contains(strings.Join(kinds, ","), "segments_changed") {
		t.Fatalf("changes are not audited as events: %v", kinds)
	}

	// Leaving removes them.
	sub, _ := e.s.MySubmission(t.Context(), Actor{"tenant", "cy"}, e.scope)
	cy := Command{Scope: e.scope, Actor: Actor{"tenant", "cy"}, Key: "leave", ExpectedRevision: e.cmd("cy", "x").ExpectedRevision}
	e.must(e.s.Withdraw(t.Context(), cy, sub.ID, sub.Revision))
	if len(e.mine("cy")) != 0 {
		t.Fatalf("a person who withdrew keeps placements: %+v", e.mine("cy"))
	}
	e.must(e.s.Erase(t.Context(), e.cmd("admin", "erase-ana"), "ana", false))
	if len(e.mine("ana")) != 0 {
		t.Fatalf("a person erased from the channel keeps placements: %+v", e.mine("ana"))
	}
}

func (e *segEnv) state() State {
	var out State
	e.must(e.s.Repository.Transact(e.t.Context(), e.scope, func(tx Transaction) error { out = *tx.State(); return nil }))
	return out
}

// TestTodo_CHATGATE_009_Security: a label or segment is never derived from a
// question members cannot see, never grants anything, and is not another
// person's to read.
func TestTodo_CHATGATE_009_Security(t *testing.T) {
	e := newSegEnv(t, false)
	// A question shown only to administrators cannot feed a label members see,
	// even though it names the consumer.
	hidden := SegmentRule{ID: "h", Field: "region", Value: "North", Segment: "Hidden group"}
	if err := e.rules("hidden", hidden); !errors.Is(err, ErrDenied) {
		t.Fatalf("a rule from an administrator-only question: %v", err)
	}
	// A question that does not name the consumer cannot feed it.
	e.must(e.s.Define(t.Context(), e.cmd("admin", "v2"), Definition{Mode: "automatic", Purpose: "Join", Fields: []Field{{ID: "office", Kind: "single_choice", KindVersion: "1.0.0", Label: "Office", Purpose: "Say where you are", DataClass: "INTERNAL", Required: true, Options: []string{"Denver", "Austin"}, Visibility: Visibility{Administrators: true, Members: true}, RetentionDays: 90}}}))
	if _, err := e.s.Publish(t.Context(), e.cmd("admin", "pub2"), Version{2, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := e.rules("no-consumer", SegmentRule{ID: "r", Field: "office", Value: "Denver", Segment: "Denver office"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("a rule from a question that does not name the consumer: %v", err)
	}
	// A label cannot be named like a permission.
	if err := e.rules("authority", SegmentRule{ID: "r", Field: "office", Value: "Denver", Label: "Administrator"}); !errors.Is(err, ErrReservedLabel) {
		t.Fatalf("a label named Administrator: %v", err)
	}
	// Only an administrator writes rules and reads the overview.
	if err := e.s.SetSegments(t.Context(), Command{Scope: e.scope, Actor: Actor{"tenant", "member"}, Key: "m", ExpectedRevision: e.cmd("admin", "x").ExpectedRevision}, officeRules, "mine"); !errors.Is(err, ErrDenied) {
		t.Fatalf("a member wrote rules: %v", err)
	}
	if _, err := e.s.SegmentOverview(t.Context(), Actor{"tenant", "member"}, e.scope); !errors.Is(err, ErrDenied) {
		t.Fatalf("a member read the overview: %v", err)
	}

	// The declaration an answer was given under decides: an answer given to a
	// question that did not name the consumer feeds nothing, whatever the form
	// says later.
	f := newSegEnv(t, false)
	f.must(f.s.Define(t.Context(), f.cmd("admin", "v2"), Definition{Mode: "automatic", Purpose: "Join", Fields: []Field{{ID: "office", Kind: "single_choice", KindVersion: "1.0.0", Label: "Office", Purpose: "Say where you are", DataClass: "INTERNAL", Required: true, Options: []string{"Denver", "Austin"}, Visibility: Visibility{Administrators: true, Members: true}, RetentionDays: 90}}}))
	if _, err := f.s.Publish(t.Context(), f.cmd("admin", "pub2"), Version{2, 0, 0}); err != nil {
		t.Fatal(err)
	}
	g, _ := f.s.Get(t.Context(), Actor{"tenant", "admin"}, f.scope, true)
	if g.Current != "2.0.0" {
		t.Fatalf("current %s", g.Current)
	}
	if _, err := f.s.Submit(t.Context(), f.cmd("late", "late"), "2.0.0", map[string]json.RawMessage{"office": json.RawMessage(`"Denver"`)}); err != nil {
		t.Fatal(err)
	}
	f.must(f.s.Define(t.Context(), f.cmd("admin", "v3"), Definition{Mode: "automatic", Purpose: "Join", Fields: []Field{{ID: "office", Kind: "single_choice", KindVersion: "1.0.0", Label: "Office", Purpose: "Say where you are", DataClass: "INTERNAL", Required: true, Options: []string{"Denver", "Austin"}, Visibility: Visibility{Administrators: true, Members: true, Consumers: []string{ConsumerSegments}}, RetentionDays: 90}}}))
	if _, err := f.s.Publish(t.Context(), f.cmd("admin", "pub3"), Version{3, 0, 0}); err != nil {
		t.Fatal(err)
	}
	f.must(f.rules("now", SegmentRule{ID: "r", Field: "office", Value: "Denver", Segment: "Denver office"}))
	if len(f.mine("late")) != 0 {
		t.Fatalf("an answer given before the question named the consumer feeds a segment: %+v", f.mine("late"))
	}
	if _, err := f.s.Submit(t.Context(), f.cmd("on3", "on3"), "3.0.0", map[string]json.RawMessage{"office": json.RawMessage(`"Denver"`)}); err != nil {
		t.Fatal(err)
	}
	if names(f.mine("on3"), PlacementSegment) != "Denver office" {
		t.Fatalf("an answer given under the new declaration: %+v", f.mine("on3"))
	}

	// A person sees their own placements only.
	if got, _ := f.s.MySegments(t.Context(), Actor{"tenant", "late"}, f.scope); len(got) != 0 {
		t.Fatalf("another person's placements: %+v", got)
	}
	// A label grants nothing: a person holding one is refused administration.
	if err := f.s.Define(t.Context(), Command{Scope: f.scope, Actor: Actor{"tenant", "on3"}, Key: "d", ExpectedRevision: f.cmd("admin", "x").ExpectedRevision}, Definition{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("a person with a segment defined a gate: %v", err)
	}
}
