//go:build !js || !wasm

package chatui

// focusDeleteAsk has no menu to act on outside the browser; see
// chatbug081_focus_js.go.
func focusDeleteAsk(string) {}
