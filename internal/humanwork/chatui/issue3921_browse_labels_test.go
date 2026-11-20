package chatui_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func issue3921Page(t *testing.T, locale string) string {
	t.Helper()
	return lane3Page(t, locale, func(m *chatui.Model) {
		m.ShowBrowse = true
		m.Callbacks.JoinConversation, m.Callbacks.CloseBrowse, m.Callbacks.SelectConversation = func(string) {}, func() {}, func(string) {}
		m.Conversations = []chatui.Conversation{
			{ID: "a", Name: "announcements", Kind: chatui.PublicChannel, Joined: true},
			{ID: "b", Name: "benefits", Kind: chatui.PublicChannel, Joined: true},
		}
		m.Browse = []chatui.Conversation{{ID: "r", Name: "random", Kind: chatui.PublicChannel, MemberCount: 4}}
	})
}

// buttonTag returns the opening tag of the button that carries data-action and data-id.
func issue3921ButtonTag(t *testing.T, page, action, id string) string {
	t.Helper()
	for _, tag := range regexp.MustCompile(`<button[^>]*>`).FindAllString(page, -1) {
		if strings.Contains(tag, `data-action="`+action+`"`) && strings.Contains(tag, `data-id="`+id+`"`) {
			return tag
		}
	}
	t.Fatalf("no %s button for %s in the browse dialog", action, id)
	return ""
}

// TestIssue3921_BrowseActionsNameTheirChannel: Open and Join keep their short
// visible label, and each one's accessible name carries the channel it acts on.
func TestIssue3921_BrowseActionsNameTheirChannel(t *testing.T) {
	for _, tc := range []struct{ locale, open, join string }{
		{"en-US", "Open announcements", "Join random"},
		{"de-DE", "Öffnen announcements", "Beitreten random"},
		{"ar", "فتح announcements", "انضمام random"},
	} {
		page := issue3921Page(t, tc.locale)
		if tag := issue3921ButtonTag(t, page, "browse-open", "a"); !strings.Contains(tag, `aria-label="`+tc.open+`"`) {
			t.Errorf("%s: Open button lacks name %q: %s", tc.locale, tc.open, tag)
		}
		if tag := issue3921ButtonTag(t, page, "browse-open", "b"); !strings.Contains(tag, `benefits"`) {
			t.Errorf("%s: second Open button lacks its channel: %s", tc.locale, tag)
		}
		tag := issue3921ButtonTag(t, page, "join", "r")
		if !strings.Contains(tag, `aria-label="`+tc.join+`"`) {
			t.Errorf("%s: Join button lacks name %q: %s", tc.locale, tc.join, tag)
		}
		if !strings.Contains(tag, `aria-describedby="browse-meta-r"`) || !strings.Contains(page, `id="browse-meta-r"`) {
			t.Errorf("%s: Join button is not tied to its row's kind and member facts", tc.locale)
		}
	}
	// The visible labels stay short.
	page := issue3921Page(t, "en-US")
	if !strings.Contains(page, ">Open<") || !strings.Contains(page, ">Join<") {
		t.Error("visible Open/Join labels changed")
	}
}
