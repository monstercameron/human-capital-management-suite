//go:build !js || !wasm

package chatui

// chatux026Focus has no page to act on outside the browser; see
// chatux026_focus_js.go.
func chatux026Focus(string) {}
