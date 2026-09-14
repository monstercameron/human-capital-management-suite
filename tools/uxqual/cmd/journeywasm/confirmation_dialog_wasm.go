//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// bindConfirmationDialogs enhances the shared server-renderable action form
// with a native modal. The dialog supplies browser focus containment and
// Escape behavior; this controller owns the open/close and pending lifecycle.
// Delegation survives GWC reconciling an action card after each RPC answer.
func bindConfirmationDialogs(store *journey.Store) {
	if store == nil {
		return
	}
	document := js.Global().Get("document")
	var opener, active js.Value
	submitted := false
	focusOutcome := func() {
		result := store.Page().Notice
		fallback := opener
		if result == nil || result.Busy {
			if fallback.Truthy() && fallback.Get("isConnected").Bool() {
				fallback.Call("focus", map[string]any{"preventScroll": true})
			}
			return
		}
		// The store publishes before GWC commits its new DOM. Wait through a
		// bounded render window so the result, not a removed action, receives
		// focus after the modal has closed.
		var focusFrame func(int)
		focusFrame = func(attempt int) {
			var callback js.Func
			callback = js.FuncOf(func(_ js.Value, _ []js.Value) any {
				defer callback.Release()
				current := store.Page().Notice
				if current == nil || current.Busy || current.Tone != result.Tone || current.Title != result.Title {
					return nil
				}
				target := document.Call("querySelector", `.jn-notice[data-tone="`+result.Tone+`"]`)
				if attempt >= 2 && target.Truthy() {
					target.Call("setAttribute", "tabindex", "-1")
					target.Call("focus", map[string]any{"preventScroll": true})
					// The product shell owns its own scroll pane. Focus alone with
					// preventScroll leaves the result above the viewport after a
					// review near the middle of a long promotion detail.
					target.Call("scrollIntoView", map[string]any{"behavior": "auto", "block": "start"})
					return nil
				}
				if attempt < 8 {
					focusFrame(attempt + 1)
				} else if fallback.Truthy() && fallback.Get("isConnected").Bool() {
					fallback.Call("focus", map[string]any{"preventScroll": true})
				}
				return nil
			})
			js.Global().Call("requestAnimationFrame", callback)
		}
		focusFrame(0)
	}

	click := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		if !target.Truthy() || target.Get("nodeType").Int() != 1 {
			return nil
		}
		if trigger := target.Call("closest", ".jn-confirm-trigger"); trigger.Truthy() {
			form := trigger.Call("closest", "form.jn-action")
			if !form.Truthy() {
				return nil
			}
			dialog := form.Call("querySelector", ".jn-confirm-dialog")
			if !dialog.Truthy() || dialog.Get("open").Bool() {
				return nil
			}
			opener, active, submitted = trigger, dialog, false
			dialog.Call("showModal")
			if title := dialog.Call("querySelector", ".jn-confirm-title"); title.Truthy() {
				title.Call("focus", map[string]any{"preventScroll": true})
			}
			return nil
		}
		if cancel := target.Call("closest", ".jn-confirm-cancel"); cancel.Truthy() {
			dialog := cancel.Call("closest", "dialog.jn-confirm-dialog")
			if dialog.Truthy() && dialog.Get("open").Bool() && !submitted {
				dialog.Call("close")
			}
		}
		return nil
	})
	document.Call("addEventListener", "click", click)

	keyboardCancel := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && submitted && active.Truthy() && args[0].Get("target").Equal(active) {
			args[0].Call("preventDefault")
		}
		return nil
	})
	document.Call("addEventListener", "cancel", keyboardCancel, true)

	submit := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || !active.Truthy() || !active.Get("open").Bool() {
			return nil
		}
		form := active.Call("closest", "form.jn-action")
		if !form.Truthy() || !args[0].Get("target").Equal(form) {
			return nil
		}
		if submitted {
			args[0].Call("preventDefault")
			return nil
		}
		submitted = true
		active.Call("setAttribute", "data-pending", "true")
		active.Call("setAttribute", "aria-busy", "true")
		for _, selector := range []string{".jn-confirm-actions button[type=submit]", ".jn-confirm-cancel"} {
			if button := active.Call("querySelector", selector); button.Truthy() {
				button.Set("disabled", true)
			}
		}
		return nil
	})
	document.Call("addEventListener", "submit", submit, true)

	closed := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || !active.Truthy() || !args[0].Get("target").Equal(active) {
			return nil
		}
		if submitted {
			focusOutcome()
		} else if opener.Truthy() && opener.Get("isConnected").Bool() {
			opener.Call("focus", map[string]any{"preventScroll": true})
		}
		opener, active = js.Undefined(), js.Undefined()
		submitted = false
		return nil
	})
	document.Call("addEventListener", "close", closed, true)

	store.Subscribe(func() {
		if !submitted || !active.Truthy() {
			return
		}
		notice := store.Page().Notice
		if notice == nil || notice.Busy {
			return
		}
		var callback js.Func
		callback = js.FuncOf(func(_ js.Value, _ []js.Value) any {
			defer callback.Release()
			if !submitted || !active.Truthy() {
				return nil
			}
			if active.Get("isConnected").Bool() && active.Get("open").Bool() {
				active.Call("close")
			} else {
				// A successful transition can replace its own action card before
				// the dialog receives close. Keep focus on the result either way.
				focusOutcome()
				opener, active = js.Undefined(), js.Undefined()
				submitted = false
			}
			return nil
		})
		js.Global().Call("requestAnimationFrame", callback)
	})
}
