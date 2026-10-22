package chatgate

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestTodo_CHATGATE_014: a member reads the channel's labels and who holds each
// (never the segments, never an answer), in a stable order; a person who is not
// in the channel reads nothing; a person who left holds no label; and the list
// follows the rules when they change.
func TestTodo_CHATGATE_014(t *testing.T) {
	e := newSegEnv(t, false)
	e.must(e.rules("first", officeRules...))
	e.join("ana", "Denver", "Night")
	e.join("ben", "Austin", "Night")
	e.join("cy", "Denver", "")
	list := func(person string) string {
		t.Helper()
		got, err := e.s.LabelHolders(t.Context(), Actor{"tenant", person}, e.scope)
		e.must(err)
		out := []string{}
		for _, l := range got {
			out = append(out, fmt.Sprintf("%s=%s", l.Name, strings.Join(l.People, "+")))
		}
		return strings.Join(out, "; ")
	}
	want := "Denver crew=ana+cy; On call=ana+ben"
	// Anyone in the channel reads the same list, whether or not they hold a label.
	for _, person := range []string{"ana", "ben", "cy", "dee"} {
		if got := list(person); got != want {
			t.Errorf("%s reads %q, want %q", person, got, want)
		}
	}
	// Segments are not part of it.
	if strings.Contains(list("dee"), "Denver office") || strings.Contains(list("dee"), "Night shift") {
		t.Errorf("a segment is listed: %s", list("dee"))
	}
	// Somebody outside the channel is refused.
	e.auth.fixtureAuthority.deny[PermissionMember] = true
	if _, err := e.s.LabelHolders(t.Context(), Actor{"tenant", "stranger"}, e.scope); !errors.Is(err, ErrDenied) {
		t.Errorf("a person outside the channel read the labels: %v", err)
	}
	delete(e.auth.fixtureAuthority.deny, PermissionMember)
	// A change of rules changes the list; a label nobody holds is not listed.
	e.must(e.rules("second", SegmentRule{ID: "r1", Field: "office", Value: "Austin", Label: "Austin crew", Segment: "Austin office"}))
	if got := list("dee"); got != "Austin crew=ben" {
		t.Errorf("after the rules changed: %q", got)
	}
	// Leaving removes the person's labels.
	e.must(e.s.Erase(t.Context(), e.cmd("admin", "erase-ben"), "ben", false))
	if got := list("dee"); got != "" {
		t.Errorf("a person who left still holds a label: %q", got)
	}
}

// TestTodo_CHATGATE_014_Options: what the composer offers under "Visible to".
// Nothing while addressing is off; a member the segments they are in, with how
// many are in each; a manager every segment; restricted only where the channel
// is private and its administrators turned it on; and nobody outside the
// channel is told anything.
func TestTodo_CHATGATE_014_Options(t *testing.T) {
	e := newSegEnv(t, true)
	e.must(e.rules("first", officeRules...))
	// In a private channel a join waits for review: admit each, as the page does.
	for _, person := range []string{"ana", "ben", "cy"} {
		office, shift := "Denver", "Night"
		if person == "ben" {
			office = "Austin"
		}
		if person == "cy" {
			shift = ""
		}
		sub := e.join(person, office, shift)
		_, err := e.s.Review(t.Context(), e.cmd("admin", "admit-"+person), sub.ID, sub.Revision, true, "fits")
		e.must(err)
	}
	options := func(person string) string {
		t.Helper()
		got, err := e.s.AudienceOptions(t.Context(), Actor{"tenant", person}, e.scope)
		e.must(err)
		out := []string{}
		for _, s := range got.Segments {
			out = append(out, fmt.Sprintf("%s=%d", s.Name, s.People))
		}
		return strings.Join(out, ",") + fmt.Sprintf("|restricted=%v", got.Restricted)
	}
	if got := options("ana"); got != "|restricted=false" {
		t.Errorf("with addressing off: %s", got)
	}
	e.must(e.s.SetAudienceLevels(t.Context(), e.cmd("admin", "levels-address"), true, false))
	if got := options("ana"); got != "Denver office=2,Night shift=2|restricted=false" {
		t.Errorf("ana: %s", got)
	}
	if got := options("ben"); got != "Night shift=2|restricted=false" {
		t.Errorf("ben, who is in one of them: %s", got)
	}
	if got := options("dee"); got != "|restricted=false" {
		t.Errorf("a person in no segment: %s", got)
	}
	if got := options("admin"); got != "Denver office=2,Night shift=2|restricted=false" {
		t.Errorf("a manager reads every segment: %s", got)
	}
	e.must(e.s.SetAudienceLevels(t.Context(), e.cmd("admin", "levels-restrict"), true, true))
	if got := options("ana"); got != "Denver office=2,Night shift=2|restricted=true" {
		t.Errorf("with restricted visibility on in a private channel: %s", got)
	}
	e.auth.private = false
	if got := options("ana"); !strings.HasSuffix(got, "restricted=false") {
		t.Errorf("a channel that is not private offers a restricted message: %s", got)
	}
	e.auth.fixtureAuthority.deny[PermissionMember] = true
	if _, err := e.s.AudienceOptions(t.Context(), Actor{"tenant", "stranger"}, e.scope); !errors.Is(err, ErrDenied) {
		t.Errorf("a person outside the channel was told the options: %v", err)
	}
}
