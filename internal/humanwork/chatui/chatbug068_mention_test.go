package chatui

import (
	"strings"
	"testing"
)

// When the read of the conversation's members fails, the "@" menu says so and
// offers Try again, in every language the page ships, instead of keeping its
// loading line for good.
func TestTodo_CHATBUG_068_MentionMembersFailed(t *testing.T) {
	cases := map[string]struct{ failed, retry string }{
		"en-US": {"Could not load who is in this conversation.", "Try again"},
		"de-DE": {"Die Mitglieder dieser Unterhaltung konnten nicht geladen werden.", ""},
		"ar":    {"تعذّر تحميل أعضاء هذه المحادثة.", ""},
	}
	for locale, want := range cases {
		m := agentUX029Room(false)
		m.Locale = locale
		m.MembersFailed = true
		markup := renderNode(t, mentionMenu(m, agentUX029Open(""), "chat-composer"))
		if !strings.Contains(markup, want.failed) {
			t.Errorf("%s: the failure is not said: %s", locale, markup)
		}
		if strings.Contains(markup, agentUX029Copy["en-US"][keyAgentUX029PeopleLoading]) || strings.Contains(markup, agentUX029Copy[chatEmojiLocale(locale)][keyAgentUX029PeopleLoading]) {
			t.Errorf("%s: the loading line stays after the read failed: %s", locale, markup)
		}
		if !strings.Contains(markup, `data-action="mention-members-retry"`) {
			t.Errorf("%s: there is no retry control: %s", locale, markup)
		}
		if want.retry != "" && !strings.Contains(markup, ">"+want.retry+"<") {
			t.Errorf("%s: the retry control does not read %q: %s", locale, want.retry, markup)
		}
		if strings.Contains(markup, "chat.agentux029") || strings.Contains(markup, "chat.mention_agents_retry") {
			t.Errorf("%s: a copy key is printed: %s", locale, markup)
		}
		// Nobody from the directory is offered while the groups cannot be told apart.
		if strings.Contains(markup, "Sofia Beltran") {
			t.Errorf("%s: the directory is offered without the members: %s", locale, markup)
		}
	}

	// A list that has loaded never shows the failure, whatever the flag held.
	loaded := agentUX029Room(true)
	loaded.MembersFailed = true
	if markup := renderNode(t, mentionMenu(loaded, agentUX029Open(""), "chat-composer")); strings.Contains(markup, "mention-members-retry") {
		t.Errorf("the failure is shown over a loaded member list: %s", markup)
	}
}

// Picking a person writes the choice into the draft, and then tells the page,
// so the page draws the note under the composer without waiting for another
// render of its own.
func TestTodo_CHATBUG_068_PickAsksForARender(t *testing.T) {
	m := agentUX029Room(true)
	m.Callbacks.AddMembers = func([]string) {}
	var calls []string
	m.Callbacks.MentionPicked = func() { calls = append(calls, "render") }
	value := "hi @Sof"
	field := composerField{
		selection: func(string) (string, int, bool) { return value, len(value), true },
		replace: func(_, updated string, _ int) {
			value = updated
			calls = append(calls, "text")
		},
	}
	store := mentionStore{box: &mentionBox{conversationID: "room", state: mentionState{Target: "chat-composer", Query: "Sof", Start: 3, End: 7, Open: true, ShowAllPeople: true}}}
	m.Members = append(m.Members, Member{ID: "sofia", Name: "Sofia Beltran"})
	pickMention(m, store, field, store.Get(), 0)
	if got := strings.Join(calls, ","); got != "text,render" {
		t.Fatalf("the pick wrote the text and asked for a render in this order: %q, want text,render (value %q)", got, value)
	}
}

