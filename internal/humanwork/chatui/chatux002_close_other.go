//go:build !(js && wasm)

package chatui

// The panels are closed by the browser's own layer code; there is none here.
func chatux002CloseSidebarPanels() {}
