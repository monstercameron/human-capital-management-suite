package chatui

import "testing"

// TestIssue3885_MenuToggleSelector: a dialog opened from a menu item sends
// focus back to that menu's own button, found by the menu's first class.
func TestIssue3885_MenuToggleSelector(t *testing.T) {
	for className, want := range map[string]string{
		"section-menu":             ".section-menu [data-chat-disclosure-toggle]",
		"section-menu is-open":     ".section-menu [data-chat-disclosure-toggle]",
		"  rail-prefs-control  x ": ".rail-prefs-control [data-chat-disclosure-toggle]",
		"":                         "",
	} {
		if got := chatDialogMenuToggleSelector(className); got != want {
			t.Errorf("chatDialogMenuToggleSelector(%q) = %q, want %q", className, got, want)
		}
	}
}
