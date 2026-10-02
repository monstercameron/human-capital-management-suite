//go:build !(js && wasm)

package chatui

// RestoreFieldValue has no page to write to outside the browser.
func RestoreFieldValue(string, string) {}
