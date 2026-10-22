package chatgate

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

func cell(voters int, options map[string]int, segments ...string) Cell {
	sort.Strings(segments)
	return Cell{Signature: segments, Voters: voters, Options: options}
}

func reported(b Breakdown) string {
	out := []string{}
	for _, s := range b.Segments {
		out = append(out, s.Segment)
	}
	return strings.Join(out, ",")
}

// TestTodo_CHATGATE_010: a poll addressed to segments reports by segment only
// when five or more people in the group voted, and never in a way that lets one
// person's vote be subtracted out.
func TestTodo_CHATGATE_010(t *testing.T) {
	if MinGroup != 5 {
		t.Fatalf("the standard group size is %d", MinGroup)
	}
	// Two groups with six and seven voters who do not overlap: both reported.
	b := BreakDown([]Cell{cell(6, map[string]int{"yes": 4, "no": 2}, "Denver office"), cell(7, map[string]int{"yes": 1, "no": 6}, "Night shift")}, 5)
	if reported(b) != "Denver office,Night shift" || b.Total != 13 || b.Withheld != 0 || b.Segments[0].Options["yes"] != 4 {
		t.Fatalf("two large groups: %+v", b)
	}
	// A group of four is withheld, and says so.
	b = BreakDown([]Cell{cell(9, map[string]int{"yes": 9}, "Denver office"), cell(4, map[string]int{"no": 4}, "Austin office")}, 5)
	if reported(b) != "" || b.Withheld != 2 || b.Total != 13 {
		// Austin has four; Denver cannot be reported alone either, because the
		// total minus Denver is those four people.
		t.Fatalf("a small group: %+v", b)
	}
	// Nested groups: 7 in Denver, 6 of them also on the night shift. Reporting
	// both would give the one person in Denver but not on nights.
	b = BreakDown([]Cell{cell(6, map[string]int{"yes": 6}, "Denver office", "Night shift"), cell(1, map[string]int{"no": 1}, "Denver office")}, 5)
	if reported(b) != "Denver office" || b.Withheld != 1 {
		t.Fatalf("a nested group was reported with its parent: %+v", b)
	}
	for _, s := range b.Segments {
		if s.Voters < 5 {
			t.Fatalf("a group under five: %+v", s)
		}
	}
	// A caller cannot ask for a smaller group.
	if got := BreakDown([]Cell{cell(3, map[string]int{"yes": 3}, "A")}, 1); got.K != 5 || len(got.Segments) != 0 {
		t.Fatalf("k=1 was honoured: %+v", got)
	}
	// Nobody voted: nothing to report.
	if got := BreakDown(nil, 5); got.Total != 0 || len(got.Segments) != 0 {
		t.Fatalf("empty: %+v", got)
	}
}

// TestTodo_CHATGATE_010_Property: over generated memberships and votes, every
// reported group has at least k voters, every difference between two reported
// groups, a reported group and the total, and the total and the union of
// reported groups, is empty or has at least k voters, and the tallies are exact.
func TestTodo_CHATGATE_010_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(10))
	segments := []string{"A", "B", "C", "D"}
	options := []string{"yes", "no", "maybe"}
	for n := 0; n < 400; n++ {
		voters := rng.Intn(60)
		type voter struct {
			sig []string
			opt string
		}
		people := make([]voter, voters)
		byKey := map[string]*Cell{}
		for i := range people {
			sig := []string{}
			for _, s := range segments {
				if rng.Intn(3) == 0 {
					sig = append(sig, s)
				}
			}
			if len(sig) == 0 {
				sig = []string{segments[rng.Intn(len(segments))]}
			}
			people[i] = voter{sig, options[rng.Intn(len(options))]}
			key := strings.Join(sig, "|")
			if byKey[key] == nil {
				byKey[key] = &Cell{Signature: sig, Options: map[string]int{}}
			}
			byKey[key].Voters++
			byKey[key].Options[people[i].opt]++
		}
		cells := []Cell{}
		for _, c := range byKey {
			cells = append(cells, *c)
		}
		out := BreakDown(cells, 5)
		if out.Total != voters {
			t.Fatalf("total %d of %d", out.Total, voters)
		}
		in := func(p voter, s string) bool { return contains(p.sig, s) }
		count := func(pred func(voter) bool) int {
			c := 0
			for _, p := range people {
				if pred(p) {
					c++
				}
			}
			return c
		}
		shown := []string{}
		for _, r := range out.Segments {
			shown = append(shown, r.Segment)
			if r.Voters < 5 || r.Voters != count(func(p voter) bool { return in(p, r.Segment) }) {
				t.Fatalf("run %d: group %s reports %d voters", n, r.Segment, r.Voters)
			}
			for _, o := range options {
				if r.Options[o] != count(func(p voter) bool { return in(p, r.Segment) && p.opt == o }) {
					t.Fatalf("run %d: group %s option %s tally is wrong", n, r.Segment, o)
				}
			}
		}
		// Every union of reported groups against every other union: what is in
		// one and not the other is empty or has at least five voters.
		for a := 1; a < 1<<len(shown); a++ {
			inUnion := func(mask int, p voter) bool {
				for i, s := range shown {
					if mask&(1<<i) != 0 && in(p, s) {
						return true
					}
				}
				return false
			}
			for b := 0; b < 1<<len(shown); b++ {
				if d := count(func(p voter) bool { return inUnion(a, p) && !inUnion(b, p) }); d != 0 && d < 5 {
					t.Fatalf("run %d: groups %v mask %d minus %d leaves %d voters: one person's vote can be recovered", n, shown, a, b, d)
				}
			}
			// The total minus any union.
			if d := count(func(p voter) bool { return !inUnion(a, p) }); d != 0 && d < 5 {
				t.Fatalf("run %d: total minus %v leaves %d voters", n, shown, d)
			}
		}
	}
}

