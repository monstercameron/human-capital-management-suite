//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATBUG-085, the client's part: a report or removal dialog chosen from a
// message's menu closes that menu, is the standard Chat dialog over a
// backdrop, keeps its button off until a reason is chosen, and hands focus
// back to the message when it closes.

// dialogFromMenu is called when a control opens a dialog. A control inside a
// message menu takes the menu with it: the menu closes, and because that
// removes the control from the page, the message is remembered so that focus
// can return to it.
func (b *chatremoveBrowser) dialogFromMenu(anchor js.Value) {
	b.dialogMessage, b.dialogInThread = "", false
	if !anchor.Call("closest", ".message-menu").Truthy() {
		return
	}
	if row := anchor.Call("closest", "[data-message-id]"); row.Truthy() {
		b.dialogMessage = row.Call("getAttribute", "data-message-id").String()
		b.dialogInThread = anchor.Call("closest", ".thread-pane").Truthy()
	}
	chatBrowser.mutate(func(model *chatui.Model) { model.MenuID = "" })
	refreshChatRoute()
}

// dialogReturn is what takes focus when the dialog closes: the control that
// opened it while that is still on the page, otherwise the More button of the
// message it was about, otherwise the message.
func (b *chatremoveBrowser) dialogReturn() js.Value {
	if b.dialogOpener.Truthy() && b.dialogOpener.Get("isConnected").Bool() {
		return b.dialogOpener
	}
	if b.dialogMessage == "" {
		return js.Undefined()
	}
	scope := ".message-list"
	if b.dialogInThread {
		scope = ".thread-pane"
	}
	id := b.dialogMessage
	if css := js.Global().Get("CSS"); css.Truthy() && css.Get("escape").Type() == js.TypeFunction {
		id = css.Call("escape", id).String()
	}
	row := js.Global().Get("document").Call("querySelector", scope+` [data-message-id="`+id+`"]`)
	if !row.Truthy() {
		return js.Undefined()
	}
	if more := row.Call("querySelector", `[data-action="menu"]`); more.Truthy() {
		return more
	}
	return row
}

// chatbug085FocusReturn gives focus to what dialogReturn found. A message row
// stands for its More button: the button is drawn only while its row is
// active, so the row takes focus first, which makes it active, and the button
// takes focus as soon as the row has drawn it. Nothing waits for a timer or a
// frame; the row's own change is what is observed.
func chatbug085FocusReturn(target js.Value) {
	if !target.Call("matches", "[data-message-id]").Bool() {
		target.Call("focus")
		return
	}
	row := target
	if more := row.Call("querySelector", `[data-action="menu"]`); more.Truthy() {
		more.Call("focus")
		return
	}
	row.Call("focus")
	compact := js.Global().Call("matchMedia", "(max-width:767px), (pointer:coarse)").Get("matches").Bool()
	if !chatbug085AwaitsMore(row.Call("matches", ":focus-visible").Bool(), compact) {
		return
	}
	document := js.Global().Get("document")
	var observer js.Value
	var drawn js.Func
	drawn = js.FuncOf(func(js.Value, []js.Value) any {
		more := row.Call("querySelector", `[data-action="menu"]`)
		active := document.Get("activeElement")
		// Focus that the person has moved since stays where they put it.
		held := active.Truthy() && (active.Equal(row) || active.Equal(document.Get("body")))
		if more.Truthy() && held {
			more.Call("focus")
		}
		if more.Truthy() || !held {
			observer.Call("disconnect")
			drawn.Release()
		}
		return nil
	})
	observer = js.Global().Get("MutationObserver").New(drawn)
	observer.Call("observe", row, map[string]any{"childList": true})
}

// chatbug085FirstField is where the caret starts in a dialog: the first field
// of its form, not the Close button in its heading.
func chatbug085FirstField(overlay js.Value) js.Value {
	if field := overlay.Call("querySelector", "form :is(input:not([type=hidden]),textarea,select,button:not([disabled]))"); field.Truthy() {
		return field
	}
	return overlay.Call("querySelector", "input,textarea,select,button")
}

// chatbug085ReasonChanged turns on the button of the form a reason was chosen
// in. The server rendered it off (chatui.ChatBug085NeedsReasonAttr).
func chatbug085ReasonChanged(radio js.Value) {
	form := radio.Call("closest", "form")
	if !form.Truthy() {
		return
	}
	chosen := form.Call("querySelector", "input[type=radio][name=reason]:checked").Truthy()
	buttons := form.Call("querySelectorAll", "["+chatui.ChatBug085NeedsReasonAttr+"]")
	for i := 0; i < buttons.Length(); i++ {
		buttons.Index(i).Set("disabled", !chosen)
	}
}
