package productui

import (
	"html"
	"strings"
	"testing"
)

// The bell's accessible name carries the combined unread count, including
// chat items, in every language, and never any message text or author.
func TestIssue3739_BellNameIncludesChatUnread(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := issue3733View(t, locale)
		view.WorkflowNotifications = []WorkflowNotification{{ID: "n1", JourneyID: "j1", WorkerName: "A", Purpose: "TASK", Status: "ASSIGNED"}}
		want := view.Locale.Text("shell.notifications") + ", " + view.Locale.Plural("notifications.unread_count", 3)
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		at := strings.Index(doc, `aria-label="`+html.EscapeString(want)+`"`)
		if at < 0 {
			t.Fatalf("%s: bell name %q missing", locale, want)
		}
		start := strings.LastIndex(doc[:at], "<summary")
		end := strings.Index(doc[at:], ">")
		if start < 0 || end < 0 {
			t.Fatalf("%s: bell name is not on a summary trigger", locale)
		}
		trigger := doc[start : at+end+1]
		if !strings.Contains(trigger, `role="button"`) {
			t.Fatalf("%s: bell trigger is not a button: %s", locale, trigger)
		}
		if strings.Contains(trigger, "tabindex=\"-1\"") {
			t.Fatalf("%s: bell trigger is out of the keyboard order: %s", locale, trigger)
		}
		for _, leak := range []string{"Dana", "Cole", "look at", "Lee Park", "c-ops"} {
			if strings.Contains(want, leak) {
				t.Fatalf("%s: bell name leaks chat content %q", locale, leak)
			}
		}
	}
}

func TestIssue3739_BellNameExcludesChatWithoutChatAccess_Security(t *testing.T) {
	view := issue3733View(t, "en-US")
	view.Page = PageWork
	view.Roles = nil
	view.EffectivePermissions = nil
	view.NavigationProjection = &AuthorizedNavigationProjection{}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	want := view.Locale.Text("shell.notifications") + ", " + view.Locale.Plural("notifications.unread_count", 0)
	if !strings.Contains(doc, `aria-label="`+html.EscapeString(want)+`"`) || strings.Contains(doc, "2 unread notifications") {
		t.Fatal("chat unread was counted for a person without Chat")
	}
}
