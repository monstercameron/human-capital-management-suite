package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// #3954: a poll card offers "Plan next step" to a reader who may see its
// results. The panel is private to the reader, names leaders and ties without
// calling an unfinished poll decided, asks for a confirmed date and named time
// zone before it states a time, and creates or sends nothing.

func issue3954Fixture(results string, counts ...int) (Model, chat.Chatcmd002View) {
	m := chatcmd003UIFixture()
	texts := []string{"9:00 AM ET", "10:00 AM PT", "Friday afternoon"}
	poll := &chat.Chatcmd002Poll{Results: results, AddOptions: "author"}
	for i, n := range counts {
		poll.Options = append(poll.Options, chat.ChannelPollOption{ID: string(rune('a' + i)), Text: texts[i], Count: n})
	}
	view := chat.Chatcmd002View{Card: chat.Chatcmd002Card{Kind: "poll", Title: "Onboarding session?", Poll: poll}, ResultsVisible: results == "always", Revision: 4}
	m.Chatcmd002Views = map[string]chat.Chatcmd002View{"post-1": view}
	return m, view
}

func issue3954Open(t *testing.T, m Model, local localStore) Model {
	t.Helper()
	if !chatcmd002ActionLocal(m, local, "chatcmd002-plan", "post-1", "") {
		t.Fatal("the open action was not handled")
	}
	m.issue3954Plan = local.get().plan
	return m
}

func issue3954Typed(values map[string]string) func() {
	old := issue3954Fields
	issue3954Fields = func(string) map[string]string { return values }
	return func() { issue3954Fields = old }
}

func issue3954FixedNow(t *testing.T) {
	t.Helper()
	old := issue3954Now
	issue3954Now = func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { issue3954Now = old })
}

func TestIssue3954_OfferedOnlyWhereResultsAreVisible(t *testing.T) {
	m, view := issue3954Fixture("always", 2, 1)
	if markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false)); !strings.Contains(markup, "Plan next step") || !strings.Contains(markup, `data-action="chatcmd002-plan"`) {
		t.Fatalf("a poll whose results the reader sees has no Plan next step: %s", markup)
	}
	if markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, true)); strings.Contains(markup, "Plan next step") {
		t.Fatalf("the preview offers Plan next step: %s", markup)
	}
	hidden, hiddenView := issue3954Fixture("after-closing", 0, 0)
	if markup := renderNode(t, Chatcmd002RenderCard(hidden, "post-1", hiddenView, false)); strings.Contains(markup, "Plan next step") {
		t.Fatalf("a poll whose results are hidden offers Plan next step: %s", markup)
	}
	todo := chat.Chatcmd002View{Card: chat.Chatcmd002Card{Kind: "todo", Title: "T", Todo: &chat.Chatcmd002Todo{Items: []chat.Chatcmd002Task{{}}}}}
	if markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", todo, false)); strings.Contains(markup, "Plan next step") {
		t.Fatalf("a to-do list offers Plan next step: %s", markup)
	}
}

func TestIssue3954_HiddenResultsRefuseTheAction_Security(t *testing.T) {
	m, _ := issue3954Fixture("after-closing", 0, 0)
	local := localStore{box: &localUI{}}
	chatcmd002ActionLocal(m, local, "chatcmd002-plan", "post-1", "")
	if local.get().plan.Post != "" {
		t.Fatalf("the panel opened on a poll whose results are hidden: %+v", local.get().plan)
	}
	m.Chatcmd002Views = nil
	chatcmd002ActionLocal(m, local, "chatcmd002-plan", "post-1", "")
	if local.get().plan.Post != "" {
		t.Fatal("the panel opened before the card's view was read")
	}
}

