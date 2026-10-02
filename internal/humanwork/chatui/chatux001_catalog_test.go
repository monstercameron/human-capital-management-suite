package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// The product catalog answers a key it does not hold with a ⟦key⟧ marker; the
// catalog the native tests use does not. This renders the conversation header,
// the sidebar search box and the grouped results with the real catalog in all
// three languages: no marker or copy key may reach the page, and each name is the
// one the language's table holds.
func TestTodo_CHATUX_001_Browser_RealCatalog(t *testing.T) {
	type words struct{ search, pinned, members, conversations, people, messages string }
	for locale, want := range map[string]words{
		"en-US": {"Search Chat", "Pinned messages (2)", "Members (18)", "Conversations", "People", "Messages"},
		"de-DE": {"Chat durchsuchen", "Angeheftete Nachrichten (2)", "Mitglieder (18)", "Unterhaltungen", "Personen", "Nachrichten"},
		"ar":    {"البحث في الدردشة", "الرسائل المثبتة (٢)", "الأعضاء (١٨)", "المحادثات", "الأشخاص", "الرسائل"},
	} {
		t.Run(locale, func(t *testing.T) {
			ctx := productui.ResolveProductLocale(locale)
			room := chatui.Conversation{ID: "general", Name: "general", Kind: chatui.PublicChannel, MemberCount: 18, Joined: true}
			m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: room.ID, CurrentUser: "ari", CurrentTenantID: "t",
				Text:          func(key string) string { return ctx.Text(key) },
				Conversations: []chatui.Conversation{room},
				ChannelPins:   []chatui.ChannelPin{{PostID: "p1", Author: "Ari", Body: "One", Sequence: 1}, {PostID: "p2", Author: "Ari", Body: "Two", Sequence: 2}},
				ChannelTodo:   chatui.ChannelTodoList{Revision: 1, Items: []chatui.ChannelTodoItem{{ID: "a", Text: "Open"}}},
				Messages:      []chatui.Message{{ID: "m", AuthorID: "ari", Author: "Ari", Body: "Hello", TimeLabel: "9:30", SentAt: time.Now().Add(-time.Minute)}},
				Callbacks:     chatui.Callbacks{Search: func(string) {}, ToggleDetails: func(bool) {}, OpenChannelTodo: func() {}, SelectConversation: func(string) {}},
			}
			page := func(m chatui.Model) string {
				markup, err := ui.RenderToString(chatui.Build(m))
				if err != nil {
					t.Fatal(err)
				}
				return html.UnescapeString(markup)
			}
			noKeys := func(name, markup string) {
				if strings.Contains(markup, "⟦") || strings.Contains(markup, "chat.ux001") || strings.Contains(markup, "chat.search") {
					t.Fatalf("%s prints a copy key: %s", name, regexp.MustCompile(`.{30}(⟦[^⟧]*⟧|chat\.(ux001|search)[a-z._]*)`).FindString(markup))
				}
			}

			header := page(m)
			noKeys("the page", header)
			for _, text := range []string{
				`placeholder="` + want.search + `"`,
				`aria-label="` + want.search + `"`,
				`aria-label="` + want.pinned + `"`,
				`aria-label="` + want.members + `"`,
				`>Ctrl+K</kbd>`,
			} {
				if !strings.Contains(header, text) {
					t.Errorf("the header or the search box lacks %s", text)
				}
			}
			if !strings.Contains(header, `title="`+want.search+` (Ctrl+K)"`) {
				t.Errorf("the search button's tooltip is not %q with its key", want.search)
			}

			// Grouped results, with the same catalog.
			m.Search = "holiday"
			view := chatui.ChatSearchView{Query: "holiday", Response: chatsearch.Response{Groups: []chatsearch.Group{
				{Kind: chatsearch.Conversation, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.Conversation, ID: "c", Text: "holiday-planning"}}},
				{Kind: chatsearch.Person, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.Person, ID: "p", Text: "Holiday Ho"}}},
				{Kind: chatsearch.Message, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.Message, ID: "x", Text: "The holiday schedule"}}},
			}}}
			m.ChatSearch = &view
			results := page(m)
			noKeys("the results", results)
			for _, name := range []string{want.conversations, want.people, want.messages} {
				if !strings.Contains(results, "<h3>"+name+"</h3>") {
					t.Errorf("no result group named %q", name)
				}
			}
		})
	}
}
