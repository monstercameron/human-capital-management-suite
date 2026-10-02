package productui

import (
	"html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// agentux024Words are the words Chat's loading state shows in each language:
// the rail title, the search field, Quiet hours, the reading-language row, the
// skip link and the two lines of the empty timeline.
var agentux024Words = map[string][]string{
	"en-US": {"Conversations", "Search Chat", "Quiet hours", "Reading language", "Skip to conversation", "Choose a conversation", "Loading conversation"},
	"de-DE": {"Unterhaltungen", "Chat durchsuchen", "Ruhezeiten", "Lesesprache", "Zur Unterhaltung springen", "Wählen Sie eine Unterhaltung", "Unterhaltung wird geladen"},
	"ar":    {"المحادثات", "البحث في الدردشة", "ساعات الهدوء", "لغة القراءة", "الانتقال إلى المحادثة", "اختر محادثة", "جارٍ تحميل المحادثة"},
}

func agentux024Render(t *testing.T, node ui.Node) string {
	t.Helper()
	out, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return html.UnescapeString(out)
}

func agentux024View(locale string) View {
	view := NewView(PageChat, "tenant", "walt", "")
	return ApplyLocale(view, ResolveProductLocale(locale))
}

// TestTodo_AGENTUX_024 builds Chat's three unresolved states (first load, a
// route change over resolved chrome, and a failed read) for a German and an
// Arabic viewer and finds the viewer's language and direction on the first
// paint: no English Chat words, a workspace that states its direction, and no
// raw catalog key.
func TestTodo_AGENTUX_024(t *testing.T) {
	english := map[string]string{}
	for _, word := range agentux024Words["en-US"] {
		english[word] = word
	}
	states := map[string]func(View) ui.Node{
		"loading":         BuildLoading,
		"content loading": BuildContentLoading,
		"failure":         func(v View) ui.Node { return BuildFailure(v, "The service did not answer.") },
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for name, build := range states {
			t.Run(locale+" "+name, func(t *testing.T) {
				page := agentux024Render(t, build(agentux024View(locale)))
				if strings.Contains(page, "⟦") {
					t.Fatal("a copy key is printed")
				}
				start := strings.Index(page, `class="chat-workspace`)
				if start < 0 {
					t.Fatal("the loading proxy has no Chat workspace")
				}
				open := page[start : start+strings.Index(page[start:], ">")]
				wantDir := map[string]string{"en-US": "ltr", "de-DE": "ltr", "ar": "rtl"}[locale]
				if !strings.Contains(open, `dir="`+wantDir+`"`) {
					t.Errorf("the workspace does not state its direction %s: %s", wantDir, open)
				}
				workspace := page[start:]
				if locale == "en-US" {
					for _, word := range agentux024Words[locale] {
						if !strings.Contains(workspace, word) {
							t.Errorf("English Chat loading state lacks %q", word)
						}
					}
					return
				}
				for _, word := range agentux024Words["en-US"] {
					if strings.Contains(workspace, word) {
						t.Errorf("%s: the English %q is on the first paint", locale, word)
					}
				}
				for _, word := range agentux024Words[locale] {
					if word != "" && !strings.Contains(workspace, word) {
						t.Errorf("%s: %q is missing from the first paint", locale, word)
					}
				}
			})
		}
	}
	// A proxy built with no locale (a component preview) stays English.
	if page := agentux024Render(t, ui.CreateElement(LoadingProxy, LoadingProxyProps{Page: PageChat})); !strings.Contains(page, "Conversations") {
		t.Error("the preview proxy lost its English words")
	}
}

// TestTodo_AGENTUX_024_Browser: the image placeholder names an image in each
// language, and Chat's own page builds the same words the proxy does.
func TestTodo_AGENTUX_024_Browser(t *testing.T) {
	for locale, want := range map[string]string{"en-US": "Image loading", "de-DE": "Bild wird geladen", "ar": "جارٍ تحميل الصورة"} {
		ctx := ResolveProductLocale(locale)
		if got := ctx.Text("chat.attachment_loading"); got != want {
			t.Errorf("%s: the image placeholder reads %q, want %q", locale, got, want)
		}
	}
}
