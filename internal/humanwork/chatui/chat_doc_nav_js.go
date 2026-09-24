//go:build js && wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// eventPlainClick reports an unmodified primary-button click.
func eventPlainClick(e ui.Event) bool {
	native := e.JSValue()
	return native.Get("button").Int() == 0 && !native.Get("ctrlKey").Bool() && !native.Get("metaKey").Bool() && !native.Get("shiftKey").Bool() && !native.Get("altKey").Bool()
}
