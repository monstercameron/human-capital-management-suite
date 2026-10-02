package chatui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// CHATBUG-007 clause table (the GREEN line, one row each):
//
//	the agent's icon, name and badge ........ TestTodo_CHATBUG_007 (three locales, two stored shapes)
//	the text with its lines ................. TestTodo_CHATBUG_007 (one list item per line)
//	linked sources .......................... TestTodo_CHATBUG_007 (one link, to the guide)
//	no tag, no data, no "Linked document" ... TestTodo_CHATBUG_007, TestTodo_CHATBUG_007_Browser
//	every other place the text is shown ..... TestTodo_CHATBUG_007_Browser (Saved, search results)
//	"who it was posted for" ................. removed by the owner (AGENTUX-072); asserted absent
//	past dates not listed as upcoming ....... the model's instruction and the evaluation case,
//	                                          TestAgentUXProactiveLive_PostedMessage in internal/application
const (
	chatbug007Text = "Upcoming company holidays remaining in 2026:\n- Thanksgiving Day — Nov 26\n- Day after Thanksgiving — Nov 27\n- Christmas Day — Dec 25"
	chatbug007Href = "/workspace/app/docs?document=doc-10c773e5"
)

// chatbug007Wires are the two shapes the announcement is stored in: the encoded
// envelope new posts carry, and the plain JSON envelope the first announcement
// on the served product was stored as.
func chatbug007Wires(t *testing.T) map[string]string {
	t.Helper()
	message := AgentAnnouncementMessage{AgentName: "Agent", OwnerName: "Walt Brennan", Text: chatbug007Text, PostedAt: time.Date(2026, 10, 1, 20, 30, 26, 0, time.UTC), Sources: []AgentAnnouncementSource{{Title: "2026 holiday guide", Href: chatbug007Href}}}
	encoded, err := AnnouncementMessageBody(message)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{"encoded": encoded, "stored envelope": AgentAnnouncementBodyPrefix + string(plain)}
}

func chatbug007Model(locale, body string) Model {
	icon := AgentIconFixture(agenticon.Input{Name: "Assistant", Instructions: "Announce upcoming holidays"})
	return Model{Locale: locale, State: StateReady, SelectedID: "general", CurrentUser: "walt", CurrentTenantID: "tenant",
		Conversations:           []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, Joined: true}},
		Messages:                []Message{{ID: "posted", AuthorID: "assistant", Author: "Hcmnext Local Persona Assistant", Body: body, SentAt: time.Date(2026, 10, 1, 20, 30, 26, 0, time.UTC)}},
		ResolvedPersonaMentions: []ResolvedPersonaMention{{Reference: ChatReference{Kind: "AGENT_MENTION", ID: "assistant", Display: "Assistant", TenantID: "tenant"}, Icon: icon, IconRevision: 4, Version: "2", Handle: "assistant"}}}
}

// chatbug007Raw is what a reader must never see of an announcement.
var chatbug007Raw = []string{"hcm_agent_announcement", "AgentName", "OwnerName", "&#34;Text&#34;", "{&#34;", "Hcmnext Local Persona Assistant", "Linked document", "0001-01-01", "v2:"}

