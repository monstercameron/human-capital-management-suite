package chatui

import (
	"strings"
	"testing"
)

// agentUX029Room is a channel whose members have not been read yet: the
// directory knows everyone, the page has seen Polly Adams write here, and
// Sofia Beltran is not in the channel.
func agentUX029Room(membersLoaded bool) Model {
	m := Model{
		SelectedID: "room", CurrentUser: "walt",
		Conversations: []Conversation{{ID: "room", Name: "random", Kind: PublicChannel, Joined: true}},
		Messages:      []Message{{ID: "m1", AuthorID: "polly", Author: "Polly Adams", Body: "Hello"}},
		SearchDirectory: []SearchPerson{
			{ID: "worker-sofia", Name: "Sofia Beltran"}, {ID: "worker-polly", Name: "Polly Adams"}, {ID: "worker-ana", Name: "Ana Ruiz"},
		},
		Callbacks: Callbacks{LoadMembers: func() {}},
	}
	if membersLoaded {
		m.Members = []Member{{ID: "walt", Name: "Walt Brennan"}, {ID: "ana", Name: "Ana Ruiz"}, {ID: "polly", Name: "Polly Adams"}}
	}
	return m
}

func agentUX029Open(query string) mentionState {
	return mentionState{Target: "chat-composer", Query: query, Open: true}
}

// TestTodo_AGENTUX_029_Grouping: the people group is there from the first
// open. While the members load it holds the people already seen here and says
// so quietly; nobody from the directory is listed until the menu can say which
// group they are in, so nobody is listed in the wrong one.
func TestTodo_AGENTUX_029_Grouping(t *testing.T) {
	const loading = "Loading who is in this conversation…"
	for _, query := range []string{"", "s", "po"} {
		before := renderNode(t, mentionMenu(agentUX029Room(false), agentUX029Open(query), "chat-composer"))
		if !strings.Contains(before, `class="mention-heading people"`) || !strings.Contains(before, "People in this conversation") || !strings.Contains(before, loading) {
			t.Errorf("query %q: the people group and its loading line are not there before the members load: %s", query, before)
		}
		if strings.Contains(before, "Not in this conversation") || strings.Contains(before, "Sofia Beltran") {
			t.Errorf("query %q: someone from the directory is listed before the menu can say where they belong: %s", query, before)
		}
		if query == "po" && !strings.Contains(before, "Polly Adams") {
			t.Errorf("the person already seen here is not offered while the members load: %s", before)
		}

		after := renderNode(t, mentionMenu(agentUX029Room(true), agentUX029Open(query), "chat-composer"))
		if strings.Contains(after, loading) {
			t.Errorf("query %q: the loading line stays after the members loaded: %s", query, after)
		}
		if query == "" && (!strings.Contains(after, "People in this conversation") || !strings.Contains(after, "Not in this conversation") || !strings.Contains(after, "Sofia Beltran")) {
			t.Errorf("the two groups are not there once the members have loaded: %s", after)
		}
	}

	// Typing a name nobody matches while the members load is not "no one":
	// the menu stays open and says what it waits for.
	only := renderNode(t, mentionMenu(agentUX029Room(false), agentUX029Open("sof"), "chat-composer"))
	if strings.Contains(only, "mention-empty") || !strings.Contains(only, `id="chat-composer-mentions"`) || !strings.Contains(only, loading) {
		t.Errorf("a name that only the directory knows closed the menu or said nobody matches: %s", only)
	}

	// A page with no way to read members has nothing to wait for.
	noLoader := agentUX029Room(false)
	noLoader.Callbacks.LoadMembers = nil
	if markup := renderNode(t, mentionMenu(noLoader, agentUX029Open("sof"), "chat-composer")); strings.Contains(markup, loading) || !strings.Contains(markup, "Sofia Beltran") {
		t.Errorf("a page that cannot read members waits for them: %s", markup)
	}
}

// TestTodo_AGENTUX_029_StableHighlight: when rows arrive above the highlighted
// one, the highlight stays on its option; a highlight the person moved is
// theirs.
func TestTodo_AGENTUX_029_StableHighlight(t *testing.T) {
	store := mentionStore{box: &mentionBox{conversationID: "room", state: agentUX029Open("")}}
	before := agentUX029Room(false)
	store.SettleActive(before)
	if st := store.Get(); st.ActiveKey != "person:polly" || st.Active != 0 {
		t.Fatalf("the highlight is on %q at %d, want Polly Adams at 0", st.ActiveKey, st.Active)
	}
	// The members arrive: Ana Ruiz and Walt Brennan sort above Polly Adams.
	after := agentUX029Room(true)
	store.SettleActive(after)
	st := store.Get()
	options, _ := mentionOptionsForState(after, "", "chat-composer", false)
	if st.ActiveKey != "person:polly" || options[st.Active].person == nil || options[st.Active].person.ID != "polly" {
		t.Fatalf("the highlight moved off Polly Adams when members arrived: %+v of %d options", st, len(options))
	}
	if st.Active == 0 {
		t.Fatalf("the members did not arrive above the highlighted row; the test proves nothing: %+v", options)
	}

	// The person moved it with the arrow keys: it stays where they put it.
	moved := store.Get()
	moved.Active = 0
	store.box.state = moved
	store.SettleActive(after)
	if got := store.Get(); got.Active != 0 || got.ActiveKey != mentionOptionKey(options[0]) {
		t.Fatalf("the person's own highlight was moved: %+v", got)
	}

	// A highlighted option that is gone falls back to a row that exists.
	store.box.state = mentionState{Target: "chat-composer", Query: "ana", Open: true, Active: 5, ActiveKey: "person:gone", SettledAt: 5}
	store.SettleActive(after)
	if got := store.Get(); got.Active != 0 || got.ActiveKey != "person:ana" {
		t.Fatalf("a vanished option left the highlight at %+v", got)
	}
	// A closed menu is left alone.
	closed := mentionStore{box: &mentionBox{state: mentionState{ActiveKey: "x", Active: 3}}}
	closed.SettleActive(after)
	if got := closed.Get(); got.Active != 3 || got.ActiveKey != "x" {
		t.Fatalf("a closed menu was changed: %+v", got)
	}
}

// TestTodo_AGENTUX_029_Browser renders the loading line in the three languages
// with a catalog that answers every key with a bracketed key, the way the
// product's does for a key it does not hold.
func TestTodo_AGENTUX_029_Browser(t *testing.T) {
	for locale, want := range map[string]string{
		"en-US": "Loading who is in this conversation…",
		"de-DE": "Die Mitglieder dieser Unterhaltung werden geladen…",
		"ar":    "جارٍ تحميل أعضاء هذه المحادثة…",
	} {
		m := agentUX029Room(false)
		m.Locale = locale
		m.Text = func(key string) string { return "⟦" + key + "⟧" }
		markup := renderNode(t, mentionMenu(m, agentUX029Open(""), "chat-composer"))
		if strings.Contains(markup, "⟦") {
			t.Errorf("%s: the menu prints a copy key: %s", locale, markup)
		}
		if !strings.Contains(markup, ">"+want+"<") || !strings.Contains(markup, `aria-live="polite"`) || !strings.Contains(markup, `role="status"`) {
			t.Errorf("%s: want the quiet status line %q: %s", locale, want, markup)
		}
	}
}
