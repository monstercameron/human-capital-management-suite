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
func TestTodo_CHATCMD_003_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			m := chatcmd002CatalogModel(locale)
			d, _ := chat.Chatcmd003ParsePoll(`"Where?" 1="Here" 2="There"`, time.Now(), nil)
			markup := chatcmd002AssertCatalog(t, chatui.Chatcmd003RenderPreview(m, d))
			if !strings.Contains(markup, `data-action="chatcmd003-post"`) || !strings.Contains(markup, `for="chatcmd003-results"`) {
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
			if !strings.Contains(markup, "Send invites") || !strings.Contains(markup, `role="checkbox"`) || !strings.Contains(markup, `for="chatcmd003-tick"`) {
				t.Fatal("todo preview missing")
			}
		})
	}
}