// audienceEnv is a private channel with addressing and restricted visibility on
// and two segments: Denver office (ana, ben, cy) and Night shift (ana, dee).
func audienceEnv(t *testing.T, private, restricted bool) *segEnv {
	e := newSegEnv(t, private)
	e.must(e.rules("segments", officeRules...))
	people := [][3]string{{"ana", "Denver", "Night"}, {"ben", "Denver", "Day"}, {"cy", "Denver", "Day"}, {"dee", "Austin", "Night"}}
	for _, p := range people {
		sub := e.join(p[0], p[1], p[2])
		if sub.Status == "review" {
			if _, err := e.s.Review(t.Context(), e.cmd("admin", "admit-"+p[0]), sub.ID, sub.Revision, true, "Known colleague"); err != nil {
				t.Fatal(err)
			}
		}
	}
	e.must(e.s.SetAudienceLevels(t.Context(), e.cmd("admin", "levels"), true, restricted))
	return e
}

// TestTodo_CHATGATE_011: content addressed to a segment goes to that segment;
// the author is told who it reaches before sending; the two levels are decided
// by the channel's administrators and are off by default; restricted
// visibility needs a private channel.
func TestTodo_CHATGATE_011(t *testing.T) {
	e := newSegEnv(t, true)
	// Off by default: nothing may be addressed.
	if _, err := e.s.Reach(t.Context(), Actor{"tenant", "admin"}, e.scope, []string{"Denver office"}, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("addressing is on by default: %v", err)
	}
	e = audienceEnv(t, true, true)
	reach, err := e.s.Reach(t.Context(), Actor{"tenant", "admin"}, e.scope, []string{"Denver office"}, true)
	if err != nil || reach.People != 3+1 || !reach.Restricted || len(reach.Segments) != 1 {
		// Three members and the author, who always reads their own message.
		t.Fatalf("an administrator's reach: %+v %v", reach, err)
	}
	reach, err = e.s.Reach(t.Context(), Actor{"tenant", "ana"}, e.scope, []string{"Denver office", "Night shift"}, false)
	if err != nil || reach.People != 4 {
		t.Fatalf("a member's reach over their own segments: %+v %v", reach, err)
	}
	// A member may address only segments they are in, so addressing never tells
	// them who is in somebody else's.
	if _, err = e.s.Reach(t.Context(), Actor{"tenant", "ben"}, e.scope, []string{"Night shift"}, false); !errors.Is(err, ErrDenied) {
		t.Fatalf("ben addressed a segment he is not in: %v", err)
	}
	if got, _ := e.s.AddressableSegments(t.Context(), Actor{"tenant", "ben"}, e.scope); strings.Join(got, ",") != "Denver office" {
		t.Fatalf("ben is offered %v", got)
	}
	if got, _ := e.s.AddressableSegments(t.Context(), Actor{"tenant", "admin"}, e.scope); strings.Join(got, ",") != "Denver office,Night shift" {
		t.Fatalf("an administrator is offered %v", got)
	}
	// An unknown segment is refused.
	if _, err = e.s.Reach(t.Context(), Actor{"tenant", "admin"}, e.scope, []string{"Nobody"}, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an unknown segment: %v", err)
	}
}

