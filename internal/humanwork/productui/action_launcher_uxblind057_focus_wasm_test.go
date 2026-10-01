//go:build js && wasm

package productui

import (
	"syscall/js"
	"testing"
)

func TestActionLauncherEscapeRestoresTriggerFocus(t *testing.T) {
	global := js.Global()
	previousDocument := global.Get("document")
	document := global.Get("Object").New()
	trigger := global.Get("Object").New()
	focused := false
	focus := js.FuncOf(func(js.Value, []js.Value) any {
		focused = true
		return nil
	})
	lookup := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].String() == "action-launcher-trigger" {
			return trigger
		}
		return js.Null()
	})
	trigger.Set("focus", focus)
	document.Set("getElementById", lookup)
	global.Set("document", document)
	t.Cleanup(func() {
		global.Set("document", previousDocument)
		focus.Release()
		lookup.Release()
	})

	prevented, closed := false, false
	if !actionLauncherEscape("Escape", func() { prevented = true }, func() { closed = true }) {
		t.Fatal("Escape was not handled")
	}
	if !prevented || !closed || !focused {
		t.Fatalf("Escape effects: prevented=%t closed=%t focused-trigger=%t", prevented, closed, focused)
	}

	focused, prevented, closed = false, false, false
	if actionLauncherEscape("Enter", func() { prevented = true }, func() { closed = true }) {
		t.Fatal("Enter was handled as Escape")
	}
	if prevented || closed || focused {
		t.Fatalf("non-Escape effects: prevented=%t closed=%t focused-trigger=%t", prevented, closed, focused)
	}
}
