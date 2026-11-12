package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestIssue3713_UnavailableChannelShowsNotFound: a deep link to a channel the
// page cannot list or open (#channel=doesnotexist) must not render the channel
// header with a loading bar, an invented "Public" kind, its actions, or a Try
// again for a channel that is not there.
func TestIssue3713_UnavailableChannelShowsNotFound(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		refusal := chatui.ChannelNoAccessText(locale)
		missing := lane3Page(t, locale, func(m *chatui.Model) {
			m.State, m.Error, m.SelectedID = chatui.StateError, refusal, "doesnotexist"
		})
		lane3NoLeaks(t, locale+" missing channel", missing)
		if !strings.Contains(missing, refusal) {
			t.Errorf("%s: the missing channel does not show its message", locale)
		}
		if strings.Contains(missing, `data-action="retry"`) {
			t.Errorf("%s: a missing channel offers Try again", locale)
		}
		if strings.Contains(missing, "conversation-actions") || strings.Contains(missing, "conversation-name-button") {
			t.Errorf("%s: a missing channel shows the channel header and its actions", locale)
		}
		if strings.Contains(missing, "topic-kind") {
			t.Errorf("%s: a missing channel invents a kind label for itself", locale)
		}
	}
}
