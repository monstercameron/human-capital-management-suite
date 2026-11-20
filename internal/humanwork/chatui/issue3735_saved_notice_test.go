package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestIssue3735_SavedReminderNotice: the reminder tells its owner a saved message
// is due, in their language, and opens the Saved list. It holds no message text,
// no note and no channel id.
func TestIssue3735_SavedReminderNotice(t *testing.T) {
	want := map[string][2]string{
		"en-US": {"A message you saved is due", "Open saved messages"},
		"de-DE": {"Eine von Ihnen gespeicherte Nachricht ist fällig", "Gespeicherte Nachrichten öffnen"},
		"ar":    {"رسالة حفظتها حان موعدها", "فتح الرسائل المحفوظة"},
	}
	for locale, text := range want {
		n := chat.ModerationNotice{TenantID: "t", ID: "saved:abc", ConversationID: "room-payroll", PostID: "post-9", Outcome: chat.ModerationOutcomeSavedReminder, At: time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)}
		out, err := ui.RenderToString(ModerationAuthorNotice(locale, n))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, text[0]) || !strings.Contains(out, text[1]) {
			t.Errorf("%s: missing copy in %s", locale, out)
		}
		if !strings.Contains(out, `href="#saved"`) || !strings.Contains(out, `role="status"`) || !strings.Contains(out, `aria-live="polite"`) {
			t.Errorf("%s: wrong notice shape: %s", locale, out)
		}
		if strings.Contains(out, "room-payroll") || strings.Contains(out, "post-9") || strings.Contains(out, "saved:abc") {
			t.Errorf("%s: internal ids shown: %s", locale, out)
		}
	}
}
