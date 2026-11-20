package chatui

import (
	"strings"
	"testing"
)

func issue3937Model() Model {
	return Model{
		Locale:     "en-US",
		SelectedID: "design",
		Conversations: []Conversation{
			{ID: "design", Name: "design", Kind: PublicChannel},
			{ID: "ops", Name: "ops", Kind: PublicChannel},
			{ID: "dm1", Name: "Camila Morales", Kind: DirectMessage},
			{ID: "grp", Name: "Launch crew", Kind: GroupChat},
		},
		Callbacks: Callbacks{Search: func(string) {}},
	}
}

func TestIssue3937_HeaderSearchScopesToCurrentConversation(t *testing.T) {
	for _, tc := range []struct {
		name, selected, current, want string
	}{
		{"channel, empty box", "design", "", "in:#design "},
		{"channel keeps typed words", "design", "deadline", "in:#design deadline"},
		{"already scoped here is left alone", "design", "in:#design deadline", "in:#design deadline"},
		{"another room's scope is replaced", "design", "in:#ops deadline", "in:#design deadline"},
		{"another room's scope with nothing else", "ops", "in:#design", "in:#ops "},
		{"direct message", "dm1", "", `in:"Camila Morales" `},
		{"group", "grp", "budget", `in:"Launch crew" budget`},
		{"other filters survive", "design", "from:@Camila Morales deadline", "in:#design from:@Camila Morales deadline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := issue3937Model()
			m.SelectedID = tc.selected
			if got := issue3937ScopedQuery(m, tc.current); got != tc.want {
				t.Fatalf("issue3937ScopedQuery(%q) = %q, want %q", tc.current, got, tc.want)
			}
		})
	}
}

func TestIssue3937_NoConversationStaysGlobal(t *testing.T) {
	m := issue3937Model()
	m.SelectedID = ""
	if got := issue3937ScopedQuery(m, "deadline"); got != "deadline" {
		t.Fatalf("no open conversation scoped the box: %q", got)
	}
	m.SelectedID = "gone"
	if got := issue3937ScopedQuery(m, ""); got != "" {
		t.Fatalf("an unknown conversation scoped the box: %q", got)
	}
}

func TestIssue3937_ScopedQueryReadsBackAsThatConversation(t *testing.T) {
	m := issue3937Model()
	for _, id := range []string{"design", "dm1", "grp"} {
		m.SelectedID = id
		_, got, _ := SearchFilters(m, issue3937ScopedQuery(m, "deadline"))
		if got != id {
			t.Fatalf("scoped query for %s resolves to %q", id, got)
		}
	}
}

func TestIssue3937_HeaderIconCarriesTheScopeAndSidebarStaysGlobal(t *testing.T) {
	m := chatux001Fixture("en-US", 0, 0, 0)
	header, _ := chatux001Header(t, m, handlers{}, 1440, "light")
	button := chatux001Button(t, header, "chat-search-open")
	extra := chatPolishAttr(button, "data-extra")
	if !strings.HasPrefix(extra, "in:") {
		t.Fatalf("header search icon does not carry a scope: data-extra=%q", extra)
	}
	if got := chatPolishAttr(button, "aria-description"); !strings.Contains(got, "#") {
		t.Fatalf("header search icon does not say where it will search: %q", got)
	}
	if got := chatPolishAttr(button, "aria-label"); got != "Search messages" {
		t.Fatalf("name changed: %q", got)
	}
}

func TestIssue3937_ScopeCopyInEveryLocale(t *testing.T) {
	m := issue3937Model()
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m.Locale = locale
		got := laneTextf(m, issue3937Copy, keyIssue3937Hint, map[string]string{"scope": "#design"})
		if got == "" || got == keyIssue3937Hint || !strings.Contains(got, "#design") {
			t.Fatalf("%s hint = %q", locale, got)
		}
	}
	m.Locale = "de-DE"
	if laneText(m, issue3937Copy, keyIssue3937Hint) == laneText(Model{Locale: "en-US"}, issue3937Copy, keyIssue3937Hint) {
		t.Fatal("de-DE falls back to English")
	}
}

func TestIssue3937_OfferChipCoversDirectAndGroup(t *testing.T) {
	for _, id := range []string{"dm1", "grp"} {
		m := issue3937Model()
		m.SelectedID = id
		markup := renderNode(t, searchFilterBar(m, "deadline"))
		if !strings.Contains(markup, `data-action="search-in-channel"`) {
			t.Fatalf("%s: no scope offer: %s", id, markup)
		}
	}
	m := issue3937Model()
	if markup := renderNode(t, searchFilterBar(m, "in:#design deadline")); strings.Contains(markup, `data-action="search-in-channel"`) {
		t.Fatal("offer still shown once scoped")
	}
}

// After the search ran, the scope chip names the conversation: not its id, and
// not "channel" for a direct or group chat.
func TestIssue3937_ScopeChipNamesTheConversation(t *testing.T) {
	m := Model{Locale: "en-US", Conversations: []Conversation{
		{ID: "c-1", Name: "design", Kind: PublicChannel},
		{ID: "c-2", Name: "Camila Morales", Kind: DirectMessage},
		{ID: "c-3", Name: "Launch crew", Kind: GroupChat},
	}}
	for _, tc := range []struct{ id, locale, want string }{
		{"c-1", "en-US", "Remove filter: In a channel #design"},
		{"c-2", "en-US", "Remove filter: In a conversation Camila Morales"},
		{"c-3", "en-US", "Remove filter: In a conversation Launch crew"},
		{"c-2", "de-DE", "Filter entfernen: In einer Unterhaltung Camila Morales"},
		{"c-2", "ar", "إزالة المرشح: في محادثة Camila Morales"},
	} {
		m.SelectedID = tc.id
		mm := m
		markup := renderNode(t, RenderChatSearch(tc.locale, ChatSearchView{Query: issue3937ScopedQuery(m, "deadline"), Context: &mm}))
		if !strings.Contains(markup, `aria-label="`+tc.want+`"`) {
			t.Errorf("%s/%s: chip is not %q in %s", tc.id, tc.locale, tc.want, markup)
		}
		if strings.Contains(markup, ` `+tc.id+`"`) || strings.Contains(markup, `value="`+tc.id+`"`) {
			t.Errorf("%s/%s: a raw conversation id is shown: %s", tc.id, tc.locale, markup)
		}
	}
}

// Two rooms can carry one name; the header scopes to the open one, in both of
// the resolvers the search reads the filter with.
func TestIssue3937_SameNamedRoomsScopeToTheOpenOne(t *testing.T) {
	m := Model{Locale: "en-US", Conversations: []Conversation{
		{ID: "dm-a", Name: "Alex Kim", Kind: DirectMessage},
		{ID: "dm-b", Name: "Alex Kim", Kind: DirectMessage},
	}}
	for _, open := range []string{"dm-a", "dm-b"} {
		m.SelectedID = open
		query := issue3937ScopedQuery(m, "budget")
		if _, got, _ := SearchFilters(m, query); got != open {
			t.Errorf("SearchFilters scoped %q to %q, want %q", query, got, open)
		}
		resolved, err := ResolveChatSearchQuery(m, query)
		if err != nil || !strings.Contains(resolved, "in:"+open) {
			t.Errorf("ResolveChatSearchQuery(%q) = %q, %v; want the filter to name %q", query, resolved, err, open)
		}
	}
}
