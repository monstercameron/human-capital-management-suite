package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
)

// TestTodo_CHATBUG_100_Integration renders Chat preferences as one panel: the
// quiet-hours, voice, reading-languages and emoji rows in that order, the
// voice row in its bounded loading shell (never a bare spinner), and the
// languages line agreeing with the Reading setting in every locale.
func TestTodo_CHATBUG_100_Integration(t *testing.T) {
	m := Model{State: StateReady, Locale: "en-US", CurrentUser: "alice", SelectedID: "general",
		Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel}},
		ChatFeatures:  &ChatFeatures{Voice: true, Renderings: true},
		Callbacks:     Callbacks{SavePreferences: func(Preferences) {}, SaveEmojiPrefs: func(string) {}},
	}
	rows := renderNode(t, html.Div(html.Props{}, chatbug045PanelRows(m, handlers{})...))
	quiet := strings.Index(rows, `data-prefs-section="quiet-hours"`)
	voice := strings.Index(rows, `data-prefs-section="voice"`)
	reading := strings.Index(rows, `chatrender-settings`)
	emoji := strings.Index(rows, `data-prefs-section="emoji-tone"`)
	if quiet < 0 || voice < 0 || reading < 0 || emoji < 0 || !(quiet < voice && voice < reading && reading < emoji) {
		t.Fatalf("preferences rows out of order: quiet=%d voice=%d reading=%d emoji=%d", quiet, voice, reading, emoji)
	}
	shell := rows[voice:]
	if !strings.Contains(shell, "Loading voice settings…") || !strings.Contains(shell, `aria-busy="true"`) {
		t.Fatal("the voice row has no loading shell")
	}
	// The shell's slow line and Try again are stylesheet-bound, so a read
	// that drags says so and offers the retry within its seconds.
	for _, want := range []string{"chat-slow", ">Try again<", "This is taking longer than usual."} {
		if !strings.Contains(Stylesheet, want) && !strings.Contains(shell, want) {
			t.Errorf("the loading shell lost %q", want)
		}
	}
	if !strings.Contains(rows[reading:], `class="chatux031-select"`) {
		t.Fatal("the reading row lost its end-aligned select")
	}
	for _, c := range []struct{ locale, setting, want string }{
		{"en-US", "en", "Your reading language is English."},
		{"de-DE", "de", "Ihre Lesesprache ist Deutsch."},
		{"ar", "ar", "لغة القراءة الخاصة بك هي العربية."},
	} {
		line := renderNode(t, RenderingLanguageStatusIndicatorWithReading(c.locale, nil, false, false, c.setting))
		if !strings.Contains(line, c.want) {
			t.Errorf("%s: languages line = %s", c.locale, line)
		}
	}
}
