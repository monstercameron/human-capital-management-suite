package chatui_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func issue3922Status(t *testing.T, locale, query string, settled bool, channels ...chatui.Conversation) string {
	t.Helper()
	page := lane3Page(t, locale, func(m *chatui.Model) {
		m.ShowBrowse, m.BrowseQuery, m.BrowseAnnounce = true, query, settled
		m.Callbacks.JoinConversation, m.Callbacks.CloseBrowse = func() func(string) { return func(string) {} }(), func() {}
		m.Conversations = nil
		m.Browse = channels
	})
	match := regexp.MustCompile(`<[^>]*browse-status[^>]*>([^<]*)<`).FindStringSubmatch(page)
	if match == nil {
		t.Fatalf("%s: browse dialog has no result status region", locale)
	}
	tag := match[0]
	for _, want := range []string{`role="status"`, `aria-live="polite"`, `aria-atomic="true"`} {
		if !strings.Contains(tag, want) {
			t.Errorf("%s: status region lacks %s: %s", locale, want, tag)
		}
	}
	return match[1]
}

// TestIssue3922_BrowseFilterAnnouncesSettledResults: once the person stops
// typing the dialog's polite status says how many channels match or that none
// do, in the page's language; while they are still typing it says nothing.
func TestIssue3922_BrowseFilterAnnouncesSettledResults(t *testing.T) {
	one := chatui.Conversation{ID: "e", Name: "engineering", Kind: chatui.PublicChannel}
	two := chatui.Conversation{ID: "f", Name: "finance", Kind: chatui.PublicChannel}
	for _, tc := range []struct {
		locale, query string
		channels      []chatui.Conversation
		want          string
	}{
		{"en-US", "eng", []chatui.Conversation{one}, "Results: 1 channel"},
		{"en-US", "e", []chatui.Conversation{one, two}, "Results: 2 channels"},
		{"en-US", "zzz", nil, "No channels match “zzz”"},
		{"en-US", "", []chatui.Conversation{one, two}, "Results: 2 channels"},
		{"de-DE", "eng", []chatui.Conversation{one}, "Ergebnisse: 1 Kanal"},
		{"de-DE", "zzz", nil, "Keine Kanäle passen zu „zzz“"},
		{"ar", "eng", []chatui.Conversation{one}, "النتائج: قناة واحدة"},
		{"ar", "zzz", nil, "لا توجد قنوات تطابق «zzz»"},
	} {
		if got := issue3922Status(t, tc.locale, tc.query, true, tc.channels...); got != tc.want {
			t.Errorf("%s %q: status = %q, want %q", tc.locale, tc.query, got, tc.want)
		}
	}
	if got := issue3922Status(t, "en-US", "en", false, one); got != "" {
		t.Errorf("status while typing = %q, want empty", got)
	}
}