// The "@" menu keeps one order whatever is typed: Agents, then people in the
// conversation, then people who are not; an agent list that is loading or
// failed is said at the top, in the Agents group, like the members row is in
// the people group.
func TestTodo_CHATBUG_068_MentionGroupsKeepOneOrder(t *testing.T) {
	position := func(markup, needle string) int { return strings.Index(markup, needle) }
	room := func(state PersonaLookupState) Model {
		m := agentUXMentionModel(state)
		m.CurrentUser = "walt"
		m.Members = []Member{{ID: "walt", Name: "Walt Brennan"}, {ID: "camila", Name: "Camila Morales"}, {ID: "polly", Name: "Polly Adams"}}
		m.SearchDirectory = []SearchPerson{{ID: "worker-sofia", Name: "Sofia Beltran"}, {ID: "worker-pia", Name: "Pia Costa"}}
		m.Callbacks.LoadMembers = func() {}
		return m
	}

	for _, query := range []string{"", "p", "pol"} {
		markup := renderNode(t, mentionMenu(room(PersonaLookupReady), mentionState{Target: "chat-composer", Query: query, Open: true, ShowAllPeople: true}, "chat-composer"))
		agents, people, outside := position(markup, `class="mention-heading agents"`), position(markup, `class="mention-heading people"`), position(markup, `class="mention-heading outside"`)
		if agents < 0 {
			t.Fatalf("query %q: no Agents group: %s", query, markup)
		}
		if people >= 0 && agents > people || outside >= 0 && agents > outside || people >= 0 && outside >= 0 && people > outside {
			t.Errorf("query %q: the groups are not Agents, People, Not in this conversation: agents %d people %d outside %d", query, agents, people, outside)
		}
	}

	// Typing filters all three groups.
	markup := renderNode(t, mentionMenu(room(PersonaLookupReady), mentionState{Target: "chat-composer", Query: "pol", Open: true, ShowAllPeople: true}, "chat-composer"))
	for _, want := range []string{"Policy Helper", "Polly Adams"} {
		if !strings.Contains(markup, want) {
			t.Errorf("typing %q dropped %s: %s", "pol", want, markup)
		}
	}
	for _, gone := range []string{"Camila Morales", "Sofia Beltran"} {
		if strings.Contains(markup, gone) {
			t.Errorf("typing %q left %s in the list: %s", "pol", gone, markup)
		}
	}

	// A list of agents that is still loading or has failed is said first, with
	// the same retry the failed members row has, also while a name is typed.
	for _, query := range []string{"", "pol"} {
		loading := room(PersonaLookupLoading)
		loading.ResolvedPersonaMentions = nil
		markup := renderNode(t, mentionMenu(loading, mentionState{Target: "chat-composer", Query: query, Open: true}, "chat-composer"))
		agents, status, people := position(markup, `class="mention-heading agents"`), position(markup, "Loading agents"), position(markup, `class="mention-heading people"`)
		if agents < 0 || status < agents || (people >= 0 && status > people) {
			t.Errorf("query %q: the agents loading line is not under the Agents heading, above the people: agents %d status %d people %d: %s", query, agents, status, people, markup)
		}
		failed := room(PersonaLookupFailed)
		failed.ResolvedPersonaMentions = nil
		markup = renderNode(t, mentionMenu(failed, mentionState{Target: "chat-composer", Query: query, Open: true}, "chat-composer"))
		agents, retry, people := position(markup, `class="mention-heading agents"`), position(markup, `data-action="persona-mention-retry"`), position(markup, `class="mention-heading people"`)
		if agents < 0 || retry < agents || (people >= 0 && retry > people) {
			t.Errorf("query %q: the failed agents row and its retry are not first: agents %d retry %d people %d: %s", query, agents, retry, people, markup)
		}
	}

	// Rows that arrive do not take the highlight from the option it is on.
	store := mentionStore{box: &mentionBox{conversationID: "room", state: mentionState{Target: "chat-composer", Open: true, ShowAllPeople: true}}}
	before := room(PersonaLookupLoading)
	before.ResolvedPersonaMentions = nil
	store.SettleActive(before)
	first := store.Get().ActiveKey
	store.box.state.Active, store.box.state.ActiveKey, store.box.state.SettledAt = 1, "person:polly", 1
	store.SettleActive(room(PersonaLookupReady))
	if got := store.Get().ActiveKey; got != "person:polly" || first == "" {
		t.Errorf("the highlight moved off its option when the agents arrived: %q", got)
	}
}
