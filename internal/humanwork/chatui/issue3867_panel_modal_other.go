//go:build !(js && wasm)

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// SyncPhonePanelModal is a no-op outside the browser.
func SyncPhonePanelModal()              {}
func syncPhonePanelModal()              {}
func trapPhonePanelFocus(ui.Event) bool { return false }
