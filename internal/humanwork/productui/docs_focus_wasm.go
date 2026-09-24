//go:build js && wasm

package productui

import (
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const docsFocusableSelector = `a[href],button:not([disabled]),input:not([disabled]):not([type="hidden"]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])`

// docsFindTarget resolves one focus target: "id:<id>" by exact id (ids
// carry document and folder ids that are not safe inside a selector), any
// other string as a CSS selector.
func docsFindTarget(doc js.Value, target string) (found js.Value) {
	defer func() {
		if recover() != nil {
			found = js.Null()
		}
	}()
	if id, ok := strings.CutPrefix(target, "id:"); ok {
		return doc.Call("getElementById", id)
	}
	return doc.Call("querySelector", target)
}

// docsFocusFirst focuses the first target present, selecting its text when
// asked (a rename field starts with the old name selected).
func docsFocusFirst(targets []string, selectText bool) (focused bool) {
	defer func() {
		if recover() != nil {
			focused = false
		}
	}()
	doc := js.Global().Get("document")
	for _, target := range targets {
		el := docsFindTarget(doc, target)
		if !el.Truthy() || el.Get("disabled").Truthy() || el.Call("closest", "[inert],[hidden]").Truthy() {
			continue
		}
		el.Call("focus")
		if selectText && el.Get("select").Type() == js.TypeFunction {
			el.Call("select")
			// A field that seeds its own text in the same commit (the rename
			// box writes the current name) moves the caret to the end after
			// this runs; select once more when the commit has settled.
			docsAfter(0, func() {
				defer func() { _ = recover() }()
				if doc.Get("activeElement").Equal(el) {
					el.Call("select")
				}
			})
		}
		return doc.Get("activeElement").Equal(el)
	}
	return false
}

// docsFocusIfIdle focuses target only when nothing else holds focus: the
// link that opened a document was removed with the list, so focus sits on
// <body>. A person who has already moved on keeps their place.
func docsFocusIfIdle(target string) {
	defer func() { _ = recover() }()
	doc := js.Global().Get("document")
	active := doc.Get("activeElement")
	if active.Truthy() && !active.Equal(doc.Get("body")) && active.Get("isConnected").Bool() {
		return
	}
	docsFocusFirst([]string{target}, false)
}

func docsFocusables(root js.Value) []js.Value {
	list := root.Call("querySelectorAll", docsFocusableSelector)
	out := make([]js.Value, 0, list.Length())
	for index := 0; index < list.Length(); index++ {
		el := list.Index(index)
		if el.Call("getClientRects").Length() == 0 || el.Call("closest", "[inert],[hidden]").Truthy() {
			continue
		}
		out = append(out, el)
	}
	return out
}

// docsTrapModal makes the dialog with dialogID modal until the returned
// cleanup runs. It closes any open "⋯" menu (a row menu left open behind the
// dialog took the first Escape), marks everything outside the dialog inert
// except live regions, keeps Tab inside, focuses initial (or the first
// control), and on cleanup restores focus to the element that opened it --
// a menu item's own disclosure trigger, since the item is hidden with the
// menu -- or to the first fallback present.
func docsTrapModal(dialogID, initial string, fallbacks []string) func() {
	defer func() { _ = recover() }()
	doc := js.Global().Get("document")
	dialog := doc.Call("getElementById", dialogID)
	if !dialog.Truthy() {
		return nil
	}
	layer := dialog.Call("closest", ".docs-dialog-layer")
	if !layer.Truthy() {
		layer = dialog
	}
	returnTo := doc.Get("activeElement")
	if returnTo.Truthy() && returnTo.Get("closest").Type() == js.TypeFunction {
		if details := returnTo.Call("closest", "details"); details.Truthy() {
			if summary := details.Get("firstElementChild"); summary.Truthy() && summary.Get("tagName").String() == "SUMMARY" {
				returnTo = summary
			}
		}
		if layer.Call("contains", returnTo).Bool() || returnTo.Equal(doc.Get("body")) {
			returnTo = js.Null()
		}
	}
	openMenus := doc.Call("querySelectorAll", "details[data-hcm-transient-popover][open]")
	for index := 0; index < openMenus.Length(); index++ {
		openMenus.Index(index).Call("removeAttribute", "open")
	}

	body := doc.Get("body")
	var inerted []js.Value
	// A live region stays reachable, even inside a subtree that turns inert:
	// the notice a dialog's action raises ("Moved to Policies") is written
	// while the dialog is still closing and must be announced. A subtree
	// holding one is descended into instead of being made inert whole.
	var makeInert func(el js.Value)
	makeInert = func(el js.Value) {
		if el.Call("hasAttribute", "inert").Bool() || el.Call("hasAttribute", "aria-live").Bool() {
			return
		}
		switch el.Get("tagName").String() {
		case "SCRIPT", "STYLE", "LINK", "TEMPLATE":
			return
		}
		if el.Call("querySelector", "[aria-live]").Truthy() {
			children := el.Get("children")
			for index := 0; index < children.Length(); index++ {
				makeInert(children.Index(index))
			}
			return
		}
		el.Call("setAttribute", "inert", "")
		inerted = append(inerted, el)
	}
	for node := layer; node.Truthy() && !node.Equal(body); node = node.Get("parentElement") {
		parent := node.Get("parentElement")
		if !parent.Truthy() {
			break
		}
		children := parent.Get("children")
		for index := 0; index < children.Length(); index++ {
			if child := children.Index(index); !child.Equal(node) {
				makeInert(child)
			}
		}
	}

	keydown := js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer func() { _ = recover() }()
		if len(args) == 0 || args[0].Get("key").String() != "Tab" || !dialog.Get("isConnected").Bool() {
			return nil
		}
		event := args[0]
		items := docsFocusables(dialog)
		if len(items) == 0 {
			event.Call("preventDefault")
			dialog.Call("focus")
			return nil
		}
		first, last := items[0], items[len(items)-1]
		active := doc.Get("activeElement")
		shift := event.Get("shiftKey").Bool()
		switch {
		case !active.Truthy() || !dialog.Call("contains", active).Bool():
			event.Call("preventDefault")
			if shift {
				last.Call("focus")
			} else {
				first.Call("focus")
			}
		case shift && (active.Equal(first) || active.Equal(dialog)):
			event.Call("preventDefault")
			last.Call("focus")
		case !shift && active.Equal(last):
			event.Call("preventDefault")
			first.Call("focus")
		}
		return nil
	})
	doc.Call("addEventListener", "keydown", keydown, true)

	start := js.Null()
	if initial != "" {
		start = docsFindTarget(doc, initial)
		if start.Truthy() && !dialog.Call("contains", start).Bool() {
			start = js.Null()
		}
	}
	if !start.Truthy() {
		if items := docsFocusables(dialog); len(items) > 0 {
			start = items[0]
		} else {
			start = dialog
		}
	}
	start.Call("focus")

	return func() {
		defer func() { _ = recover() }()
		doc.Call("removeEventListener", "keydown", keydown, true)
		keydown.Release()
		for _, el := range inerted {
			el.Call("removeAttribute", "inert")
		}
		if returnTo.Truthy() && returnTo.Get("isConnected").Bool() && !returnTo.Call("closest", "[inert],[hidden]").Truthy() {
			returnTo.Call("focus")
			if doc.Get("activeElement").Equal(returnTo) {
				return
			}
		}
		docsFocusFirst(fallbacks, false)
	}
}

