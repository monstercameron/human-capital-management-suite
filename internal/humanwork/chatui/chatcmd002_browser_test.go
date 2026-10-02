package chatui_test

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"strings"
	"testing"
	"time"
)

func chatcmd002CatalogModel(locale string) chatui.Model {
	ctx := productui.ResolveProductLocale(locale)
	return chatui.Model{SelectedID: "room", Locale: ctx.Resolved, Text: func(key string) string { return ctx.Text(key) }}
}
func chatcmd002AssertCatalog(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(markup, "⟦⟧") || strings.Contains(markup, "chatcmd002_") || strings.Contains(markup, "chatcmd003_") {
		t.Fatalf("raw key in %s", markup)
	}
	return markup
}
func TestTodo_CHATCMD_002_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			m := chatcmd002CatalogModel(locale)
			d, _ := chat.Chatcmd003ParsePoll(`"Where?" 1="Here" 2="There"`, time.Now(), nil)
			markup := chatcmd002AssertCatalog(t, chatui.Chatcmd002RenderCard(m, "post", chat.Chatcmd002View{Card: d.Card, ResultsVisible: true}, false))
			if !strings.Contains(markup, "Where?") || !strings.Contains(markup, "<progress") {
				t.Fatal("poll not rendered")
			}
			if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
				t.Fatal("Arabic direction lost")
			}
		})
	}
}

// TestTodo_CHATBUG_057_Browser renders, with the product's own catalog in the
// three languages, every state of the card a reader meets: a named poll with
// votes, an anonymous poll before and after the ballot, a list with a finished
// task, and the preview of a loosely typed poll with its tidy changes. No state
// may print a key in place of its words.
func TestTodo_CHATBUG_057_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			m := chatcmd002CatalogModel(locale)
			m.Chatcmd002.Mutate = func(string, uint64, chat.Chatcmd002Mutation) {}
			named, _ := chat.Chatcmd003ParsePoll(`"Where?" 1="Here" 2="There" multiple=yes closes=2030-01-02`, time.Now(), chat.Chatcmd004ResolveDate)
			named.Card.Poll.Options[0].ID, named.Card.Poll.Options[1].ID = "a", "b"
			named.Card.Poll.Options[0].Count = 2
			view := chat.Chatcmd002View{Card: named.Card, ResultsVisible: true, CanManage: true, MyOptions: []string{"a"}, Voted: true, Notice: "conflict", Voters: map[string][]chat.Chatcmd002Voter{"a": {{HomeTenantID: "t", SubjectID: "gone"}}}}
			markup := chatcmd002AssertCatalog(t, chatui.Chatcmd002RenderCard(m, "post", view, false))
			for _, want := range []string{`data-action="chatcmd002-vote"`, `aria-pressed="true"`, `data-action="chatcmd002-close"`, `role="alert"`, s31Digits(locale, "2 · 100%"), "<progress"} {
				if !strings.Contains(markup, want) {
					t.Errorf("named poll lacks %s", want)
				}
			}
			if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
				t.Error("Arabic direction lost")
			}
			secret := named.Card
			secret.Poll = &chat.Chatcmd002Poll{Options: named.Card.Poll.Options, Anonymous: true, Results: "after-voting", AddOptions: "author"}
			before := chatcmd002AssertCatalog(t, chatui.Chatcmd002RenderCard(m, "post", chat.Chatcmd002View{Card: secret}, false))
			if !strings.Contains(before, `data-action="chatcmd002-cast"`) || !strings.Contains(before, `type="radio"`) || strings.Contains(before, "<progress") {
				t.Errorf("anonymous poll before the ballot: %s", before)
			}
			after := chatcmd002AssertCatalog(t, chatui.Chatcmd002RenderCard(m, "post", chat.Chatcmd002View{Card: secret, Voted: true, ResultsVisible: true}, false))
			if strings.Contains(after, `data-action="chatcmd002-cast"`) || !strings.Contains(after, "<progress") {
				t.Errorf("anonymous poll after the ballot: %s", after)
			}
			list, _ := chat.Chatcmd004ParseTodo(`"Launch" 1="Send invites" 2="Book the room"`, nil, "me", time.Now(), chat.Chatcmd004ResolveDate)
			list.Card.Todo.Items[0].ID, list.Card.Todo.Items[1].ID = "one", "two"
			list.Card.Todo.Items[0].Completed, list.Card.Todo.Items[0].CompletedBySubjectID, list.Card.Todo.Items[0].CompletedAtUnix = true, "gone", time.Now().Unix()
			todo := chatcmd002AssertCatalog(t, chatui.Chatcmd002RenderCard(m, "post", chat.Chatcmd002View{Card: list.Card, CanTick: map[string]bool{"one": true, "two": true}, CanManage: true}, false))
			if strings.Count(todo, `role="checkbox"`) != 2 || !strings.Contains(todo, `aria-checked="true"`) || !strings.Contains(todo, `data-extra="CLOSE"`) {
				t.Errorf("to-do card: %s", todo)
			}
			loose, _ := chat.Chatcmd003ParsePoll("where for lunch? tacos, pho or pizza", time.Now(), chat.Chatcmd004ResolveDate)
			preview := chatcmd002AssertCatalog(t, chatui.Chatcmd003RenderPreview(m, chat.Chatcmd003Tidy(loose)))
			for _, want := range []string{"Where for lunch?", "<del>tacos</del>", `data-action="chatcmd003-original"`, `id="chatcmd003-post"`, `id="chatcmd003-anonymous"`, `role="switch"`} {
				if !strings.Contains(preview, want) {
					t.Errorf("preview lacks %s", want)
				}
			}
		})
	}
}

func TestTodo_CHATCMD_003_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			m := chatcmd002CatalogModel(locale)
			d, _ := chat.Chatcmd003ParsePoll(`"Where?" 1="Here" 2="There"`, time.Now(), nil)
			markup := chatcmd002AssertCatalog(t, chatui.Chatcmd003RenderPreview(m, d))
			if !strings.Contains(markup, `data-action="chatcmd003-post"`) || !strings.Contains(markup, `aria-labelledby="chatcmd003-results-label"`) || !strings.Contains(markup, `data-id="closes"`) {
				t.Fatal("poll preview missing")
			}
		})
	}
}
func TestTodo_CHATCMD_004_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			m := chatcmd002CatalogModel(locale)
			d, _ := chat.Chatcmd004ParseTodo(`"Launch" 1="Send invites"`, nil, "me", time.Now(), chat.Chatcmd004ResolveDate)
			markup := chatcmd002AssertCatalog(t, chatui.Chatcmd003RenderPreview(m, d))
			if !strings.Contains(markup, "Send invites") || !strings.Contains(markup, `role="checkbox"`) || !strings.Contains(markup, `aria-labelledby="chatcmd003-tick-label"`) {
				t.Fatal("todo preview missing")
			}
		})
	}
}
