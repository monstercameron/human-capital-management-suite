//go:build !js || !wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func composerIsComposing(ui.KeyboardEvent) bool { return false }
