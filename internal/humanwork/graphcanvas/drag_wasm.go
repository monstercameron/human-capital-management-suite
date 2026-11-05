//go:build js && wasm

package graphcanvas

import "syscall/js"

// Direct DOM transforms during node drags: the dragged node follows the
// pointer through style writes, bypassing render entirely, while node state
// commits once on drop. Every write guards a missing document or element:
// SSR and tests without a DOM stay silent no-ops.

func applyNodeTransform(elementID string, dx, dy float64) {
	setNodeTransform(elementID, "translate("+formatPx(dx)+","+formatPx(dy)+")")
}

func clearNodeTransform(elementID string) { setNodeTransform(elementID, "") }

func setNodeTransform(elementID, transform string) {
	document := js.Global().Get("document")
	if document.IsUndefined() || document.IsNull() {
		return
	}
	element := document.Call("getElementById", elementID)
	if element.IsUndefined() || element.IsNull() {
		return
	}
	style := element.Get("style")
	if style.IsUndefined() || style.IsNull() {
		return
	}
	style.Call("setProperty", "transform", transform)
}
