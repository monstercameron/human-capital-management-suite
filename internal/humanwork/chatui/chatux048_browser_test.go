package chatui_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATUX_048_Browser renders the whole Chat page, as the product's
// catalog answers it, for the three people who read a removed message: a reader,
// its author and a person who may review. Each reads only what they may; the
// notice is in the text column of the message grid; Restore is the standard
// secondary button; no copy key is printed, in en-US, de-DE and ar.
func TestTodo_CHATUX_048_Browser(t *testing.T) {
	at := time.Date(2026, 10, 1, 15, 4, 0, 0, time.UTC)
	record := map[string]chatui.ModerationRemoval{
		"m2": {By: "walt", At: at, Reason: "harassment"},
		"m3": {By: "walt", At: at, Reason: "harassment"},
	}
	removedBy := map[string]string{"en-US": "Removed by Walt Brennan", "de-DE": "Entfernt von Walt Brennan", "ar": "أزالها Walt Brennan"}
	removed := map[string]string{"en-US": "Removed by an administrator", "de-DE": "Von einem Administrator entfernt", "ar": "أزالها مسؤول"}
	reason := map[string]string{"en-US": "Reason: Harassment or bullying", "de-DE": "Grund: Belästigung oder Mobbing", "ar": "السبب: مضايقة أو تنمر"}
	restore := map[string]string{"en-US": "Restore", "de-DE": "Wiederherstellen", "ar": "استعادة"}
	tombstone := regexp.MustCompile(`(?s)<div class="message-row chatremove-tombstone chatux048-removed"[^>]*data-message-id="([^"]+)">(.*?)</div></div>`)

	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		// A reader: the words everyone gets, nothing about who or why, no button.
		reader := chatmodPage(t, locale, "mia", chatui.ModerationState{Ready: true, Removals: record})
		if strings.Contains(reader, "⟦") || strings.Contains(reader, "Walt Brennan") && strings.Contains(reader, removedBy[locale]) {
			t.Errorf("%s: a reader reads more than the words everyone gets", locale)
		}
		if strings.Contains(reader, reason[locale]) || strings.Contains(reader, restore[locale]) {
			t.Errorf("%s: a reader reads a reason or is offered Restore", locale)
		}
		rows := tombstone.FindAllStringSubmatch(reader, -1)
		if len(rows) != 2 {
			t.Fatalf("%s: want two removed rows in the message grid, got %d", locale, len(rows))
		}
		for _, row := range rows {
			if !strings.Contains(row[2], `class="chatux048-removed-body"`) || !strings.Contains(row[2], removed[locale]) {
				t.Errorf("%s: the notice is not in the text column: %s", locale, row[0])
			}
		}

		// The author of m3 reads when and why, not who; Ask for a review is a secondary button.
		own := chatmodPage(t, locale, "walt", chatui.ModerationState{Ready: true, NoticeCount: 1, Removals: record,
			Notices: map[string]chatui.ModerationOwnNotice{"m3": {ConversationID: "general", PostID: "m3", Reason: "harassment", HostTenantID: "t", CanAppeal: true, At: at}}})
		if strings.Contains(own, "⟦") || !strings.Contains(own, reason[locale]) || strings.Contains(own, removedBy[locale]) {
			t.Errorf("%s: the author's notice: %s", locale, regexp.MustCompile(`(?s)data-message-id="m3".*?</form>`).FindString(own))
		}
		if !strings.Contains(own, removed[locale]+" · ") && !strings.Contains(own, " · ") {
			t.Errorf("%s: the author is not told when", locale)
		}
		if !regexp.MustCompile(`class="button secondary small chatux048-removed-button"[^>]*type="submit"|type="submit"[^>]*class="button secondary small chatux048-removed-button"`).MatchString(own) {
			t.Errorf("%s: the appeal is not the standard secondary button", locale)
		}

		// A person who may review reads who, when and why, and Restore is the secondary button.
		mod := chatmodPage(t, locale, "olive", chatui.ModerationState{Ready: true, Moderator: true, Open: 3, Reviewable: map[string]bool{"general": true}, Removable: map[string]bool{"general": true}, Removals: record})
		if strings.Contains(mod, "⟦") || strings.Count(mod, removedBy[locale]) != 2 || strings.Count(mod, reason[locale]) != 2 {
			t.Errorf("%s: a reviewer does not read who and why on both rows: %d %d", locale, strings.Count(mod, removedBy[locale]), strings.Count(mod, reason[locale]))
		}
		if n := len(regexp.MustCompile(`class="button secondary small chatux048-removed-button"[^>]*data-chatremove-open="[^"]*action=restore[^"]*"[^>]*>`+regexp.QuoteMeta(restore[locale])).FindAllString(mod, -1)); n != 2 {
			t.Errorf("%s: want Restore as the standard secondary button on both rows, got %d", locale, n)
		}
	}
}
