//go:build !js || !wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// syncEditFocus is a no-op on the native build; see edit_focus_js.go
// (CHAT-01).
func syncEditFocus(string) {}

// syncEditBoxHeight and chatbug073EditKey have no box to act on outside the
// browser; see edit_focus_js.go (CHATBUG-073).
func syncEditBoxHeight(string) {}

func chatbug073EditKey(ui.KeyboardEvent, string) bool { return false }
