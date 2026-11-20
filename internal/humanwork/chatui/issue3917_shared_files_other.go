//go:build !js || !wasm

package chatui

func sharedFilesNameFocused() bool { return false }

// FocusSharedFile has no page to focus outside the browser.
func FocusSharedFile(string) {}