// docsBindMenuKeys adds arrow-key travel to the Docs "⋯" disclosures. The
// menus are plain disclosures holding buttons (no menu role, so Tab still
// moves through them as screen readers expect); the arrows are a shortcut.
func docsBindMenuKeys() func() {
	defer func() { _ = recover() }()
	doc := js.Global().Get("document")
	handler := js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer func() { _ = recover() }()
		if len(args) == 0 {
			return nil
		}
		event := args[0]
		key := event.Get("key").String()
		switch key {
		case "ArrowDown", "ArrowUp", "Home", "End":
		default:
			return nil
		}
		target := event.Get("target")
		if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
			return nil
		}
		details := target.Call("closest", "details.docs-row-menu,details.docs-folder-menu")
		if !details.Truthy() {
			return nil
		}
		if !details.Get("open").Bool() {
			if key != "ArrowDown" && key != "ArrowUp" {
				return nil
			}
			details.Set("open", true)
		}
		items := details.Call("querySelectorAll", ".docs-menu-item:not(:disabled)")
		count := items.Length()
		if count == 0 {
			return nil
		}
		current := -1
		for index := 0; index < count; index++ {
			if items.Index(index).Equal(target) {
				current = index
			}
		}
		next := docsMenuNext(key, current, count)
		event.Call("preventDefault")
		items.Index(next).Call("focus")
		return nil
	})
	doc.Call("addEventListener", "keydown", handler)
	return func() {
		doc.Call("removeEventListener", "keydown", handler)
		handler.Release()
	}
}

// docsAfter runs fn once after delay; the returned func cancels it.
func docsAfter(delay time.Duration, fn func()) func() {
	var callback js.Func
	done := false
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		if !done {
			done = true
			callback.Release()
			fn()
		}
		return nil
	})
	timer := js.Global().Call("setTimeout", callback, delay.Milliseconds())
	return func() {
		if !done {
			done = true
			js.Global().Call("clearTimeout", timer)
			callback.Release()
		}
	}
}

// docsEventInDialog reports whether a key event started inside a Docs
// dialog, which owns its own Escape.
func docsEventInDialog(event ui.Event) bool { return docsEventInside(event, ".docs-dialog-layer") }

// docsEventInside reports whether an event's target sits inside an element
// matching selector.
func docsEventInside(event ui.Event, selector string) (inside bool) {
	defer func() {
		if recover() != nil {
			inside = false
		}
	}()
	target := event.JSValue().Get("target")
	return target.Truthy() && target.Get("closest").Type() == js.TypeFunction && target.Call("closest", selector).Truthy()
}
