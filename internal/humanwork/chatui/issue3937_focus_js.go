//go:build js && wasm

package chatui

import (
	"strings"
	"syscall/js"
)

// focusChatSearchScoped is the header search icon's action (#3937): it writes
// the open conversation's "in:" filter into the sidebar's search box, keeping the
// words already typed there, and puts the caret after it. The write is marked as
// typed so a later render of the empty model value cannot take it back, and it is
// repeated on the next frame because on a phone the drawer holding the box opens
// first. Nothing is searched until the person adds words.
func focusChatSearchScoped(m Model) {
	scope := func() {
		field := js.Global().Get("document").Call("getElementById", "chat-search")
		if !field.Truthy() {
			return
		}
		current := field.Get("value").String()
		next := issue3937ScopedQuery(m, current)
		if next != current {
			field.Set("value", next)
			field.Set("__chatTyped", true)
			field.Set("__chatCleared", false)
			// Words already in the box are an active search: narrow it now, so the
			// results match the scope the box states.
			if m.Callbacks.Search != nil && strings.TrimSpace(current) != "" {
				m.Callbacks.Search(strings.TrimSpace(next))
			}
		}
		field.Call("focus")
		end := len([]rune(next))
		func() {
			defer func() { _ = recover() }()
			field.Call("setSelectionRange", end, end)
		}()
	}
	if mobileRailActive() && m.Callbacks.ToggleSidebar != nil && !m.SidebarOpen {
		mobileRailFocusSearch = true
		m.Callbacks.ToggleSidebar(true)
	}
	scope()
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		defer frame.Release()
		scope()
		return nil
	})
	js.Global().Call("requestAnimationFrame", frame)
}
