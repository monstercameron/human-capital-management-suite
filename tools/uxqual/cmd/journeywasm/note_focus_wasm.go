//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// bindNoteComposerFocus returns focus to the notes textarea when a note
// submission settles, but only when focus was lost (it sits on <body> or on
// an element that is no longer in the document); a reader who has already
// moved elsewhere keeps their place.
func bindNoteComposerFocus(store *journey.Store) {
	if store == nil {
		return
	}
	var tracker noteFocusTracker
	store.Subscribe(func() {
		if !tracker.observe(store.Page()) {
			return
		}
		var attempt func(frame int)
		attempt = func(frame int) {
			var callback js.Func
			callback = js.FuncOf(func(js.Value, []js.Value) any {
				defer callback.Release()
				document := js.Global().Get("document")
				active := document.Get("activeElement")
				body := document.Get("body")
				lost := !active.Truthy() || active.Equal(body) || !active.Get("isConnected").Bool()
				if !lost {
					return nil
				}
				target := document.Call("getElementById", "note-body")
				if target.Truthy() && !target.Get("disabled").Bool() {
					target.Call("focus", map[string]any{"preventScroll": true})
					return nil
				}
				if frame < 8 {
					attempt(frame + 1)
				}
				return nil
			})
			js.Global().Call("requestAnimationFrame", callback)
		}
		attempt(0)
	})
}
