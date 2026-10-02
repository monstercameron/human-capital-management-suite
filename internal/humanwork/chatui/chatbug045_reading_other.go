//go:build !js || !wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// renderingChangeIsScope is always false without a browser: there is no event
// target to ask.
func renderingChangeIsScope(ui.Event) bool { return false }
