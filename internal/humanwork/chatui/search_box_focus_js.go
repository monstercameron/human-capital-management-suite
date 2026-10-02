//go:build js && wasm

package chatui

import "syscall/js"

// focusChatSearchBox puts the caret in the sidebar's one search box. It sets
// focus directly and again on the next frame, so nothing depends on a focus
// event or on the timing of a repaint; on a phone the drawer holding the box is
// opened first.
func focusChatSearchBox(m Model) {
	if mobileRailActive() && m.Callbacks.ToggleSidebar != nil && !m.SidebarOpen {
		m.Callbacks.ToggleSidebar(true)
	}
	focusNow := func() {
		if field := js.Global().Get("document").Call("getElementById", "chat-search"); field.Truthy() {
			field.Call("focus")
		}
	}
	focusNow()
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		defer frame.Release()
		focusNow()
		return nil
	})
	js.Global().Call("requestAnimationFrame", frame)
}
