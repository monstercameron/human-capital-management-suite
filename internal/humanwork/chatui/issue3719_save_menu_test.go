package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// Issue 3719: the message menu carried a Save for later row that the
// stylesheet hid on desktop (0x0), so keyboard and screen-reader users arrowed
// onto an invisible item. The row is now shown everywhere the menu is.
func TestIssue3719_SaveMenuItemIsNeverHiddenInTheMenu(t *testing.T) {
	// Any rule that hides a menu-scoped chatsave-action row is the defect.
	hide := regexp.MustCompile(`([^{}]*chatsave-action[^{}]*)\{[^{}]*display:\s*none`)
	for _, m := range hide.FindAllStringSubmatch(Stylesheet, -1) {
		sel := m[1]
		if strings.Contains(sel, ".message-menu") || (strings.Contains(sel, "menu-item") && !strings.Contains(sel, "message-actions")) {
			t.Errorf("the stylesheet hides the Save for later menu row: %s", strings.TrimSpace(sel))
		}
	}
	if !strings.Contains(Stylesheet, ".message-menu .menu-item.chatsave-action{display:flex}") {
		t.Error("the stylesheet does not state that the Save for later menu row is displayed")
	}
	// The row is still in the menu and labelled for assistive technology.
	m := chatux022Model()
	m.MenuID = "m1"
	got := strings.Join(chatux022Rows(t, m, Message{ID: "m1", Body: "hello", Author: "walt"}), "|")
	if !strings.Contains(got, "Save for later") {
		t.Errorf("the menu has no Save for later row: %s", got)
	}
}
