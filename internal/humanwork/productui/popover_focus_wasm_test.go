//go:build js && wasm

package productui

import (
	"syscall/js"
	"testing"
	"time"
)

func TestActionLauncherFocusDismissal(t *testing.T) {
	global := js.Global()
	previous := global.Get("document")
	doc := global.Get("Object").New()
	root := global.Get("Object").New()
	trigger := global.Get("Object").New()
	inside := global.Get("Object").New()
	outside := global.Get("Object").New()
	handlers := map[string]js.Value{}
	focused := 0
	prevented := 0
	contains := js.FuncOf(func(_ js.Value, args []js.Value) any { return args[0].Equal(inside) })
	// UXAUDIT-003: the real target this control passes is its trigger
	// button id, not "root" again -- getElementById must tell them apart
	// so the Escape branch below can prove it moves focus to the actual
	// trigger, not back onto the popover root by accident.
	lookup := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].String() == "trigger" {
			return trigger
		}
		return root
	})
	focus := js.FuncOf(func(js.Value, []js.Value) any { focused++; return nil })
	preventDefault := js.FuncOf(func(js.Value, []js.Value) any { prevented++; return nil })
	add := js.FuncOf(func(_ js.Value, args []js.Value) any { handlers[args[0].String()] = args[1]; return nil })
	remove := js.FuncOf(func(_ js.Value, args []js.Value) any { delete(handlers, args[0].String()); return nil })
	root.Set("contains", contains)
	trigger.Set("focus", focus)
	doc.Set("getElementById", lookup)
	doc.Set("addEventListener", add)
	doc.Set("removeEventListener", remove)
	doc.Set("activeElement", inside)
	global.Set("document", doc)
	t.Cleanup(func() {
		global.Set("document", previous)
		contains.Release()
		lookup.Release()
		add.Release()
		remove.Release()
		focus.Release()
		preventDefault.Release()
	})
	dismissed := 0
	cleanup := bindPopoverFocusDismissal("root", "trigger", func() { dismissed++ })
	emit := func(kind string, target, related js.Value) {
		event := global.Get("Object").New()
		event.Set("type", kind)
		event.Set("target", target)
		event.Set("relatedTarget", related)
		handlers[kind].Invoke(event)
	}
	emit("focusout", inside, inside)
	time.Sleep(220 * time.Millisecond)
	if dismissed != 0 {
		t.Fatal("moving within the popover dismissed it")
	}
	doc.Set("activeElement", outside)
	emit("focusout", inside, outside)
	time.Sleep(220 * time.Millisecond)
	if dismissed != 1 {
		t.Fatal("leaving focus did not dismiss the popover")
	}
	if focused != 0 {
		t.Fatal("ordinary focus travel was pulled back to the launcher trigger")
	}
	emit("pointerdown", inside, js.Null())
	if dismissed != 1 {
		t.Fatal("inside click dismissed the popover")
	}
	emit("pointerdown", outside, js.Null())
	if dismissed != 2 {
		t.Fatal("outside click did not dismiss the popover")
	}
	doc.Set("activeElement", outside)
	emit("focusout", inside, outside)
	emit("pointerdown", outside, js.Null())
	time.Sleep(220 * time.Millisecond)
	if dismissed != 3 {
		t.Fatalf("focusout and pointerdown raced into %d dismissals, want one", dismissed-2)
	}
	if focused != 0 {
		t.Fatal("outside click lost focus to the launcher trigger")
	}
	// Escape dismisses and restores focus to the actual trigger.
	escape := global.Get("Object").New()
	escape.Set("type", "keydown")
	escape.Set("target", inside)
	escape.Set("key", "Escape")
	escape.Set("preventDefault", preventDefault)
	handlers["keydown"].Invoke(escape)
	if dismissed != 4 || focused != 1 || prevented != 1 {
		t.Fatalf("Escape dismissal = dismissed %d, focused %d, prevented %d; want 4, 1, 1", dismissed, focused, prevented)
	}
	emit("focusout", inside, outside)
	cleanup()
	time.Sleep(220 * time.Millisecond)
	if dismissed != 4 || len(handlers) != 0 {
		t.Fatal("cleanup left live callbacks")
	}
}
