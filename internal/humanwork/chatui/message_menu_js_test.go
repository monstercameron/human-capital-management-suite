//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

func TestMessageMenuFocusWaitsForPopupAndSkipsDisabled(t *testing.T) {
	global := js.Global()
	oldDoc, oldFrame := global.Get("document"), global.Get("requestAnimationFrame")
	defer func() { global.Set("document", oldDoc); global.Set("requestAnimationFrame", oldFrame) }()
	focused := 0
	focus := js.FuncOf(func(js.Value, []js.Value) any { focused++; return nil })
	defer focus.Release()
	item := js.ValueOf(map[string]any{"focus": focus})
	items := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() != messageMenuItems {
			t.Errorf("unexpected menu item selector: %s", args[0].String())
		}
		return js.ValueOf([]any{item})
	})
	defer items.Release()
	menu := js.ValueOf(map[string]any{"dataset": map[string]any{"messageMenu": "post-1"}, "querySelectorAll": items})
	queries := 0
	query := js.FuncOf(func(_ js.Value, args []js.Value) any {
		queries++
		if args[0].String() != ".message-menu[data-message-menu]" {
			t.Errorf("unexpected menu selector: %s", args[0].String())
		}
		if queries == 1 {
			return js.ValueOf([]any{})
		}
		return js.ValueOf([]any{menu})
	})
	defer query.Release()
	global.Set("document", js.ValueOf(map[string]any{"querySelectorAll": query}))
	frame := js.FuncOf(func(_ js.Value, args []js.Value) any { args[0].Invoke(); return nil })
	defer frame.Release()
	global.Set("requestAnimationFrame", frame)
	focusMessageMenu("post-1", true)
	if queries != 2 || focused != 1 {
		t.Fatalf("queries=%d focused=%d, want retry then focus", queries, focused)
	}
	focusMessageMenu("post-1", false)
	if queries != 3 || focused != 1 {
		t.Fatalf("pointer opening moved focus: queries=%d focused=%d", queries, focused)
	}
}
