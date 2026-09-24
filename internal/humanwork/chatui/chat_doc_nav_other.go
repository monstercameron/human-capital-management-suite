//go:build !(js && wasm)

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func eventPlainClick(ui.Event) bool { return false }
