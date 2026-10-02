package chatui

import "testing"

// TestTodo_CHATBUG_052 holds what the tab says: the conversation that is open,
// and the Moderation page, search results and Saved panel when one of them is
// what the page shows.
func TestTodo_CHATBUG_052(t *testing.T) {
	m := Model{Locale: "en-US", SelectedID: "random", Moderation: ModerationState{Ready: true, Moderator: true},
		Conversations: []Conversation{
			{ID: "random", Name: "random", Kind: PublicChannel},
			{ID: "policy", Name: "Policy Helper", Kind: DirectMessage, Agent: true, AgentID: "policy-helper"},
		}}
	for _, c := range []struct{ page, query, want string }{
		{"", "", "#random"},
		{ChatPageModeration, "", "Moderation"},
		{ChatPageSaved, "", "Saved"},
		{ChatPageSearch, " open enrollment ", "Search: open enrollment"},
	} {
		if got := ChatTabTitle(m, c.page, c.query); got != c.want {
			t.Errorf("page %q reads %q, want %q", c.page, got, c.want)
		}
	}
	m.SelectedID = "policy"
	if got := ChatTabTitle(m, "", ""); got != "Policy Helper" {
		t.Errorf("a direct message reads %q", got)
	}
	m.SelectedID = "random"
	if got := ChatTabTitle(m, "", ""); got != "#random" {
		t.Errorf("leaving the direct message left %q on the tab", got)
	}
	m.Moderation.Moderator = false
	if got := ChatTabTitle(m, ChatPageModeration, ""); got != "Notices about your messages" {
		t.Errorf("a person with notices only reads %q", got)
	}
	m.Locale = "de-DE"
	if got := ChatTabTitle(m, ChatPageSearch, "Urlaub"); got != "Suchen: Urlaub" {
		t.Errorf("German search title %q", got)
	}
	if got := ChatTabTitle(Model{}, "", ""); got != "" {
		t.Errorf("nothing open names %q", got)
	}
}
