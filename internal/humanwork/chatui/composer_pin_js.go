//go:build js && wasm

package chatui

import "syscall/js"

var composerPinInstalled bool

// installComposerPin keeps a timeline that was at its newest message on its
// newest message when a composer grows. On a phone the idle composer is one
// row and gains its toolbar on focus; without this the message being answered
// slides under the taller box. CHAT-3716: a composer that grows while the
// viewer types (several lines, toolbar open) is followed too. The check runs at
// beforeinput, before the text changes, and the pin is applied at input, after
// the list has shrunk under the composer.
func installComposerPin() {
	if composerPinInstalled {
		return
	}
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	composerPinInstalled = true
	scrollerFor := func(target js.Value) string {
		if !target.Truthy() || target.Get("id").Type() != js.TypeString {
			return ""
		}
		switch target.Get("id").String() {
		case "chat-composer":
			return ".chat-main .message-list"
		case "thread-composer":
			return ".thread-pane .thread-scroll"
		}
		return ""
	}
	atNewest := func(scroller string) bool {
		list := doc.Call("querySelector", scroller)
		if !list.Truthy() {
			return false
		}
		return chat3716AtNewest(list.Get("scrollHeight").Float(), list.Get("scrollTop").Float(), list.Get("clientHeight").Float())
	}
	settle := func(scroller string) {
		var frame js.Func
		frames := 0
		frame = js.FuncOf(func(js.Value, []js.Value) any {
			frames++
			if frames < 2 {
				js.Global().Call("requestAnimationFrame", frame)
				return nil
			}
			defer frame.Release()
			if l := doc.Call("querySelector", scroller); l.Truthy() {
				l.Set("scrollTop", l.Get("scrollHeight"))
			}
			return nil
		})
		js.Global().Call("requestAnimationFrame", frame)
	}
	// pinFor is the list that was on its newest message when the pending edit
	// began, or empty when no edit is waiting to be followed.
	pinFor := ""
	beforeInput := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		pinFor = ""
		if scroller := scrollerFor(args[0].Get("target")); scroller != "" && atNewest(scroller) {
			pinFor = scroller
		}
		return nil
	})
	input := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || pinFor == "" {
			return nil
		}
		scroller := pinFor
		pinFor = ""
		if scrollerFor(args[0].Get("target")) == scroller {
			settle(scroller)
		}
		return nil
	})
	focus := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		scroller := scrollerFor(args[0].Get("target"))
		if scroller == "" || !atNewest(scroller) {
			return nil
		}
		settle(scroller)
		return nil
	})
	doc.Call("addEventListener", "beforeinput", beforeInput)
	doc.Call("addEventListener", "input", input)
	doc.Call("addEventListener", "focusin", focus)
	doc.Call("addEventListener", "focusout", focus)
}
