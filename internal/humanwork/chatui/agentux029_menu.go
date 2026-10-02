package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AGENTUX-029 and the page check of 2026-10-02 (finding 9): the "@" menu groups
// the people it offers under "In this conversation" and "Not in this
// conversation", and which group a person belongs to is only known once the
// conversation's members have been read. Until then the menu used to put every
// person from the directory under "Not in this conversation" and move the
// members up when the list arrived, under the cursor. Now the menu names the
// group it can fill (the people already seen here), says quietly that the
// members are on their way, and offers no one from the directory until it can
// say which group they are in. The highlighted row follows its option when rows
// arrive instead of staying at its place.

const (
	keyAgentUX029PeopleLoading = "chat.agentux029.people_loading"
	keyAgentUX029PeopleFailed  = "chat.agentux029.people_failed"
)

var agentUX029Copy = map[string]map[string]string{
	"en-US": {
		keyAgentUX029PeopleLoading: "Loading who is in this conversation…",
		keyAgentUX029PeopleFailed:  "Could not load who is in this conversation.",
	},
	"de-DE": {
		keyAgentUX029PeopleLoading: "Die Mitglieder dieser Unterhaltung werden geladen…",
		keyAgentUX029PeopleFailed:  "Die Mitglieder dieser Unterhaltung konnten nicht geladen werden.",
	},
	"ar": {
		keyAgentUX029PeopleLoading: "جارٍ تحميل أعضاء هذه المحادثة…",
		keyAgentUX029PeopleFailed:  "تعذّر تحميل أعضاء هذه المحادثة.",
	},
}

func agentUX029Text(m Model, key string) string {
	return chatbug039Text(key, agentUX029Copy[chatEmojiLocale(m.Locale)][key], agentUX029Copy["en-US"][key])
}

// mentionPeopleLoading reports that the conversation's members have been asked
// for and have not arrived. A loaded list is never empty (the viewer is in
// it), and a page that cannot load members at all (no callback) has nothing to
// wait for.
func mentionPeopleLoading(m Model) bool {
	return len(m.Members) == 0 && m.Callbacks.LoadMembers != nil && !m.selected().Agent
}

// mentionPeopleLoadingRow is the quiet line under the people group while the
// members load, nil when they have.
func mentionPeopleLoadingRow(m Model) ui.Node {
	if !mentionPeopleLoading(m) {
		return nil
	}
	if m.MembersFailed {
		// The read failed: say so and offer the retry, rather than a loading
		// line that never ends.
		return html.Div(html.Props{Class: "mention-agent-state error members-failed", Role: "status", Aria: map[string]string{"live": "assertive"}},
			html.P(html.Props{Text: agentUX029Text(m, keyAgentUX029PeopleFailed)}),
			html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "mention-members-retry"}, Text: mentionMenuText(m, KeyMentionAgentsRetry)}))
	}
	return chatux037MenuLoading(m, "loading people", agentUX029Text(m, keyAgentUX029PeopleLoading), "mention-members-retry")
}

// mentionOptionKey names an option by what it is, not by where it stands.
func mentionOptionKey(option mentionOption) string {
	switch {
	case option.person != nil:
		return "person:" + option.person.ID
	case option.persona != nil:
		return "agent:" + option.persona.Reference.TenantID + ":" + option.persona.Reference.ID
	}
	return ""
}

// SettleActive keeps the highlighted option the same option when rows arrive
// above it. It runs while the workspace renders and changes the store before
// the render reads it, like ForConversation, asking for no second render. A
// highlight the person moved (arrow keys, Tab on a row) is taken as it stands.
func (s mentionStore) SettleActive(m Model) {
	if s.box == nil || !s.box.state.Open {
		return
	}
	state := s.box.state
	options, _ := mentionOptionsForState(m, state.Query, state.Target, state.ShowAllPeople)
	if len(options) == 0 {
		return
	}
	if state.ActiveKey != "" && state.Active == state.SettledAt {
		for i, option := range options {
			if mentionOptionKey(option) == state.ActiveKey {
				state.Active = i
				break
			}
		}
	}
	if state.Active >= len(options) {
		state.Active = len(options) - 1
	}
	if state.Active < 0 {
		state.Active = 0
	}
	state.ActiveKey, state.SettledAt = mentionOptionKey(options[state.Active]), state.Active
	s.box.state = state
}
