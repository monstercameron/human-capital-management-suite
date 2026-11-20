//go:build js && wasm

package chatui

import "syscall/js"

// sharedFilesNameFocused reports whether focus is in the Shared files name field.
func sharedFilesNameFocused() bool {
	active := js.Global().Get("document").Get("activeElement")
	return active.Truthy() && active.Get("id").String() == "chat-shared-files-name"
}

// FocusSharedFile puts focus on the row of one shared file, scrolling it into
// view, once the panel has drawn it. It is how a person who opened a file's
// message comes back to the place they left in the list.
func FocusSharedFile(id string) {
	frame := js.Global().Get("requestAnimationFrame")
	find := func() js.Value {
		rows := js.Global().Get("document").Call("querySelectorAll", ".shared-file-row")
		for i := 0; i < rows.Get("length").Int(); i++ {
			if rows.Index(i).Get("dataset").Get("sharedFileId").String() == id {
				return rows.Index(i).Call("querySelector", ".shared-file-open")
			}
		}
		return js.Undefined()
	}
	run := func() {
		if button := find(); button.Truthy() {
			button.Call("focus")
			button.Call("scrollIntoView", map[string]any{"block": "nearest"})
		}
	}
	if frame.Type() != js.TypeFunction {
		run()
		return
	}
	var cb js.Func
	cb = js.FuncOf(func(js.Value, []js.Value) any {
		defer cb.Release()
		run()
		return nil
	})
	frame.Invoke(cb)
}
