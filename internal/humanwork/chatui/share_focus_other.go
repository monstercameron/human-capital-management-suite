//go:build !(js && wasm)

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func rememberShareTrigger(ui.Event) {}
func restoreShareFocus()            {}
func RestoreShareFocus()            {}
func focusShareDialog()             {}
func EnsureShareFocus() bool        { return false }
func trapShareFocus(ui.Event) bool  { return false }
