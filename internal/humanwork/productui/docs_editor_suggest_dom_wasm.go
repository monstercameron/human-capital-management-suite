//go:build js && wasm

package productui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func docsSuggestEventIndex(event ui.MouseEvent) (index string) {
	defer func() {
		if recover() != nil {
			index = ""
		}
	}()
	target := event.JSValue().Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return ""
	}
	option := target.Call("closest", "[data-suggest-index]")
	if !option.Truthy() {
		return ""
	}
	return option.Get("dataset").Get("suggestIndex").String()
}
