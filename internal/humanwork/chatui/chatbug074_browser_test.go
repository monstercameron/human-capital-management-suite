package chatui_test

import (
	"html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_CHATBUG_074_Browser draws a channel that has a to-do list and a poll,
// in the three languages, as a person opens it: the list and the poll have one
// home, the chips under the header, and the header carries no button of its own
// for either. No word is printed as a key.
func TestTodo_CHATBUG_074_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			ctx := productui.ResolveProductLocale(locale)
			m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "room", CurrentUser: "walt", CurrentTenantID: "t",
				Text:          func(key string) string { return ctx.Text(key) },
				Conversations: []chatui.Conversation{{ID: "room", Name: "scratch", Kind: chatui.PublicChannel, Joined: true, MemberCount: 2}},
				Messages:      []chatui.Message{{ID: "m1", AuthorID: "walt", Author: "Walt Brennan", Body: "Hello", Revision: 1}},
				ChannelPoll:   chatui.ChannelPoll{Revision: 4, Question: "Lunch spot?", Options: []chatui.ChannelPollOption{{ID: "a", Text: "Tacos"}, {ID: "b", Text: "Pho"}}},
				ChannelTodo: chatui.ChannelTodoList{Revision: 3, Items: []chatui.ChannelTodoItem{
					{ID: "open", Text: "Order pizza", CanToggle: true, CompletionMode: "EVERYONE"},
				}},
			}
			m.Callbacks.SendMessage = func(string, string) {}
			m.Callbacks.AddChannelTodo = func(string, string) {}
			m.Callbacks.OpenChannelTodo = func() {}
			m.Callbacks.OpenChannelPoll = func(string) {}
			rendered, err := ui.RenderToString(chatui.Build(m))
			if err != nil {
				t.Fatal(err)
			}
			page := html.UnescapeString(rendered)
			if strings.Contains(page, "⟦") {
				t.Fatalf("a copy key is printed: %s", page)
			}
			header := page[strings.Index(page, `<header`):]
			header = header[:strings.Index(header, `</header>`)]
			for _, gone := range []string{`data-action="open-todo"`, `data-action="open-poll"`, "channel-todo-trigger", "channel-poll-trigger"} {
				if strings.Contains(header, gone) {
					t.Errorf("the header still carries %s", gone)
				}
			}
			bar := page[strings.Index(page, `class="channel-tray-bar"`):]
			bar = bar[:strings.Index(bar, "</div>")]
			for _, want := range []string{`data-action="tray-todo"`, `data-action="tray-poll"`, "Lunch spot?"} {
				if !strings.Contains(bar, want) {
					t.Errorf("the chips under the header lack %s: %s", want, bar)
				}
			}
			if strings.Count(bar, "<button") != 2 {
				t.Errorf("the bar holds the list and the poll and nothing else: %s", bar)
			}
			if locale == "ar" && !strings.Contains(page, `dir="rtl"`) {
				t.Error("Arabic direction lost")
			}
		})
	}
}
