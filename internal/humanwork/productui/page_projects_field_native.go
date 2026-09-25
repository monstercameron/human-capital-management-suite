//go:build !(js && wasm)

package productui

// projectFieldValue has no DOM outside the browser.
func projectFieldValue(string) string { return "" }
