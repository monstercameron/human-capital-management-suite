package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatbug084Sidebar renders the whole Chat page with the product's own catalog
// and returns the arranged sections and the Archived section, when present.
func chatbug084Sidebar(t *testing.T, locale, selected string, wantArchived bool) (sections, archived string) {
	t.Helper()
	ctx := productui.ResolveProductLocale(locale)
	at := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: selected, CurrentUser: "walt", CurrentTenantID: "t",
		Text: func(key string) string { return ctx.Text(key) },
		Conversations: []chatui.Conversation{
			{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true},
			{ID: "people", Name: "people-ops", Kind: chatui.PublicChannel, Joined: true},
			{ID: "old", Name: "old-project", Kind: chatui.PublicChannel, Joined: true},
			{ID: "dm", Name: "Loretta Haynes", Kind: chatui.DirectMessage, Joined: true},
		},
		Messages:     []chatui.Message{{ID: "m1", AuthorID: "walt", Author: "Walt Brennan", SentAt: at, Body: "Hello", Revision: 1}},
		ChatFeatures: &chatui.ChatFeatures{Status: true},
		ChannelStatuses: map[string]chatui.ChannelStatusView{
			"old": {Status: chat.ChannelStatus{ConversationID: "old", Name: "old-project", Status: chatpolicy.StatusArchived}, Transitions: []chat.StatusTransition{{Status: chatpolicy.StatusOpen}}},
		},
		ChangeChannelStatus: func(chat.ChangeChannelStatusRequest) {},
	}
	m.Callbacks.SendMessage = func(string, string) {}
	m.Callbacks.SelectConversation = func(string) {}
	rendered, err := ui.RenderToString(chatui.Build(m))
	if err != nil {
		t.Fatal(err)
	}
	page := html.UnescapeString(rendered)
	if strings.Contains(page, "⟦") {
		t.Errorf("%s: the page prints a copy key: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(page))
	}
	start, end := strings.Index(page, `class="chat-rail`), strings.Index(page, `class="chat-main"`)
	if start < 0 || end < start {
		t.Fatalf("%s: the page has no sidebar before the conversation", locale)
	}
	rail := page[start:end]
	foot := strings.Index(rail, `data-section-id="archived"`)
	if foot < 0 {
		if wantArchived {
			t.Fatalf("%s: the Archived section is missing: %s", locale, rail)
		}
		return rail, ""
	}
	if !wantArchived {
		t.Fatalf("%s: the Archived section should be absent when the only archived conversation is selected: %s", locale, rail[foot:])
	}
	return rail[:foot], rail[foot:]
}

// chatbug084Row reports whether a part of the sidebar holds a row that opens
// the conversation.
func chatbug084Row(part, id string) bool {
	return regexp.MustCompile(`<button[^>]*class="chat-row[^"]*"[^>]*data-id="`+id+`"`).MatchString(part) ||
		regexp.MustCompile(`<button[^>]*data-id="`+id+`"[^>]*class="chat-row[^"]*"`).MatchString(part)
}

// TestTodo_CHATBUG_084_Browser draws the sidebar a person scans, in the three
// languages: an archived channel is under Archived and not among the channels,
// except while it is the conversation they have open, when its row stays in
// the list (selected) so they can see where they are.
func TestTodo_CHATBUG_084_Browser(t *testing.T) {
	for locale, archivedLabel := range map[string]string{"en-US": "Archived", "de-DE": "Archiviert", "ar": "مؤرشفة"} {
		sections, archived := chatbug084Sidebar(t, locale, "general", true)
		if chatbug084Row(sections, "old") || strings.Contains(sections, "old-project") {
			t.Errorf("%s: the archived channel is still listed among the channels", locale)
		}
		for _, id := range []string{"general", "people", "dm"} {
			if !chatbug084Row(sections, id) {
				t.Errorf("%s: the sidebar lost the open conversation %q", locale, id)
			}
		}
		if !strings.Contains(archived, archivedLabel) || !strings.Contains(archived, `aria-expanded="false"`) || !strings.Contains(archived, `class="section-count"`) {
			t.Errorf("%s: Archived is missing its collapsed heading or count: %s", locale, archived)
		}
		if strings.Contains(archived, "old-project") {
			t.Errorf("%s: a collapsed Archived section renders a hidden row: %s", locale, archived)
		}
		for _, name := range []string{"people-ops", "Loretta Haynes"} {
			if strings.Contains(archived, name) {
				t.Errorf("%s: %q, which is not archived, is under Archived", locale, name)
			}
		}

		// While the archived channel is open its selected row stays in its
		// arranged section and is removed from Archived.
		sections, archived = chatbug084Sidebar(t, locale, "old", false)
		if !chatbug084Row(sections, "old") {
			t.Errorf("%s: the open archived channel vanished from the list", locale)
		}
		row := regexp.MustCompile(`<button[^>]*data-id="old"[^>]*>`).FindString(sections)
		if !strings.Contains(row, "selected") {
			t.Errorf("%s: the open archived channel's row is not the selected one: %s", locale, row)
		}
		if archived != "" {
			t.Errorf("%s: the selected archived channel remains duplicated in Archived: %s", locale, archived)
		}
	}
}
