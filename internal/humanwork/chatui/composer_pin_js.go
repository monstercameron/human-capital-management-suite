//go:build js && wasm

package chatui

import "syscall/js"

var composerPinInstalled bool

// installComposerPin keeps a timeline that was at its newest message on its
// newest message when a composer grows. On a phone the idle composer is one
// row and gains its toolbar on focus; without this the message being answered
// slides under the taller box.
func installComposerPin() {
	if composerPinInstalled {
		return
	}
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	composerPinInstalled = true
	handler := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		if !target.Truthy() || target.Get("id").Type() != js.TypeString {
			return nil
		}
		scroller := ""
		switch target.Get("id").String() {
		case "chat-composer":
			scroller = ".chat-main .message-list"
		case "thread-composer":
			scroller = ".thread-pane .thread-scroll"
		default:
			return nil
		}
		list := doc.Call("querySelector", scroller)
		if !list.Truthy() {
			return nil
		}
		atBottom := list.Get("scrollHeight").Float()-list.Get("scrollTop").Float()-list.Get("clientHeight").Float() < 48
		if !atBottom {
			return nil
		}
		var settle js.Func
		frames := 0
		settle = js.FuncOf(func(js.Value, []js.Value) any {
			frames++
			if frames < 2 {
				js.Global().Call("requestAnimationFrame", settle)
				return nil
			}
			defer settle.Release()
			if l := doc.Call("querySelector", scroller); l.Truthy() {
				l.Set("scrollTop", l.Get("scrollHeight"))
			}
			return nil
		})
		js.Global().Call("requestAnimationFrame", settle)
		return nil
	})
	doc.Call("addEventListener", "focusin", handler)
	doc.Call("addEventListener", "focusout", handler)
}