// TestTodo_CHATGATE_011_Security: restricted visibility needs the channel's own
// level, and a private channel; it is the only way a gate hides content.
func TestTodo_CHATGATE_011_Security(t *testing.T) {
	// A public channel cannot switch restricted visibility on.
	pub := newSegEnv(t, false)
	pub.must(pub.rules("segments", officeRules...))
	if err := pub.s.SetAudienceLevels(t.Context(), pub.cmd("admin", "r"), true, true); !errors.Is(err, ErrRestricted) {
		t.Fatalf("restricted visibility in a public channel: %v", err)
	}
	// Restricted without addressing makes no sense.
	if err := pub.s.SetAudienceLevels(t.Context(), pub.cmd("admin", "r2"), false, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("restricted without addressing: %v", err)
	}
	// Addressing alone may be on in a public channel, and then asks for no
	// restriction.
	pub.must(pub.s.SetAudienceLevels(t.Context(), pub.cmd("admin", "a"), true, false))
	if _, err := pub.s.Reach(t.Context(), Actor{"tenant", "admin"}, pub.scope, []string{"Denver office"}, true); !errors.Is(err, ErrRestricted) {
		t.Fatalf("a restricted message with the level off: %v", err)
	}
	// In a private channel with only addressing on, restricted is refused too.
	priv := audienceEnv(t, true, false)
	if _, err := priv.s.Reach(t.Context(), Actor{"tenant", "admin"}, priv.scope, []string{"Denver office"}, true); !errors.Is(err, ErrRestricted) {
		t.Fatalf("restricted with the level off: %v", err)
	}
	// Only an administrator changes the levels.
	if err := priv.s.SetAudienceLevels(t.Context(), Command{Scope: priv.scope, Actor: Actor{"tenant", "ben"}, Key: "x", ExpectedRevision: priv.cmd("admin", "y").ExpectedRevision}, true, true); !errors.Is(err, ErrDenied) {
		t.Fatalf("a member switched visibility: %v", err)
	}
	// A gate never changes what a non-member can discover: the public read of a
	// gate carries no segment, rule or placement.
	g, err := priv.s.Get(t.Context(), Actor{"tenant", "outsider"}, priv.scope, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(g)
	for _, leak := range []string{"Denver office", "Night shift", "Assignments", "Segments"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("the gate read by a non-member carries %q", leak)
		}
	}
	// A person outside every segment reaches nothing and is not in any list.
	st := priv.state().Segments
	if st.InAny("outsider", []string{"Denver office", "Night shift"}) || len(st.Members([]string{"Night shift"})) != 2 {
		t.Fatalf("membership: %+v", st.Members([]string{"Night shift"}))
	}
}

// TestTodo_CHATGATE_011_Property: no sequence of segment changes reveals a
// restricted message to someone outside its audience at send time. The audience
// is frozen with the message, so it is the same whatever the rules do after.
func TestTodo_CHATGATE_011_Property(t *testing.T) {
	e := audienceEnv(t, true, true)
	_, frozen, err := ResolveAudience(e.state().Segments, true, true, "admin", []string{"Night shift"}, true)
	if err != nil {
		t.Fatal(err)
	}
	sent := append([]string(nil), frozen...)
	// Every kind of change: more people join the segment, the rules are
	// reverted, the segment is renamed away, people withdraw.
	e.join("eve", "Austin", "Night")
	e.must(e.rules("rename", SegmentRule{ID: "r1", Field: "office", Value: "Denver", Segment: "Day crew"}))
	e.must(e.s.RevertSegments(t.Context(), e.cmd("admin", "revert"), 1, "undo"))
	e.join("fay", "Austin", "Night")
	if strings.Join(frozen, ",") != strings.Join(sent, ",") {
		t.Fatal("the frozen audience moved")
	}
	for _, outsider := range []string{"eve", "fay", "ben", "cy", "stranger"} {
		if contains(frozen, outsider) {
			t.Fatalf("%s is in an audience frozen before they joined or never were in it: %v", outsider, frozen)
		}
	}
	if !contains(frozen, "ana") || !contains(frozen, "dee") || !contains(frozen, "admin") {
		t.Fatalf("the audience at send time: %v", frozen)
	}
}

var _ = context.Background
