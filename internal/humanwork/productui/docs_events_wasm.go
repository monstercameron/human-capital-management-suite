//go:build js && wasm

package productui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// docsEventAction reads the nearest [data-docs-action] above a click. The
// Docs list delegates every row control to one handler, so a list of
// hundreds of documents registers a handful of listeners instead of a few
// per row. plain reports an unmodified primary click: a link opened with a
// modifier or the middle button keeps the browser's own behaviour.
func docsEventAction(event ui.Event) (action, id string, plain bool) {
	native := event.JSValue()
	target := native.Get("target")
	if !target.Truthy() || target.Get("closest").IsUndefined() {
		return "", "", false
	}
	el := target.Call("closest", "[data-docs-action]")
	if !el.Truthy() || el.Get("disabled").Truthy() {
		return "", "", false
	}
	dataset := el.Get("dataset")
	action = dataset.Get("docsAction").String()
	if value := dataset.Get("docsId"); value.Truthy() {
		id = value.String()
	}
	plain = native.Get("button").Int() == 0 && !native.Get("ctrlKey").Bool() && !native.Get("metaKey").Bool() && !native.Get("shiftKey").Bool() && !native.Get("altKey").Bool()
	return action, id, plain
}

// setDocsSearchValue replaces the Docs search box's text after an outside
// navigation (back, forward, a link) changed the query.
func setDocsSearchValue(value string) {
	if el := js.Global().Get("document").Call("getElementById", "docs-browse-query"); el.Truthy() && el.Get("value").String() != value {
		el.Set("value", value)
	}
}

// setDocsFieldValue clears or fills an uncontrolled field by id.
func setDocsFieldValue(id, value string) {
	if el := js.Global().Get("document").Call("getElementById", id); el.Truthy() {
		el.Set("value", value)
	}
}

// docsSubmitForm submits a form by id through its submit event, so the
// form's own handler runs.
func docsSubmitForm(id string) {
	if form := js.Global().Get("document").Call("getElementById", id); form.Truthy() {
		form.Call("requestSubmit")
	}
}

func docsModifierHeld(event ui.Event) bool {
	native := event.JSValue()
	return native.Get("ctrlKey").Bool() || native.Get("metaKey").Bool()
}

// docsListenEscape closes an open Docs dialog on Escape wherever focus is:
// after a row inside the dialog is removed, focus falls to the document
// body, outside the dialog's own key handler.
func docsListenEscape(close func()) func() {
	doc := js.Global().Get("document")
	handler := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Get("key").String() == "Escape" && !args[0].Get("defaultPrevented").Bool() {
			// One Escape is one step back: mark it handled so a second
			// listener (a nested layer, or one not yet torn down) skips it.
			args[0].Call("preventDefault")
			close()
		}
		return nil
	})
	doc.Call("addEventListener", "keydown", handler)
	return func() {
		doc.Call("removeEventListener", "keydown", handler)
		handler.Release()
	}
}
