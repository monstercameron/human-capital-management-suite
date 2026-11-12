package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestIssue3710_BrowseCountSingular: the Browse channels header reads the
// count with the locale's plural rule, so one channel is "1 channel", not
// "1 channels", and two or more keep the plural.
func TestIssue3710_BrowseCountSingular(t *testing.T) {
	for _, tc := range []struct {
		locale string
		count  int
		want   string
		bad    string
	}{
		{"en-US", 1, ">1 channel<", ">1 channels<"},
		{"en-US", 2, ">2 channels<", ""},
		{"de-DE", 1, ">1 Kanal<", ""},
	} {
		channels := make([]chatui.Conversation, 0, tc.count)
		for i := 0; i < tc.count; i++ {
			id := string(rune('a' + i))
			channels = append(channels, chatui.Conversation{ID: id, Name: "random" + id, Kind: chatui.PublicChannel, MemberCount: 3})
		}
		page := lane3Page(t, tc.locale, func(m *chatui.Model) {
			m.ShowBrowse = true
			m.Callbacks.JoinConversation, m.Callbacks.CloseBrowse = func(string) {}, func() {}
			m.Conversations = nil
			m.Browse = channels
		})
		if !strings.Contains(page, tc.want) {
			t.Errorf("%s with %d channels: browse header misses %q", tc.locale, tc.count, tc.want)
		}
		if tc.bad != "" && strings.Contains(page, tc.bad) {
			t.Errorf("%s with %d channels: browse header reads %q", tc.locale, tc.count, tc.bad)
		}
	}
}
