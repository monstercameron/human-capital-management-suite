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
	inside := global.Get("Object").New()
	outside := global.Get("Object").New()
	trigger := global.Get("Object").New()
	focusedTrigger := false
	trigger.Set("focus", js.FuncOf(func(js.Value, []js.Value) any { focusedTrigger = true; return nil }))
	handlers := map[string]js.Value{}
	contains := js.FuncOf(func(_ js.Value, args []js.Value) any { return args[0].Equal(inside) })
	// UXAUDIT-003: the real target this control passes is its trigger
	// button id, not "root" again -- getElementById must tell them apart
	// so the Escape branch below can prove it moves focus to the actual
	// trigger, not back onto the popover root by accident.
	lookup := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() == "trigger" {
			return trigger
		}
		return root
	})
	add := js.FuncOf(func(_ js.Value, args []js.Value) any { handlers[args[0].String()] = args[1]; return nil })
	remove := js.FuncOf(func(_ js.Value, args []js.Value) any { delete(handlers, args[0].String()); return nil })
	root.Set("contains", contains)
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
	emit("pointerdown", inside, js.Null())
	if dismissed != 1 {
		t.Fatal("inside click dismissed the popover")
	}
	emit("pointerdown", outside, js.Null())
	if dismissed != 2 {
		t.Fatal("outside click did not dismiss the popover")
	}

	// UXAUDIT-003 RED: Escape did not close the launcher live (the
	// trigger's aria-expanded stayed "true"). This is the binding that
	// RED clause traces to: Escape pressed while focus is inside the
	// popover must dismiss it and hand focus back to the trigger.
	escape := global.Get("Object").New()
	escape.Set("type", "keydown")
	escape.Set("target", inside)
	escape.Set("key", "Escape")
	prevented := false
	escape.Set("preventDefault", js.FuncOf(func(js.Value, []js.Value) any { prevented = true; return nil }))
	handlers["keydown"].Invoke(escape)
	if !prevented {
		t.Fatal("Escape over the popover was not intercepted")
	}
	if dismissed != 3 {
		t.Fatal("Escape did not dismiss the popover")
	}
	if !focusedTrigger {
		t.Fatal("Escape did not restore focus to the trigger")
	}

	emit("focusout", inside, outside)
	cleanup()
	time.Sleep(220 * time.Millisecond)
	if dismissed != 3 || len(handlers) != 0 {
		t.Fatal("cleanup left live callbacks")
	}
}
