package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_085_Browser renders the two dialogs as the server sends
// them, in the three languages: each has its title, a named Close, the quoted
// message drawn with its list, and a button that is off until a reason is
// chosen. It also holds the menu entry that opens them to the form the client
// closes the menu for.
func TestTodo_CHATBUG_085_Browser(t *testing.T) {
	type words struct{ report, send, remove, removeTitle, close string }
	for locale, want := range map[string]words{
		"en-US": {"Report message", "Send report", "Remove message", "Remove this message?", "Close"},
		"de-DE": {"Nachricht melden", "Meldung senden", "Nachricht entfernen", "Diese Nachricht entfernen?", "Schließen"},
		"ar":    {"الإبلاغ عن رسالة", "إرسال البلاغ", "إزالة الرسالة", "إزالة هذه الرسالة؟", "إغلاق"},
	} {
		for _, form := range []struct {
			report        bool
			title, button string
		}{{true, want.report, want.send}, {false, want.removeTitle, want.remove}} {
			action := "remove"
			if form.report {
				action = "report"
			}
			rendered, err := ui.RenderToString(chatui.ModerationDialog(chatui.ModerationDialogModel{
				Model: chatui.Model{Locale: locale, SelectedID: "room"}, Report: form.report, Action: action,
				Selection: chat.RemovalSelection{ConversationID: "room", PostIDs: []string{"post"}},
				Target:    chatui.ModerationTargetView{AuthorID: "jake", HomeTenantID: "t", AuthorName: "Jake Sullivan", Body: "- one\n- two"},
			}))
			if err != nil {
				t.Fatal(err)
			}
			markup := html.UnescapeString(rendered)
			if strings.Contains(markup, "⟦") || strings.Contains(markup, "chat.") {
				t.Errorf("%s %s: the dialog prints a copy key: %s", locale, action, markup)
			}
			if !regexp.MustCompile(`<h2[^>]*id="chatremove-title"[^>]*>` + regexp.QuoteMeta(form.title) + `</h2>`).MatchString(markup) {
				t.Errorf("%s %s: the title does not read %q", locale, action, form.title)
			}
			if !strings.Contains(markup, `aria-label="`+want.close+`"`) || !strings.Contains(markup, `data-chatremove-close="true"`) {
				t.Errorf("%s %s: no Close named %q in the heading", locale, action, want.close)
			}
			button := regexp.MustCompile(`<button[^>]*type="submit"[^>]*>` + regexp.QuoteMeta(form.button) + `</button>`).FindString(markup)
			if button == "" || !strings.Contains(button, " disabled") || !strings.Contains(button, chatui.ChatBug085NeedsReasonAttr+`="true"`) {
				t.Errorf("%s %s: %q is missing or can be pressed before a reason is chosen: %s", locale, action, form.button, button)
			}
			if strings.Count(markup, "<li") < 2 || !strings.Contains(markup, `class="chatremove chat-dialog"`) {
				t.Errorf("%s %s: the quoted list is not a list, or the form is not the standard dialog", locale, action)
			}
			if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
				t.Errorf("ar %s: the dialog is not right-to-left", action)
			}
		}
	}
	for _, entry := range chatui.ModerationMenuEntries("en-US", "room", "post", true) {
		item, err := ui.RenderToString(entry)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(item, `data-chatremove-open="/api/chat/moderation/page?`) || !strings.Contains(item, "action=re") || !strings.Contains(item, `role="menuitem"`) {
			t.Errorf("a menu entry does not open the dialog the client closes the menu for: %s", item)
		}
	}
}
