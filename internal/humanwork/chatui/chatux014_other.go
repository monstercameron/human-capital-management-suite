//go:build !js || !wasm

package chatui

// Outside the browser no box is checked; a reply is not copied to the conversation.
func domChecked(string) bool     { return false }
func setDOMChecked(string, bool) {}
