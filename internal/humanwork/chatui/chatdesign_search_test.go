//go:build !(js && wasm)

package chatui

import (
	"strings"
	"testing"
)

// #3775: Chat's own search says it finds messages, in every locale, wherever
// it is named: the list's field, its label and the header button.
func TestChatDesign_3775_ChatSearchSaysItSearchesMessages(t *testing.T) {
	for locale, want := range map[string]struct{ name, old string }{
		"en-US": {"Search messages", "Search Chat"},
		"de-DE": {"Nachrichten durchsuchen", "Chat durchsuchen"},
		"ar":    {"البحث في الرسائل", "البحث في الدردشة"},
	} {
		m := chat4Fixture(locale, "sent", false)
		if got := chatux001Text(m, "chat.ux001.search"); got != want.name {
			t.Errorf("%s: Chat's search is named %q, want %q", locale, got, want.name)
		}
		if hint := chatux010Hint(m); !strings.HasPrefix(hint, want.name+" (") {
			t.Errorf("%s: the shortcut hint %q does not start with the search's name", locale, hint)
		}
		page := render(t, m)
		if !strings.Contains(page, `placeholder="`+want.name+`"`) && !strings.Contains(page, `aria-label="`+want.name+`"`) {
			t.Errorf("%s: the rendered Chat does not name its search %q", locale, want.name)
		}
		if strings.Contains(page, want.old) {
			t.Errorf("%s: the rendered Chat still says %q", locale, want.old)
		}
	}
}
