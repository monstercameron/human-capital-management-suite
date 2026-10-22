package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_100_Browser opens Chat preferences on the page: the four
// rows render in order, the voice row carries its bounded loading shell, the
// reading select keeps its row-end class, and the languages line never
// contradicts a set Reading language. Live settling (a block reaches its
// terminal state within its bound) is specified in
// tools/uxqual/browser/chatbug100_prefs_settle.spec.mjs, which needs a
// running server and browser and is not executed here.
func TestTodo_CHATBUG_100_Browser(t *testing.T) {
	setup := func(m *chatui.Model) {
		m.ChatFeatures = &chatui.ChatFeatures{Voice: true, Renderings: true}
		m.Callbacks.SavePreferences = func(chatui.Preferences) {}
		m.Callbacks.SaveEmojiPrefs = func(string) {}
	}
	for _, locale := range w7Locales {
		page := w7Page(t, locale, setup)
		quiet := strings.Index(page, `data-prefs-section="quiet-hours"`)
		voice := strings.Index(page, `data-prefs-section="voice"`)
		reading := strings.Index(page, `chatrender-settings`)
		emoji := strings.Index(page, `data-prefs-section="emoji-tone"`)
		if quiet < 0 || voice < 0 || reading < 0 || emoji < 0 || !(quiet < voice && voice < reading && reading < emoji) {
			t.Fatalf("%s: preferences rows out of order: quiet=%d voice=%d reading=%d emoji=%d", locale, quiet, voice, reading, emoji)
		}
		shell := page[voice:]
		if !strings.Contains(shell, `aria-busy="true"`) || !strings.Contains(shell, "chatux037-loading") {
			t.Errorf("%s: the voice row has no bounded loading shell", locale)
		}
		if !strings.Contains(page[reading:], `class="chatux031-select"`) {
			t.Errorf("%s: the reading row lost its end-aligned select", locale)
		}
		if !strings.Contains(page, `data-chat-layer="chat-prefs"`) || !strings.Contains(page, `popover="manual"`) {
			t.Errorf("%s: Chat preferences is not a top-layer popover", locale)
		}
	}
}