func TestTodo_CHATBUG_007(t *testing.T) {
	for shape, wire := range chatbug007Wires(t) {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			markup := render(t, chatbug007Model(locale, wire))
			// The agent's icon, name and badge, not the initials of its internal name.
			for _, want := range []string{"agent-icon", "agent-badge", "Assistant"} {
				if !strings.Contains(markup, want) {
					t.Fatalf("%s/%s: the message lacks %q: %s", shape, locale, want, markup)
				}
			}
			if strings.Contains(markup, ">HA<") {
				t.Fatalf("%s/%s: the author is drawn as the initials of its internal name: %s", shape, locale, markup)
			}
			// The text keeps its lines: the heading line, then one list item each.
			if strings.Count(markup, "<li>") != 3 {
				t.Fatalf("%s/%s: %d list items, want the three holidays: %s", shape, locale, strings.Count(markup, "<li>"), markup)
			}
			for _, line := range []string{"Upcoming company holidays remaining in 2026", "Thanksgiving Day — Nov 26", "Day after Thanksgiving — Nov 27", "Christmas Day — Dec 25"} {
				if !strings.Contains(markup, line) {
					t.Fatalf("%s/%s: the line %q is missing: %s", shape, locale, line, markup)
				}
			}
			// One linked source, to the guide, named by its title.
			if strings.Count(markup, `href="`+chatbug007Href+`"`) != 1 || !strings.Contains(markup, "2026 holiday guide") {
				t.Fatalf("%s/%s: the source is not one link to the guide: %s", shape, locale, markup)
			}
			for _, raw := range chatbug007Raw {
				if strings.Contains(markup, raw) {
					t.Fatalf("%s/%s: the message prints %q: %s", shape, locale, raw, markup)
				}
			}
			// The owner removed the "posted for" line (AGENTUX-072): it is not drawn.
			if strings.Contains(markup, "Walt Brennan") || strings.Contains(markup, "agent-announcement-attribution") {
				t.Fatalf("%s/%s: the message names who it was posted for: %s", shape, locale, markup)
			}
		}
	}
}

// The announcement is read as its sentence wherever its text is shown: in the
// Saved panel and in search results, never as the tag and its data.
func TestTodo_CHATBUG_007_Browser(t *testing.T) {
	for shape, wire := range chatbug007Wires(t) {
		// Saved: drawn the way the conversation draws it, under the agent's name.
		view := chatsave002View("todo", []SavedMessageRow{{TenantID: "tenant", ConversationID: "general", PostID: "posted", AuthorID: "assistant", Author: "Hcmnext Local Persona Assistant", Channel: "general", InChannel: true, SentAt: chatsave002Now.Add(-time.Hour), Body: wire, Availability: "readable", Sequence: 4, Revision: 1}})
		view.Model = chatbug007Model("en-US", wire)
		saved := renderNode(t, RenderSavedMessages(view))
		for _, want := range []string{"Thanksgiving Day", "Christmas Day", "2026 holiday guide"} {
			if !strings.Contains(chatbug021Visible(saved), want) {
				t.Fatalf("%s: the Saved item lacks %q: %s", shape, want, saved)
			}
		}
		for _, raw := range chatbug007Raw {
			if strings.Contains(saved, raw) {
				t.Fatalf("%s: the Saved item prints %q: %s", shape, raw, saved)
			}
		}

		// Search: the row shows the sentence, not the data.
		results := renderNode(t, RenderChatSearch("en-US", ChatSearchView{Query: "holiday", Response: chatsearch.Response{Groups: []chatsearch.Group{{Kind: chatsearch.Message, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.Message, ID: "posted", Text: wire, AuthorID: "assistant", Target: chatsearch.Target{ConversationID: "general", MessageID: "posted"}}}}}}}))
		// The query word is wrapped in a highlight, so the line is read around it.
		if visible := chatbug021Visible(results); !strings.Contains(visible, "remaining in 2026") || !strings.Contains(visible, "Christmas Day") {
			t.Fatalf("%s: the search result lost its sentence: %s", shape, results)
		}
		for _, raw := range chatbug007Raw {
			if strings.Contains(results, raw) {
				t.Fatalf("%s: the search result prints %q: %s", shape, raw, results)
			}
		}
	}

	// First paint, before the agent directory has answered: the same message is
	// still an announcement under a neutral agent label, never the tag.
	for shape, wire := range chatbug007Wires(t) {
		model := chatbug007Model("en-US", wire)
		model.ResolvedPersonaMentions = nil
		markup := render(t, model)
		if !strings.Contains(markup, "<li>") || !strings.Contains(markup, "2026 holiday guide") {
			t.Fatalf("%s: the first paint lost the announcement: %s", shape, markup)
		}
		for _, raw := range chatbug007Raw {
			if strings.Contains(markup, raw) {
				t.Fatalf("%s: the first paint prints %q: %s", shape, raw, markup)
			}
		}
	}
}
