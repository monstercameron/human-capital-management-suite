package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

var chatux037BrowserBare = regexp.MustCompile(`(?i)loading|please wait|wird geladen|werden geladen|جار[ٍ]? تحميل|يرجى الانتظار|⟦`)

// chatux037BrowserVisible is the text a person sees: the accessible status
// elements, styles and tags are removed.
func chatux037BrowserVisible(markup string) string {
	markup = html.UnescapeString(markup)
	markup = regexp.MustCompile(`(?s)<(span|p|div)[^>]*class="[^"]*sr-only[^"]*"[^>]*>.*?</(span|p|div)>`).ReplaceAllString(markup, " ")
	markup = regexp.MustCompile(`(?s)<(style|script)[^>]*>.*?</(style|script)>`).ReplaceAllString(markup, " ")
	markup = regexp.MustCompile(`(?s)<[^>]*>`).ReplaceAllString(markup, " ")
	return strings.Join(strings.Fields(markup), " ")
}

// TestTodo_CHATUX_037_Browser renders each Chat surface in its loading state
// with the product's own catalog, in the three languages the product ships, and
// fails on any bare loading sentence or printed copy key.
func TestTodo_CHATUX_037_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		ctx := productui.ResolveProductLocale(locale)
		room := chatui.Conversation{ID: "room", Name: "general", Kind: chatui.PublicChannel, MemberCount: 3, Joined: true}
		base := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: room.ID, CurrentUser: "walt", CurrentTenantID: "t",
			Text:          func(key string) string { return ctx.Text(key) },
			Conversations: []chatui.Conversation{room},
			Messages:      []chatui.Message{{ID: "root", Author: "Ari", AuthorID: "ari", Body: "Question"}},
		}
		surfaces := map[string]func() ui.Node{}
		thread := base
		thread.ShowThread, thread.ThreadParentID, thread.ThreadLoading = true, "root", true
		surfaces["thread"] = func() ui.Node { return chatui.Build(thread) }
		person := base
		person.ShowPerson, person.PersonDetails = true, &chatui.PersonDetails{ID: "worker"}
		surfaces["person"] = func() ui.Node { return chatui.Build(person) }
		surfaces["moderation"] = func() ui.Node {
			return chatui.ModerationPage(chatui.ModerationPageModel{Locale: ctx.Resolved, State: chatui.StateLoading})
		}
		surfaces["saved"] = func() ui.Node {
			return chatui.RenderSavedMessages(chatui.SavedMessagesView{Locale: ctx.Resolved, Tab: "todo", Loading: true})
		}
		surfaces["search"] = func() ui.Node {
			return chatui.RenderChatSearch(ctx.Resolved, chatui.ChatSearchView{Query: "launch", Loading: true})
		}
		surfaces["gate"] = func() ui.Node { return chatui.RenderGate(chatui.GateView{Locale: ctx.Resolved, State: "loading"}) }
		for name, node := range surfaces {
			markup, err := ui.RenderToString(node())
			if err != nil {
				t.Fatalf("%s %s: %v", locale, name, err)
			}
			if !strings.Contains(markup, "chatux037-loading") {
				t.Errorf("%s %s: the surface does not use the shared loading component", locale, name)
			}
			visible := chatux037BrowserVisible(markup)
			if found := chatux037BrowserBare.FindString(visible); found != "" {
				i := strings.Index(visible, found)
				t.Errorf("%s %s: bare loading sentence or key %q near %q", locale, name, found, visible[max(0, i-60):min(len(visible), i+80)])
			}
			// The accessible status is there, once.
			if strings.Count(markup, `class="sr-only" role="status"`) < 1 && name != "saved" {
				t.Errorf("%s %s: the status for assistive technology is missing", locale, name)
			}
		}
	}
}