func TestIssue3954_LeaderTieAndOpenStateAreSaidPlainly(t *testing.T) {
	cases := []struct {
		name   string
		counts []int
		closed bool
		want   []string
		not    []string
	}{
		{"one leader open", []int{3, 1}, false, []string{"9:00 AM ET has the most votes: 3 of 4", "Voting is still open"}, []string{"Tie", "decided"}},
		{"tie", []int{2, 2}, false, []string{"Tie: 9:00 AM ET; 10:00 AM PT each have 2 of 4 votes", "has not picked one"}, []string{"has the most votes"}},
		{"closed leader", []int{1, 3}, true, []string{"10:00 AM PT has the most votes: 3 of 4", "Voting is closed"}, []string{"still open"}},
		{"no votes", []int{0, 0}, false, []string{"No votes yet"}, []string{"has the most votes", "Tie"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, view := issue3954Fixture("always", tc.counts...)
			if tc.closed {
				now := time.Now()
				view.Card.ClosedAt = &now
				m.Chatcmd002Views["post-1"] = view
			}
			local := localStore{box: &localUI{}}
			m = issue3954Open(t, m, local)
			markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false))
			for _, want := range tc.want {
				if !strings.Contains(markup, want) {
					t.Errorf("panel lacks %q: %s", want, markup)
				}
			}
			for _, not := range tc.not {
				if strings.Contains(markup, not) {
					t.Errorf("panel wrongly says %q: %s", not, markup)
				}
			}
			for _, want := range []string{"Onboarding session?", "Only you can see this", "Calendar is not connected"} {
				if !strings.Contains(markup, want) {
					t.Errorf("panel lacks %q: %s", want, markup)
				}
			}
		})
	}
}

func TestIssue3954_PanelNeverNamesVoters_Security(t *testing.T) {
	m, view := issue3954Fixture("always", 2, 1)
	view.Voters = map[string][]chat.Chatcmd002Voter{"a": {{HomeTenantID: "t", SubjectID: "u-secret"}}}
	view.Card.Poll.Anonymous = true
	m.Chatcmd002Views["post-1"] = view
	m.Members = []Member{{ID: "u-secret", Name: "Secret Person"}}
	local := localStore{box: &localUI{}}
	m = issue3954Open(t, m, local)
	panel := renderNode(t, issue3954Panel(m, "post-1", view))
	if panel == "" || strings.Contains(panel, "u-secret") || strings.Contains(panel, "Secret Person") {
		t.Fatalf("the plan is empty or names a voter: %s", panel)
	}
}

func TestIssue3954_FreeFormTimeNeedsDateZoneAndLength(t *testing.T) {
	issue3954FixedNow(t)
	m, view := issue3954Fixture("always", 3, 1)
	local := localStore{box: &localUI{}}
	m = issue3954Open(t, m, local)
	prepare := func(values map[string]string) string {
		defer issue3954Typed(values)()
		chatcmd002ActionLocal(m, local, "chatcmd002-plan-prepare", "post-1", "")
		m.issue3954Plan = local.get().plan
		return renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false))
	}
	// Nothing is stated before the organizer confirms: the draft keeps the
	// original wording and says the time is still to be confirmed.
	if markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false)); !strings.Contains(markup, "still to be confirmed") || !strings.Contains(markup, "9:00 AM ET") {
		t.Fatalf("the first draft does not keep the original wording: %s", markup)
	}
	refusals := []struct {
		name   string
		values map[string]string
		want   string
	}{
		{"no date", map[string]string{"plan-time": "09:00", "plan-zone": "America/New_York", "plan-minutes": "30"}, "Enter a date and a time"},
		{"no zone", map[string]string{"plan-date": "2030-06-03", "plan-time": "09:00", "plan-minutes": "30"}, "time zone name"},
		{"abbreviation", map[string]string{"plan-date": "2030-06-03", "plan-time": "09:00", "plan-zone": "ET", "plan-minutes": "30"}, "time zone name"},
		{"no length", map[string]string{"plan-date": "2030-06-03", "plan-time": "09:00", "plan-zone": "America/New_York"}, "between 5 and 1440 minutes"},
		{"skipped hour", map[string]string{"plan-date": "2030-03-10", "plan-time": "02:30", "plan-zone": "America/New_York", "plan-minutes": "30"}, "does not exist"},
		{"repeated hour", map[string]string{"plan-date": "2030-11-03", "plan-time": "01:30", "plan-zone": "America/New_York", "plan-minutes": "30"}, "happens twice"},
		{"past", map[string]string{"plan-date": "2029-06-03", "plan-time": "09:00", "plan-zone": "America/New_York", "plan-minutes": "30"}, "already passed"},
	}
	for _, tc := range refusals {
		markup := prepare(tc.values)
		if !strings.Contains(markup, tc.want) || !strings.Contains(markup, `role="alert"`) {
			t.Errorf("%s: no alert saying %q: %s", tc.name, tc.want, markup)
		}
		if strings.Contains(markup, "Date and time: ") && (strings.Contains(markup, "(UTC-") || strings.Contains(markup, "(UTC+")) {
			t.Errorf("%s: a time was stated anyway: %s", tc.name, markup)
		}
		if got := local.get().plan.Zone; got != tc.values["plan-zone"] {
			t.Errorf("%s: the typed zone was not kept: %q", tc.name, got)
		}
	}
	// A confirmed time is stated with its zone and offset, never guessed.
	markup := prepare(map[string]string{"plan-date": "2030-12-03", "plan-time": "09:00", "plan-zone": "America/New_York", "plan-minutes": "45"})
	for _, want := range []string{"Date and time: December 3, 2030, 9:00 AM America/New_York (UTC-5)", "Length: 45 minutes", "Chosen in the poll: 9:00 AM ET"} {
		if !strings.Contains(markup, want) {
			t.Errorf("confirmed draft lacks %q: %s", want, markup)
		}
	}
}

