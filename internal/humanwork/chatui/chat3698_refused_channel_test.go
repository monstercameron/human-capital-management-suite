package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestIssue3698_RefusedChannelShowsNoAccess: a refused private channel reads as
// no access, without the channel's header, its Public label, its actions or a
// retry, and without wording that says the channel was removed.
func TestIssue3698_RefusedChannelShowsNoAccess(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		refusal := chatui.ChannelNoAccessText(locale)
		refused := lane3Page(t, locale, func(m *chatui.Model) {
			m.State, m.Error, m.SelectedID = chatui.StateError, refusal, "leadership-private"
		})
		lane3NoLeaks(t, locale+" refused channel", refused)
		if !strings.Contains(refused, refusal) {
			t.Errorf("%s: the refused channel does not say the reader has no access", locale)
		}
		if strings.Contains(refused, `data-action="retry"`) {
			t.Errorf("%s: a permission refusal offers Try again", locale)
		}
		if strings.Contains(refused, "conversation-actions") || strings.Contains(refused, "conversation-name-button") {
			t.Errorf("%s: a refused channel shows the channel header and its actions", locale)
		}
		if strings.Contains(refused, "no longer available") {
			t.Errorf("%s: a refused channel is worded as removed", locale)
		}
	}
	if chatui.ChannelNoAccessText("en-US") != "You do not have access to this channel." {
		t.Errorf("the English line changed: %q", chatui.ChannelNoAccessText("en-US"))
	}

	// A room the page lists keeps its header, actions and retry on an error.
	listed := lane3Page(t, "en-US", func(m *chatui.Model) { m.State, m.Error = chatui.StateError, "failed" })
	if !strings.Contains(listed, "conversation-actions") {
		t.Errorf("a listed room lost its header actions on an error")
	}
	if !strings.Contains(listed, `data-action="retry"`) {
		t.Errorf("a listed room lost its retry on an error")
	}
}
