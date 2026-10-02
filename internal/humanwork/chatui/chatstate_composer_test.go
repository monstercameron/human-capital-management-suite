package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// TestTodo_CHATSTATE_002_ComposerSentence: where a person cannot post, the
// composer is replaced by one sentence that says why and who can, naming the
// channel it is about, and never by a box with nothing said.
func TestTodo_CHATSTATE_002_ComposerSentence(t *testing.T) {
	room := Conversation{ID: "announce", Name: "announcements", Kind: PublicChannel, Joined: true}
	view := ChannelStatusView{Status: chat.ChannelStatus{TenantID: "t", ConversationID: "announce", Status: chatpolicy.StatusAnnouncements, Revision: 2}}
	for locale, want := range map[string]string{
		"en-US": "Only announcers can post in #announcements. You can reply in threads and react.",
		"de-DE": "Nur Ankündigende können in #announcements schreiben.",
		"ar":    "في #announcements",
	} {
		m := Model{Locale: locale, SelectedID: "announce", Conversations: []Conversation{room}}
		notice := renderNode(t, ChannelStatusComposerNotice(m, view))
		if !strings.Contains(notice, want) || strings.Contains(notice, "{name}") || strings.Contains(notice, "<textarea") || strings.Count(notice, "<p") != 1 {
			t.Errorf("%s: %s", locale, notice)
		}
	}
	// A person who can post keeps the composer, and so does a failed read.
	view.CanPost = true
	if ChannelStatusComposerNotice(Model{SelectedID: "announce", Conversations: []Conversation{room}}, view) != nil {
		t.Error("a person who may post lost the composer")
	}
}
