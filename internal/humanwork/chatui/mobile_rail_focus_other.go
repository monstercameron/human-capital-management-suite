//go:build !(js && wasm)

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func rememberMobileRailTrigger(ui.Event) {}
func syncMobileRailFocus(bool)           {}
func trapMobileRailFocus(ui.Event) bool  { return false }
func clearMobileRailFocus()              {}
