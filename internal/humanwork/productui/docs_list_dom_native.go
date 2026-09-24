//go:build !(js && wasm)

package productui

// docsSearchFocused has no focus to read outside the browser.
func docsSearchFocused() bool { return false }

// setDocsSelectAllMixed has no checkbox to mark outside the browser.
func setDocsSelectAllMixed(bool) {}
