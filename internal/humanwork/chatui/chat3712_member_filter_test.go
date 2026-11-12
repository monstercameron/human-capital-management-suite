package chatui

import (
	"strings"
	"testing"
)

// TestIssue3712_MemberFilterEmptyState: a member filter that matches nobody
// says so in the Members section and names the way back to the full list. The
// section used to end at an empty list, with only Manage channel below it.
func TestIssue3712_MemberFilterEmptyState(t *testing.T) {
	for locale, want := range map[string]string{"en-US": "No members match", "de-DE": "Keine Mitglieder", "ar": "يطابقون"} {
		m := chatux030Model(3)
		m.Locale = locale
		m.MemberQuery = "zzz"
		page := chatux021Details(t, m, handlers{})
		section := page[strings.Index(page, `id="chat-details-members"`):]
		section = section[:strings.Index(section, "</section>")]
		if got := strings.Count(section, `class="member-row"`); got != 0 {
			t.Fatalf("%s: a filter that matches nobody still lists %d people", locale, got)
		}
		if !strings.Contains(section, `class="member-empty"`) || !strings.Contains(section, want) || !strings.Contains(section, "zzz") {
			t.Errorf("%s: the empty filter has no message naming the query: %s", locale, section)
		}
	}
	// A filter that matches someone keeps the list and no message.
	m := chatux030Model(3)
	m.MemberQuery = "Person 01"
	page := chatux021Details(t, m, handlers{})
	if strings.Contains(page, `class="member-empty"`) {
		t.Error("a filter that matches a person shows the empty message")
	}
}
