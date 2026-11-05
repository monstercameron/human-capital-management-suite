//go:build js && wasm

package graphcanvas

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// pointerCoords reads canvas-relative pointer coordinates in a browser:
// client position minus the canvas element's live bounding rect, so scroll
// and page layout never skew the gesture.
func pointerCoords(canvasID string, event ui.MouseEvent) (x, y float64, button int, shift bool) {
	value := event.JSValue()
	if value.IsUndefined() || value.IsNull() {
		return 0, 0, 0, false
	}
	x, y = jsFloat(value, "clientX"), jsFloat(value, "clientY")
	if left, top, ok := canvasOrigin(canvasID); ok {
		x, y = x-left, y-top
	}
	return x, y, jsInt(value, "button"), jsBool(value, "shiftKey")
}

// wheelDeltas reads a wheel gesture: pixel deltas plus the canvas-relative
// anchor. Line and page delta modes scale to pixels.
func wheelDeltas(canvasID string, event ui.MouseEvent) (dx, dy, x, y float64) {
	value := event.JSValue()
	if value.IsUndefined() || value.IsNull() {
		return 0, 0, 0, 0
	}
	dx, dy = jsFloat(value, "deltaX"), jsFloat(value, "deltaY")
	switch jsInt(value, "deltaMode") {
	case 1:
		dx, dy = dx*16, dy*16
	case 2:
		dx, dy = dx*800, dy*600
	}
	x, y = jsFloat(value, "clientX"), jsFloat(value, "clientY")
	if left, top, ok := canvasOrigin(canvasID); ok {
		x, y = x-left, y-top
	}
	return dx, dy, x, y
}

// clickOffset reads a click position relative to the canvas element.
func clickOffset(canvasID string, event ui.MouseEvent) (x, y float64) {
	value := event.JSValue()
	if value.IsUndefined() || value.IsNull() {
		return 0, 0
	}
	x, y = jsFloat(value, "clientX"), jsFloat(value, "clientY")
	if left, top, ok := canvasOrigin(canvasID); ok {
		x, y = x-left, y-top
	}
	return x, y
}

// canvasOrigin returns the canvas element's live viewport offset.
func canvasOrigin(canvasID string) (float64, float64, bool) {
	document := js.Global().Get("document")
	if document.IsUndefined() || document.IsNull() {
		return 0, 0, false
	}
	element := document.Call("getElementById", canvasID)
	if element.IsUndefined() || element.IsNull() {
		return 0, 0, false
	}
	rect := element.Call("getBoundingClientRect")
	if rect.IsUndefined() || rect.IsNull() {
		return 0, 0, false
	}
	return jsFloat(rect, "left"), jsFloat(rect, "top"), true
}

func jsFloat(value js.Value, prop string) float64 {
	field := value.Get(prop)
	if field.IsUndefined() || field.IsNull() {
		return 0
	}
	return field.Float()
}

func jsInt(value js.Value, prop string) int {
	field := value.Get(prop)
	if field.IsUndefined() || field.IsNull() {
		return 0
	}
	return field.Int()
}

func jsBool(value js.Value, prop string) bool {
	field := value.Get(prop)
	if field.IsUndefined() || field.IsNull() {
		return false
	}
	return field.Bool()
}
