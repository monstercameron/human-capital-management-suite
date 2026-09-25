package projectclient

import "strings"

// Board keyboard shortcuts. The host reads the key event and where focus is,
// and asks BoardShortcut what, if anything, the key means. Keeping the guard
// here keeps it testable without a browser.

// Shortcut actions.
const (
	ShortcutNewTask = "new-task"
	ShortcutSearch  = "search"
	ShortcutFilters = "filters"
	ShortcutNext    = "next"
	ShortcutPrev    = "prev"
	ShortcutRight   = "right"
	ShortcutLeft    = "left"
	ShortcutHelp    = "help"
)

// KeyTarget describes the focused element for the guard.
type KeyTarget struct {
	// Tag is the element's tag name in any case ("INPUT", "textarea").
	Tag string
	// Editable is true for contenteditable elements.
	Editable bool
	// InOverlay is true inside an open menu, popover or dialog, which own
	// their keys.
	InOverlay bool
}

// KeyPress is one keydown.
type KeyPress struct {
	Key                 string
	Ctrl, Meta, Alt     bool
	Repeat, IsComposing bool
	Target              KeyTarget
}

// TypingTarget reports whether keys typed at target are text, not commands.
func TypingTarget(target KeyTarget) bool {
	switch strings.ToLower(target.Tag) {
	case "input", "textarea", "select":
		return true
	}
	return target.Editable
}

// BoardShortcut returns the action for a key press on the board, or "".
// Keys never act while the user is typing, composing text, holding a
// command modifier, or working inside a menu, popover or dialog.
func BoardShortcut(press KeyPress) string {
	if press.Ctrl || press.Meta || press.Alt || press.IsComposing || TypingTarget(press.Target) || press.Target.InOverlay {
		return ""
	}
	switch press.Key {
	case "c", "C":
		if press.Repeat {
			return ""
		}
		return ShortcutNewTask
	case "/":
		return ShortcutSearch
	case "f", "F":
		if press.Repeat {
			return ""
		}
		return ShortcutFilters
	case "j", "ArrowDown":
		return ShortcutNext
	case "k", "ArrowUp":
		return ShortcutPrev
	case "l", "ArrowRight":
		return ShortcutRight
	case "h", "ArrowLeft":
		return ShortcutLeft
	case "?":
		return ShortcutHelp
	}
	return ""
}
