//go:build js && wasm

package chatui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// closeOnOutsidePress closes the open GIF picker for a press that landed
// outside it and outside the button that opens it (AGENTUX-062: the same rule as
// every other layer). Focus is not moved: the press chose what takes it.
func (views *giphyPickerViews) closeOnOutsidePress(e ui.MouseEvent) {
	if views.view == nil || views.targetID == "" {
		return
	}
	target := e.JSValue().Get("target")
	if target.Truthy() && target.Get("closest").Type() == js.TypeFunction && target.Call("closest", ".giphy-picker,[data-action=giphy-toggle]").Truthy() {
		return
	}
	views.close(views.targetID, false)
}
