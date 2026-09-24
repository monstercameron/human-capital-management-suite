//go:build !(js && wasm)

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func rememberPersonTrigger(ui.Event)  {}
func syncPersonFocus(bool)            {}
func syncPersonFocusFor(bool, string) {}
func FocusComposer()                  {}
func FocusComposerFor(string)         {}
