package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// TestTodo_CHATBUG_097_Browser opens a thread in each shipped language: the
// heading names the thread and its channel, the bell says what it does, the
// parent is whole with the replies under it, the composer is ready, and no
// element id appears twice.
func TestTodo_CHATBUG_097_Browser(t *testing.T) {
	notify := map[string]string{"en-US": "Notify me about replies", "de-DE": "Über Antworten benachrichtigen", "ar": "أبلغني بالردود"}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := Model{Locale: locale, SelectedID: "room", CurrentUser: "eve",
			Conversations: []Conversation{{ID: "room", Name: "General", Kind: PublicChannel, Joined: true, MemberCount: 18}},
			Messages:      []Message{{ID: "p1", AuthorID: "hans", Author: "Hans", Body: "Shall we?", TimeLabel: "09:41", Replies: 6}},
			ShowThread:    true, ThreadParentID: "p1",
			ThreadMessages: []Message{{ID: "r1", AuthorID: "mona", Author: "Mona", Body: "On it.", TimeLabel: "09:52"}},
			Callbacks:      walkCallbacks()}
		pane := renderNode(t, threadPane(m, handlers{}))
		if !strings.Contains(pane, "thread-channel-link") || !strings.Contains(pane, "Hans") || !strings.Contains(pane, "Shall we?") {
			t.Errorf("%s: the thread heading lost its channel or the parent its author and text", locale)
		}
		if !strings.Contains(pane, `aria-label="`+notify[locale]+`"`) {
			t.Errorf("%s: the bell does not say what it does", locale)
		}
		if !strings.Contains(pane, "On it.") || strings.Index(pane, "thread-root") > strings.Index(pane, "thread-replies") {
			t.Errorf("%s: the replies are not drawn whole under the parent", locale)
		}
		if !strings.Contains(pane, "thread-composer") || !strings.Contains(pane, `data-action="close-thread"`) {
			t.Errorf("%s: the thread lost its composer or its back control", locale)
		}
		ids := map[string]int{}
		for _, match := range regexp.MustCompile(`\sid="([^"]+)"`).FindAllStringSubmatch(pane, -1) {
			ids[match[1]]++
		}
		for id, n := range ids {
			if n > 1 {
				t.Errorf("%s: id %q appears %d times in the thread pane", locale, id, n)
			}
		}
	}
}
