//go:build js && wasm

package chatui

import "syscall/js"

// focusDeleteAsk puts the caret on Cancel once the menu has been redrawn as the
// question (CHATBUG-081). The press that asked was on a row that the redraw
// removes, and a removed element leaves the caret on the page body. The redraw
// lands on a later frame than the press, so a few frames are tried.
func focusDeleteAsk(menuID string) {
	frame := js.Global().Get("requestAnimationFrame")
	if menuID == "" || frame.Type() != js.TypeFunction {
		return
	}
	attempts := 0
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		if menu := messageMenuPopup(menuID); menu.Truthy() {
			if cancel := menu.Call("querySelector", `[data-delete-ask="cancel"]`); cancel.Truthy() {
				positionMessageMenu(menu)
				cancel.Call("focus")
				callback.Release()
				return nil
			}
		}
		attempts++
		if attempts < 15 {
			frame.Invoke(callback)
		} else {
			callback.Release()
		}
		return nil
	})
	frame.Invoke(callback)
}

// shownMenuItems is the rows of an open message menu that are drawn. The
// stylesheet hides the rows that the hover bar already carries (CHATUX-022),
// and a hidden row cannot take the caret, so the arrow keys skip it.
func shownMenuItems(menu js.Value) []js.Value {
	found := menu.Call("querySelectorAll", messageMenuItems)
	shown := make([]js.Value, 0, found.Get("length").Int())
	for i := 0; i < found.Get("length").Int(); i++ {
		item := found.Index(i)
		if rects := item.Get("getClientRects"); rects.Type() == js.TypeFunction && item.Call("getClientRects").Get("length").Int() == 0 {
			continue
		}
		shown = append(shown, item)
	}
	return shown
}
