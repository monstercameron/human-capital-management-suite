//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

func TestTodo_CHAT_039_BrowserEmbedSandboxAndBridgeFence(t *testing.T) {
	globalNames := []string{"document", "window"}
	previous := make(map[string]js.Value, len(globalNames))
	for _, name := range globalNames {
		previous[name] = js.Global().Get(name)
	}
	defer func() {
		for _, name := range globalNames {
			js.Global().Set(name, previous[name])
		}
	}()

	created := js.Global().Get("Array").New()
	doc := js.Global().Get("Object").New()
	create := js.FuncOf(func(_ js.Value, args []js.Value) any {
		el := webEmbedTestElement()
		el.Set("tag", args[0].String())
		created.Call("push", el)
		return el
	})
	doc.Set("createElement", create)
	defer create.Release()
	js.Global().Set("document", doc)

	window := js.Global().Get("Object").New()
	listeners := js.Global().Get("Object").New()
	add := js.FuncOf(func(_ js.Value, args []js.Value) any { listeners.Set(args[0].String(), args[1]); return nil })
	remove := js.FuncOf(func(_ js.Value, args []js.Value) any { listeners.Set(args[0].String(), js.Null()); return nil })
	window.Set("addEventListener", add)
	window.Set("removeEventListener", remove)
	defer add.Release()
	defer remove.Release()
	js.Global().Set("window", window)

	parent := webEmbedTestElement()
	var resized int
	embed := WebEmbedDescriptor{URL: "https://widgets.example/app", Origin: "https://widgets.example", Grant: "grant-1", Nonce: "nonce-1234567890", Title: "Planning widget"}
	cleanup := MountWebEmbed(parent, embed, "Preview unavailable", func(height int) { resized = height })
	defer cleanup()
	if parent.Get("children").Length() != 1 {
		t.Fatal("approved embed did not create a frame")
	}
	frame := parent.Get("children").Index(0)
	attrs := frame.Get("attrs")
	if attrs.Get("sandbox").String() != "allow-scripts" || attrs.Get("referrerpolicy").String() != "no-referrer" || attrs.Get("allow").String() != "" || attrs.Get("src").String() != embed.URL {
		t.Fatalf("frame policy = %#v", attrs)
	}
	if frame.Get("style").Get("height").Truthy() {
		t.Fatal("embed was allowed to size itself before a checked bridge message")
	}
	handler := listeners.Get("message")
	data := js.ValueOf(map[string]any{"version": 1, "grant": embed.Grant, "nonce": embed.Nonce, "action": "resize", "height": 260})
	wrongSource := js.Global().Get("Object").New()
	handler.Invoke(js.ValueOf(map[string]any{"origin": embed.Origin, "source": wrongSource, "data": data}))
	handler.Invoke(js.ValueOf(map[string]any{"origin": "https://evil.example", "source": frame.Get("contentWindow"), "data": data}))
	if resized != 0 {
		t.Fatal("forged source or origin reached the bridge callback")
	}
	handler.Invoke(js.ValueOf(map[string]any{"origin": embed.Origin, "source": frame.Get("contentWindow"), "data": data}))
	if resized != 260 || frame.Get("style").Get("height").String() != "260px" {
		t.Fatal("valid typed resize was not applied")
	}
	cleanup()
	if !frame.Get("removed").Bool() || !listeners.Get("message").IsNull() {
		t.Fatal("embed cleanup left a live frame or bridge listener")
	}
}

func webEmbedTestElement() js.Value {
	element := js.Global().Get("Object").New()
	element.Set("attrs", js.Global().Get("Object").New())
	element.Set("children", js.Global().Get("Array").New())
	element.Set("style", js.Global().Get("Object").New())
	set := js.FuncOf(func(_ js.Value, args []js.Value) any { element.Get("attrs").Set(args[0].String(), args[1]); return nil })
	append := js.FuncOf(func(_ js.Value, args []js.Value) any { element.Get("children").Call("push", args[0]); return nil })
	remove := js.FuncOf(func(js.Value, []js.Value) any { element.Set("removed", true); return nil })
	addEvent := js.FuncOf(func(_ js.Value, args []js.Value) any { element.Set("event:"+args[0].String(), args[1]); return nil })
	removeEvent := js.FuncOf(func(_ js.Value, args []js.Value) any { element.Set("event:"+args[0].String(), js.Null()); return nil })
	element.Set("setAttribute", set)
	element.Set("appendChild", append)
	element.Set("remove", remove)
	element.Set("addEventListener", addEvent)
	element.Set("removeEventListener", removeEvent)
	return element
}
