//go:build js && wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// shiftHeld reports whether Shift was held for a keyboard event, so Enter
// sends and Shift+Enter inserts a line break in the composer.
func shiftHeld(event ui.KeyboardEvent) bool { return event.GetShiftKey() }
