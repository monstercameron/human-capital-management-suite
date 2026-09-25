//go:build !js || !wasm

package chatui

// syncEditFocus is a no-op on the native build; see edit_focus_js.go
// (CHAT-01).
func syncEditFocus(string) {}
