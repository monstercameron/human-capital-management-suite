package productui

import (
	"strings"
	"testing"
)

// TestTodo_3739_NotificationBellIsNamedButton pins the header bell as a
// button with the accessible name "Notifications, N unread". A <summary>
// without an explicit role is exposed as a disclosure triangle, so a
// name-based lookup for role button (getByRole('button', {name: /notif/i}))
// finds nothing.
func TestTodo_3739_NotificationBellIsNamedButton(t *testing.T) {
	view := testView(PageWork)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	label := view.Locale.Text("shell.notifications") + ", " + view.Locale.Plural("notifications.unread_count", 0)
	at := strings.Index(doc, `aria-label="`+label+`"`)
	if at < 0 {
		t.Fatalf("bell label %q missing from rendered header", label)
	}
	start := strings.LastIndex(doc[:at], "<summary")
	if start < 0 {
		t.Fatal("bell label is not on a <summary> trigger")
	}
	end := strings.Index(doc[start:], ">")
	if end < 0 {
		t.Fatal("unterminated bell trigger")
	}
	trigger := doc[start : start+end+1]
	if !strings.Contains(trigger, `role="button"`) {
		t.Fatalf("bell trigger has no button role: %s", trigger)
	}
}
