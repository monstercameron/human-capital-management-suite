//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Issue #3948 in the browser. Leaving a report that has a reason or details in
// it asks first. The report form is taken out of the dialog and held on the
// question (a property of the node, in memory); Keep writing puts it back as it
// was and Discard drops it. Nothing is written to storage or sent anywhere: a
// report exists only in the open dialog until it is submitted.

// requestCloseDialog is what the person's own ways of leaving a dialog do:
// Escape, Cancel, Close, a press on the backdrop.
func (b *chatremoveBrowser) requestCloseDialog() {
	overlay := b.overlay("dialog")
	if !overlay.Truthy() {
		return
	}
	panel := overlay.Call("querySelector", "[data-chatremove-discard-panel]")
	form := overlay.Call("querySelector", "form[data-chatremove=report]")
	dirty := false
	if form.Truthy() {
		reason := ""
		if chosen := form.Call("querySelector", "input[type=radio][name=reason]:checked"); chosen.Truthy() {
			reason = chosen.Get("value").String()
		}
		note := ""
		if field := form.Call("querySelector", "[name=note]"); field.Truthy() {
			note = field.Get("value").String()
		}
		b.mu.Lock()
		sending := b.busy
		b.mu.Unlock()
		dirty = issue3948Dirty(reason, note, sending)
	}
	switch issue3948CloseStep(form.Truthy(), dirty, panel.Truthy()) {
	case "keep":
		b.keepWriting(overlay, panel)
	case "ask":
		b.askDiscard(overlay)
	default:
		b.closeKind("dialog")
	}
}

// askDiscard replaces the report with the question.
func (b *chatremoveBrowser) askDiscard(overlay js.Value) {
	held := overlay.Call("querySelector", ".chatremove")
	if !held.Truthy() {
		b.closeKind("dialog")
		return
	}
	document := js.Global().Get("document")
	focused := document.Get("activeElement")
	holder := document.Call("createElement", "div")
	holder.Set("innerHTML", chatui.ReportDiscardMarkup(b.locale))
	panel := holder.Get("firstElementChild")
	if !panel.Truthy() {
		return
	}
	panel.Set("chatremoveHeld", held)
	if focused.Truthy() && held.Call("contains", focused).Bool() {
		panel.Set("chatremoveFocus", focused)
	}
	held.Call("remove")
	overlay.Call("appendChild", panel)
	if keep := panel.Call("querySelector", `[data-chatremove-discard="keep"]`); keep.Truthy() {
		keep.Call("focus")
	}
}

// keepWriting puts the report back as it was, with the caret where it was.
func (b *chatremoveBrowser) keepWriting(overlay, panel js.Value) {
	held := panel.Get("chatremoveHeld")
	if !held.Truthy() {
		return
	}
	focused := panel.Get("chatremoveFocus")
	panel.Call("remove")
	overlay.Call("appendChild", held)
	if focused.Truthy() && focused.Get("isConnected").Bool() {
		focused.Call("focus")
		return
	}
	if field := chatbug085FirstField(overlay); field.Truthy() {
		field.Call("focus")
	}
}

// discardChoice answers the question: "keep" or "yes".
func (b *chatremoveBrowser) discardChoice(choice string) {
	overlay := b.overlay("dialog")
	if !overlay.Truthy() {
		return
	}
	panel := overlay.Call("querySelector", "[data-chatremove-discard-panel]")
	if !panel.Truthy() {
		return
	}
	if choice == "yes" {
		b.closeKind("dialog")
		return
	}
	b.keepWriting(overlay, panel)
}

// retryDialog reads the dialog's content again, from Try again in its loading
// or failed frame.
func (b *chatremoveBrowser) retryDialog() {
	overlay := b.overlay("dialog")
	if !overlay.Truthy() {
		return
	}
	if href := overlay.Get("chatremoveHref"); href.Type() == js.TypeString {
		b.openOverlay(href.String(), js.Undefined())
	}
}
