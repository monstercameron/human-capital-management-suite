//go:build js && wasm

package chatui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var mobileRailTrigger js.Value
var mobileRailOpen bool
var mobileRailGeneration uint64
var mobileRailResize js.Func

func mobileRailActive() bool {
	match := js.Global().Get("matchMedia")
	return match.Type() == js.TypeFunction && match.Invoke("(max-width: 760px)").Get("matches").Bool()
}

func rememberMobileRailTrigger(event ui.Event) {
	target := event.JSValue().Get("target")
	if target.Truthy() && target.Get("closest").Type() == js.TypeFunction {
		mobileRailTrigger = target.Call("closest", ".mobile-chat-toggle")
	}
}

func setMobileRailModal(open bool) js.Value {
	doc := js.Global().Get("document")
	if !doc.Truthy() || doc.Get("querySelector").Type() != js.TypeFunction {
		return js.Null()
	}
	root := doc.Call("querySelector", ".chat-workspace")
	if !root.Truthy() {
		return js.Null()
	}
	rail := root.Call("querySelector", ".chat-rail")
	if !rail.Truthy() {
		return js.Null()
	}
	modal := open && mobileRailActive()
	if modal {
		rail.Call("setAttribute", "role", "dialog")
		rail.Call("setAttribute", "aria-modal", "true")
	} else {
		rail.Call("setAttribute", "role", "navigation")
		rail.Call("removeAttribute", "aria-modal")
	}
	for _, selector := range []string{".chat-main", ".chat-side", ".chat-skip"} {
		el := root.Call("querySelector", selector)
		if el.Truthy() {
			el.Set("inert", modal)
		}
	}
	return rail
}

func syncMobileRailFocus(open bool) {
	changed := open != mobileRailOpen
	mobileRailOpen = open
	if !changed && !open {
		return
	}
	if open && !mobileRailResize.Truthy() {
		mobileRailResize = js.FuncOf(func(js.Value, []js.Value) any {
			setMobileRailModal(mobileRailOpen)
			return nil
		})
		js.Global().Call("addEventListener", "resize", mobileRailResize)
	} else if !open && mobileRailResize.Truthy() {
		js.Global().Call("removeEventListener", "resize", mobileRailResize)
		mobileRailResize.Release()
		mobileRailResize = js.Func{}
	}
	mobileRailGeneration++
	generation := mobileRailGeneration
	frame := js.Global().Get("requestAnimationFrame")
	if frame.Type() != js.TypeFunction {
		return
	}
	attempts := 0
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		if generation != mobileRailGeneration {
			callback.Release()
			return nil
		}
		rail := setMobileRailModal(open)
		if !rail.Truthy() && attempts < 30 {
			attempts++
			frame.Invoke(callback)
			return nil
		}
		if rail.Truthy() && open && mobileRailActive() {
			doc := js.Global().Get("document")
			root := doc.Call("querySelector", ".chat-workspace")
			search := rail.Call("querySelector", "#chat-search")
			ready := root.Truthy() && root.Get("dataset").Get("sidebarOpen").String() == "true" && search.Truthy() && search.Get("isConnected").Bool() && search.Call("getClientRects").Get("length").Int() > 0
			if !ready {
				attempts++
				if attempts < 60 {
					frame.Invoke(callback)
					return nil
				}
			} else if !rail.Call("contains", doc.Get("activeElement")).Bool() {
				search.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
				// Reconciliation can replace the focused precommit node. Verify on
				// the next frame before declaring the drawer settled.
				attempts++
				if attempts < 60 {
					frame.Invoke(callback)
					return nil
				}
			}
		} else if !open && mobileRailTrigger.Truthy() && mobileRailTrigger.Get("isConnected").Bool() && mobileRailActive() {
			// A room switch can deliberately focus a new composer; leave that focus alone.
			doc := js.Global().Get("document")
			active := doc.Get("activeElement")
			if !active.Truthy() || active.Equal(doc.Get("body")) || active.Equal(mobileRailTrigger) || rail.Call("contains", active).Bool() {
				mobileRailTrigger.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
			}
		}
		callback.Release()
		return nil
	})
	frame.Invoke(callback)
}

func trapMobileRailFocus(event ui.Event) bool {
	if !mobileRailActive() || event.JSValue().Get("key").String() != "Tab" {
		return false
	}
	rail := setMobileRailModal(true)
	if !rail.Truthy() {
		return false
	}
	items := rail.Call("querySelectorAll", "a[href]:not([tabindex='-1']),button:not([disabled]),input:not([disabled]),summary,[tabindex]:not([tabindex='-1'])")
	visible := make([]js.Value, 0, items.Get("length").Int())
	for i := 0; i < items.Get("length").Int(); i++ {
		item := items.Index(i)
		if mobileRailFocusable(item) {
			visible = append(visible, item)
		}
	}
	if len(visible) == 0 {
		return false
	}
	active := js.Global().Get("document").Get("activeElement")
	first, last := visible[0], visible[len(visible)-1]
	if event.JSValue().Get("shiftKey").Bool() {
		if active.Equal(first) || !rail.Call("contains", active).Bool() {
			last.Call("focus")
			return true
		}
	} else if active.Equal(last) || !rail.Call("contains", active).Bool() {
		first.Call("focus")
		return true
	}
	return false
}

func mobileRailFocusable(item js.Value) bool {
	closedDetails := item.Call("closest", "details:not([open])")
	insideClosedDetails := closedDetails.Truthy() && (item.Get("tagName").String() != "SUMMARY" || !item.Get("parentElement").Equal(closedDetails))
	hiddenAncestor := item.Call("closest", "[hidden],[inert],[aria-hidden='true']")
	return !insideClosedDetails && !hiddenAncestor.Truthy() && item.Call("getClientRects").Get("length").Int() > 0
}

func clearMobileRailFocus() {
	mobileRailGeneration++
	mobileRailOpen = false
	if mobileRailResize.Truthy() {
		js.Global().Call("removeEventListener", "resize", mobileRailResize)
		mobileRailResize.Release()
		mobileRailResize = js.Func{}
	}
	setMobileRailModal(false)
	mobileRailTrigger = js.Undefined()
}
