//go:build !js || !wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func applyComposerFormat(string, string) {}
func commandHeld(ui.KeyboardEvent) bool  { return false }
