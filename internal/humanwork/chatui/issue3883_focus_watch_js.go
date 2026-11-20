//go:build js && wasm

package chatui

import "syscall/js"

// watchChatLayerOpenerFocus keeps returning focus to the opener of a layer for
// as long as it takes the page to put the opener back (issue 3883). Call it right after
// restoreChatLayerFocus. That call, made as the close button is pressed, can find the opener without a
// layout box; this one retries each frame while focus is on the page.
func watchChatLayerOpenerFocus(kind string) {
	doc := js.Global().Get("document")
	pressed := doc.Get("activeElement") // still the Close button when the opener had no layout yet
	frames := 0
	var step js.Func
	step = js.FuncOf(func(js.Value, []js.Value) any {
		frames++
		root := chatLayerRoot()
		active := doc.Get("activeElement")
		lost := !active.Truthy() || active.Equal(doc.Get("body")) || active.Equal(doc.Get("documentElement"))
		var opener js.Value
		usable := false
		if root.Truthy() {
			opener = currentChatLayerOpener(root, kind)
			if opener.Truthy() && opener.Get("isConnected").Truthy() {
				box := opener.Call("getBoundingClientRect")
				usable = box.Get("width").Float() > 0 && box.Get("height").Float() > 0
			}
		}
		focus, done := issue3883WatchStep(frames, lost, usable, usable && active.Equal(opener), pressed.Truthy() && active.Equal(pressed))
		if focus {
			opener.Call("focus", map[string]any{"preventScroll": true})
		}
		if done {
			step.Release()
			return nil
		}
		js.Global().Call("requestAnimationFrame", step)
		return nil
	})
	js.Global().Call("requestAnimationFrame", step)
}
