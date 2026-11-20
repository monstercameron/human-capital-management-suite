//go:build !js || !wasm

package chatui

// issue3892FocusConversation has no page to act on outside the browser.
func issue3892FocusConversation() {}
