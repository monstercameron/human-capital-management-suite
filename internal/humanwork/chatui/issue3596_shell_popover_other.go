//go:build !js || !wasm

package chatui

// The native build has no page, so no header menu is ever open.
func chatShellPopoverOpen() bool { return false }
