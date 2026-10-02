package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_CHATUX_004_Browser renders the composer with the product's own
// catalog, the one that answers a key it does not hold with a bracketed marker,
// and finds no marker anywhere: not in the text, the tooltips or the accessible
// names of the Add menu, the + @ Aa buttons or the open "/" list, in en-US,
// de-DE and ar.
func TestTodo_CHATUX_004_Browser(t *testing.T) {
	want := map[string]struct{ add, poll, note, mention, format, commands, giphy string }{
		"en-US": {"Add to your message", "Poll", "Ask the channel a question with answer options", "Mention someone", "Formatting", "Commands", "Search for a GIF and post it"},
		"de-DE": {"Zur Nachricht hinzufügen", "Umfrage", "Dem Kanal eine Frage mit Antwortoptionen stellen", "Jemanden erwähnen", "Formatierung", "Befehle", "Ein GIF suchen und senden"},
		"ar":    {"إضافة إلى الرسالة", "استطلاع", "اطرح على القناة سؤالًا مع خيارات للإجابة", "الإشارة إلى شخص", "التنسيق", "الأوامر", "ابحث عن صورة GIF وانشرها"},
	}
	for locale, words := range want {
		ctx := productui.ResolveProductLocale(locale)
		m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), CurrentUser: "alice", SelectedID: "general",
			Text:          func(key string) string { return ctx.Text(key) },
			Conversations: []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true}},
			Callbacks:     chatui.Callbacks{SendMessage: func(string, string) {}, OpenChannelPoll: func(string) {}, OpenChannelTodo: func() {}},
		}
		for _, open := range []bool{false, true} {
			markup := html.UnescapeString(chatui.ComposerMarkupForTest(t, m, "", open))
			if strings.Contains(markup, "⟦") {
				t.Errorf("%s open=%v: the composer prints a copy key: %s", locale, open, regexp.MustCompile(`.{40}⟦[^⟧]*⟧`).FindString(markup))
			}
			for _, text := range []string{words.add, words.poll, words.note, words.mention, words.format} {
				if !strings.Contains(markup, text) {
					t.Errorf("%s open=%v: the composer misses %q", locale, open, text)
				}
			}
			if open {
				for _, text := range []string{words.commands, words.giphy} {
					if !strings.Contains(markup, text) {
						t.Errorf("%s: the open command list misses %q", locale, text)
					}
				}
			}
		}
		// Tooltips and accessible names of the three buttons are real words.
		markup := html.UnescapeString(chatui.ComposerMarkupForTest(t, m, "", false))
		for _, class := range []string{"composer-add-trigger", "composer-mention-button", "composer-format-toggle"} {
			button := regexp.MustCompile(`<button[^>]*class="[^"]*` + class + `[^"]*"[^>]*>`).FindString(markup)
			if button == "" || !strings.Contains(button, `aria-label="`) || !strings.Contains(button, `title="`) || strings.Contains(button, "chat.composer") {
				t.Errorf("%s: %s lacks a real name or tooltip: %s", locale, class, button)
			}
		}
	}
}
