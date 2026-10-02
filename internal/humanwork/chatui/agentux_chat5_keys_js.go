//go:build js && wasm

package chatui

import "syscall/js"
import "github.com/monstercameron/GoWebComponents/v5/ui"

func composerIsComposing(e ui.KeyboardEvent) bool {
	return e.JSValue().Get("isComposing").Truthy() || (e.JSValue().Get("keyCode").Type() == js.TypeNumber && e.JSValue().Get("keyCode").Int() == 229)
}
