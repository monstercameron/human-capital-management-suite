//go:build js && wasm

package main

import "syscall/js"

// Issue 3950. While edits exist only on this page (a write of the drafts
// failed), leaving or reloading would lose them. The browser's own "leave
// site?" prompt is switched on for exactly that time, so the person can stay
// and let the retry finish. It is off whenever everything is saved.
var chatDraftUnloadGuard struct {
	on       bool
	listener js.Func
}

func setChatDraftUnloadGuard(on bool) {
	if chatDraftUnloadGuard.on == on {
		return
	}
	window := js.Global()
	if !window.Truthy() || window.Get("addEventListener").Type() != js.TypeFunction {
		return
	}
	if on {
		chatDraftUnloadGuard.listener = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				args[0].Call("preventDefault")
				args[0].Set("returnValue", "")
			}
			return nil
		})
		window.Call("addEventListener", "beforeunload", chatDraftUnloadGuard.listener)
	} else {
		window.Call("removeEventListener", "beforeunload", chatDraftUnloadGuard.listener)
		chatDraftUnloadGuard.listener.Release()
	}
	chatDraftUnloadGuard.on = on
}
