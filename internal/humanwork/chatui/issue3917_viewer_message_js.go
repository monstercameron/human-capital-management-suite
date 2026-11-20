//go:build js && wasm

package chatui

import (
	"syscall/js"
	"time"
)

var issue3917ReturnClose func()

// issue3917ViewMessageGone reports whether the message an image was sent in is
// no longer on the page.
func issue3917ViewMessageGone(post string) bool {
	if post == "" {
		return true
	}
	row := js.Global().Get("document").Call("querySelector", "[data-message-id=\""+issue3917CSSEscape(post)+"\"]")
	return !row.Truthy() || !row.Get("isConnected").Bool()
}

func issue3917CSSEscape(value string) string {
	css := js.Global().Get("CSS")
	if css.Truthy() && css.Get("escape").Type() == js.TypeFunction {
		return css.Call("escape", value).String()
	}
	return value
}

// issue3917ViewMessage brings the message into view and offers the way back to
// the image. The caller has closed the viewer and kept the thumbnail and the
// function that reopens it.
func issue3917ViewMessage(post, room, locale, dir string, reopen func()) {
	if issue3917ReturnClose != nil {
		issue3917ReturnClose()
	}
	doc := js.Global().Get("document")
	bar := doc.Call("createElement", "div")
	bar.Set("className", "chat-image-return")
	bar.Call("setAttribute", "role", "group")
	bar.Call("setAttribute", "aria-label", issue3917ViewerText(locale, "bar"))
	bar.Call("setAttribute", "lang", locale)
	bar.Call("setAttribute", "dir", dir)
	back := doc.Call("createElement", "button")
	back.Set("type", "button")
	back.Set("textContent", issue3917ViewerText(locale, "back"))
	dismiss := doc.Call("createElement", "button")
	dismiss.Set("type", "button")
	dismiss.Set("textContent", "×")
	dismiss.Call("setAttribute", "aria-label", issue3917ViewerText(locale, "dismiss"))
	dismiss.Set("title", issue3917ViewerText(locale, "dismiss"))
	bar.Call("appendChild", back)
	bar.Call("appendChild", dismiss)
	doc.Get("body").Call("appendChild", bar)

	var onClick, onKey js.Func
	var timer *time.Timer
	closed := false
	finish := func() {
		if closed {
			return
		}
		closed = true
		issue3917ReturnClose = nil
		if timer != nil {
			timer.Stop()
		}
		bar.Call("removeEventListener", "click", onClick)
		doc.Call("removeEventListener", "keydown", onKey, true)
		bar.Call("remove")
		onClick.Release()
		onKey.Release()
	}
	issue3917ReturnClose = finish
	onClick = js.FuncOf(func(_ js.Value, args []js.Value) any {
		target := args[0].Get("target")
		switch {
		case target.Equal(back):
			workspace := doc.Call("querySelector", ".chat-workspace")
			sameRoom := workspace.Truthy() && workspace.Get("dataset").Get("selectedId").String() == room
			finish()
			if sameRoom {
				reopen()
			}
		case target.Equal(dismiss):
			finish()
		}
		return nil
	})
	onKey = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].Get("key").String() == "Escape" && bar.Call("contains", doc.Get("activeElement")).Bool() {
			args[0].Call("preventDefault")
			finish()
		}
		return nil
	})
	bar.Call("addEventListener", "click", onClick)
	doc.Call("addEventListener", "keydown", onKey, true)
	// A bar nobody used does not wait for ever.
	timer = time.AfterFunc(5*time.Minute, func() { finish() })
	FocusSearchMessageOrElse(post, func() { finish() })
}
