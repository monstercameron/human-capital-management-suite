package chatui

import (
	"strings"
	"testing"
)

// TestIssue3893_MembersDisclosureBeforeNewRows: the Show all / Show fewer
// button sits after the first eight people and before the rows it reveals, so
// pressing Enter on it and then Tab walks into the newly shown people instead
// of skipping them.
func TestIssue3893_MembersDisclosureBeforeNewRows(t *testing.T) {
	for _, tc := range []struct{ locale, all, fewer string }{
		{"en-US", "Show all 15", "Show fewer"},
		{"de-DE", "Alle 15 anzeigen", "Weniger anzeigen"},
		{"ar", "", "عرض أقل"},
	} {
		m := chatux030Model(15)
		m.Locale = tc.locale
		collapsed := chatux021Details(t, m, handlers{})
		collapsed = collapsed[strings.Index(collapsed, `id="chat-details-members"`):]
		if got := strings.Count(collapsed, `class="member-row"`); got != chatux030MembersVisible {
			t.Fatalf("%s: collapsed list shows %d people, want %d", tc.locale, got, chatux030MembersVisible)
		}
		if tc.all != "" && !strings.Contains(collapsed, ">"+tc.all+"<") {
			t.Errorf("%s: collapsed list lacks %q", tc.locale, tc.all)
		}
		open := chatux021Details(t, m, handlers{local: localUI{detailGroups: map[string]bool{chatux030MembersKey: true}}})
		open = open[strings.Index(open, `id="chat-details-members"`):]
		open = open[:strings.Index(open, "</section>")]
		button := strings.Index(open, `data-id="members-all"`)
		if button < 0 || !strings.Contains(open, tc.fewer) {
			t.Fatalf("%s: expanded list has no collapse button %q: %s", tc.locale, tc.fewer, open)
		}
		last := strings.Index(open, "Person 07")
		first := strings.Index(open, "Person 08")
		end := strings.Index(open, "Person 14")
		if last < 0 || first < 0 || end < 0 {
			t.Fatalf("%s: rows missing from expanded list", tc.locale)
		}
		if !(last < button && button < first) {
			t.Errorf("%s: the collapse button is at %d, want between the eighth person (%d) and the first newly shown one (%d), so forward Tab reaches the new rows", tc.locale, button, last, first)
		}
		if got := strings.Count(open, `class="member-row"`); got != 15 {
			t.Errorf("%s: expanded list shows %d people, want 15", tc.locale, got)
		}
	}
}