func TestIssue3954_TiedPollNeedsAChoiceAndStaysNotFinal(t *testing.T) {
	issue3954FixedNow(t)
	m, view := issue3954Fixture("always", 2, 2)
	local := localStore{box: &localUI{}}
	m = issue3954Open(t, m, local)
	defer issue3954Typed(map[string]string{"plan-date": "2030-12-03", "plan-time": "09:00", "plan-zone": "Europe/Berlin", "plan-minutes": "30"})()
	chatcmd002ActionLocal(m, local, "chatcmd002-plan-prepare", "post-1", "")
	m.issue3954Plan = local.get().plan
	if markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false)); !strings.Contains(markup, "Choose which option to plan") {
		t.Fatalf("a tie was planned without a choice: %s", markup)
	}
	chatcmd002ActionLocal(m, local, "chatcmd002-plan-pick", "post-1", "b")
	chatcmd002ActionLocal(m, local, "chatcmd002-plan-prepare", "post-1", "")
	m.issue3954Plan = local.get().plan
	markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false))
	for _, want := range []string{"Chosen in the poll: 10:00 AM PT", "Date and time: December 3, 2030, 9:00 AM Europe/Berlin (UTC+1)", "tied and voting is still open; this is not a final decision"} {
		if !strings.Contains(markup, want) {
			t.Errorf("tied draft lacks %q: %s", want, markup)
		}
	}
	chatcmd002ActionLocal(m, local, "chatcmd002-plan-pick", "post-1", "zzz")
	if local.get().plan.Option != "b" {
		t.Errorf("an option the poll does not have was accepted: %+v", local.get().plan)
	}
}

func TestIssue3954_ChangedVotesAreShownAndRecheckedBeforeAnApprovedDraft(t *testing.T) {
	issue3954FixedNow(t)
	m, view := issue3954Fixture("always", 3, 1)
	local := localStore{box: &localUI{}}
	m = issue3954Open(t, m, local)
	// A vote arrives: the post's revision moves on.
	view.Revision = 5
	view.Card.Poll.Options[1].Count = 4
	m.Chatcmd002Views["post-1"] = view
	m.issue3954Plan = local.get().plan
	markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false))
	if !strings.Contains(markup, "The votes changed since you opened this") || !strings.Contains(markup, `data-action="chatcmd002-plan-refresh"`) {
		t.Fatalf("a changed poll is not flagged: %s", markup)
	}
	// Preparing details after a change is held back once, so the organizer reads the new result.
	defer issue3954Typed(map[string]string{"plan-date": "2030-12-03", "plan-time": "09:00", "plan-zone": "Europe/Berlin", "plan-minutes": "30"})()
	chatcmd002ActionLocal(m, local, "chatcmd002-plan-prepare", "post-1", "")
	if local.get().plan.Details != "" {
		t.Fatalf("details were prepared from a poll that changed: %+v", local.get().plan)
	}
	m.issue3954Plan = local.get().plan
	if markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false)); strings.Contains(markup, "The votes changed since you opened this") {
		t.Fatalf("the warning stays after the organizer has seen the new result: %s", markup)
	}
	chatcmd002ActionLocal(m, local, "chatcmd002-plan-prepare", "post-1", "")
	if !strings.Contains(local.get().plan.Details, "Date and time: December 3, 2030, 9:00 AM Europe/Berlin") {
		t.Fatalf("details were not prepared after the recheck: %+v", local.get().plan)
	}
	// Then a vote after the draft was made is flagged again.
	view.Revision = 6
	m.Chatcmd002Views["post-1"] = view
	m.issue3954Plan = local.get().plan
	if markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false)); !strings.Contains(markup, "The votes changed since") {
		t.Fatalf("a vote after the draft is not flagged: %s", markup)
	}
	// If results become hidden the draft is not prepared and the panel closes.
	view.ResultsVisible = false
	m.Chatcmd002Views["post-1"] = view
	chatcmd002ActionLocal(m, local, "chatcmd002-plan-prepare", "post-1", "")
	if local.get().plan.Post != "" {
		t.Fatalf("details were prepared for a reader who can no longer see the results: %+v", local.get().plan)
	}
}

func TestIssue3954_CloseReturnsToTheButton(t *testing.T) {
	m, view := issue3954Fixture("always", 3, 1)
	local := localStore{box: &localUI{}}
	m = issue3954Open(t, m, local)
	if markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false)); !strings.Contains(markup, `id="issue3954-plan-post-1"`) || !strings.Contains(markup, `data-action="chatcmd002-plan-close"`) {
		t.Fatalf("an open panel has no heading to focus or no close button: %s", markup)
	}
	chatcmd002ActionLocal(m, local, "chatcmd002-plan-close", "post-1", "")
	m.issue3954Plan = local.get().plan
	markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false))
	if strings.Contains(markup, "Only you can see this") || !strings.Contains(markup, `id="issue3954-open-post-1"`) {
		t.Fatalf("closing did not bring back the button: %s", markup)
	}
}

func TestIssue3954_CopyInThreeLanguages(t *testing.T) {
	for key, copy := range issue3954Copy {
		for i, line := range copy {
			if strings.TrimSpace(line) == "" {
				t.Errorf("%s has no text for language %d", key, i)
			}
		}
		if copy[0] == copy[1] || copy[0] == copy[2] {
			t.Errorf("%s is not translated: %v", key, copy)
		}
	}
	for locale, want := range map[string]string{"de-DE": "Nächsten Schritt planen", "ar": "تخطيط الخطوة التالية"} {
		m, view := issue3954Fixture("always", 3, 1)
		m.Locale = locale
		local := localStore{box: &localUI{}}
		if markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false)); !strings.Contains(markup, want) {
			t.Errorf("%s: button lacks %q: %s", locale, want, markup)
		}
		m = issue3954Open(t, m, local)
		panel := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false))
		if strings.Contains(panel, "Only you can see this") || strings.Contains(panel, "Calendar is not connected") {
			t.Errorf("%s: panel has English copy: %s", locale, panel)
		}
		if locale == "ar" && !strings.Contains(panel, `dir="rtl"`) {
			t.Errorf("ar panel is not right to left: %s", panel)
		}
	}
}

// Poll wording is untrusted: a title or option that looks like a placeholder
// is printed as typed, never filled with the panel's own numbers.
func TestIssue3954_PollWordingIsPrintedAsTyped_Security(t *testing.T) {
	m, view := issue3954Fixture("always", 3, 1)
	view.Card.Title = "Kickoff {n} {status}"
	view.Card.Poll.Options[0].Text = "Mon {total} {n}"
	m.Chatcmd002Views["post-1"] = view
	local := localStore{box: &localUI{}}
	m = issue3954Open(t, m, local)
	markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false))
	for _, want := range []string{"Kickoff {n} {status}", "Mon {total} {n} has the most votes: 3 of 4", "Chosen in the poll: Mon {total} {n} (3 of 4 votes"} {
		if !strings.Contains(markup, want) {
			t.Errorf("wording was altered, lacks %q: %s", want, markup)
		}
	}
}

// Enter prepares and Escape closes from every field of the panel.
func TestIssue3954_FieldsAnswerEnterAndEscape(t *testing.T) {
	m, view := issue3954Fixture("always", 3, 1)
	local := localStore{box: &localUI{}}
	m = issue3954Open(t, m, local)
	markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false))
	if n := strings.Count(markup, `data-chatcmd002-enter="chatcmd002-plan-prepare"`); n != 4 {
		t.Errorf("want 4 fields that prepare on Enter, got %d: %s", n, markup)
	}
	if n := strings.Count(markup, `data-chatcmd002-escape="chatcmd002-plan-close"`); n != 4 {
		t.Errorf("want 4 fields that close on Escape, got %d: %s", n, markup)
	}
}
