//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

// chat031BrowserStub is a minimal stand-in DOM element supporting the
// attribute and "inert" property writes the workspace shell relies on for
// its narrow-layout modal behavior.
func chat031BrowserStub() js.Value {
	el := js.Global().Get("Object").New()
	el.Set("attrs", js.Global().Get("Object").New())
	setAttr := js.FuncOf(func(_ js.Value, args []js.Value) any {
		el.Get("attrs").Set(args[0].String(), args[1])
		return nil
	})
	removeAttr := js.FuncOf(func(_ js.Value, args []js.Value) any {
		el.Get("attrs").Delete(args[0].String())
		return nil
	})
	el.Set("setAttribute", setAttr)
	el.Set("removeAttribute", removeAttr)
	return el
}

// TestTodo_CHAT_031_Browser is the CHAT-031 BROWSER matrix test. It exercises
// the rendered workspace shell against a stubbed DOM: the served path
// reports whether the shell actually mounted, and the narrow-layout rail
// drawer flips between a plain navigation landmark and a focus-trapping
// modal dialog that inerts the rest of the workspace, exactly as a real
// browser would apply it after MountWebEmbed-style reconciliation.
func TestTodo_CHAT_031_Browser(t *testing.T) {
	global := js.Global()
	previous := global.Get("document")
	previousMatchMedia := global.Get("matchMedia")
	defer global.Set("document", previous)
	defer global.Set("matchMedia", previousMatchMedia)

	// The narrow-layout modal behavior only engages under a narrow viewport,
	// exactly like the real browser's own matchMedia.
	narrow := true
	matchMedia := js.FuncOf(func(_ js.Value, args []js.Value) any {
		result := global.Get("Object").New()
		result.Set("matches", narrow)
		return result
	})
	defer matchMedia.Release()
	global.Set("matchMedia", matchMedia)

	// Before the shell has rendered, the served page reports no workspace.
	unmounted := global.Get("Object").New()
	noMatch := js.FuncOf(func(js.Value, []js.Value) any { return js.Null() })
	defer noMatch.Release()
	unmounted.Set("querySelector", noMatch)
	global.Set("document", unmounted)
	if chatWorkspaceMounted(global.Get("document")) {
		t.Fatal("workspace reported mounted before anything rendered")
	}

	rail, main, side, skip := chat031BrowserStub(), chat031BrowserStub(), chat031BrowserStub(), chat031BrowserStub()
	root := chat031BrowserStub()
	rootQuery := js.FuncOf(func(_ js.Value, args []js.Value) any {
		switch args[0].String() {
		case ".chat-rail":
			return rail
		case ".chat-main":
			return main
		case ".chat-side":
			return side
		case ".chat-skip":
			return skip
		}
		return js.Null()
	})
	defer rootQuery.Release()
	root.Set("querySelector", rootQuery)

	doc := global.Get("Object").New()
	docQuery := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() == ".chat-workspace" {
			return root
		}
		return js.Null()
	})
	defer docQuery.Release()
	doc.Set("querySelector", docQuery)
	global.Set("document", doc)

	if !chatWorkspaceMounted(global.Get("document")) {
		t.Fatal("mounted workspace was not detected")
	}

	// Narrow layout, rail open: it becomes a modal dialog and the rest of the
	// workspace is inert so keyboard and screen-reader users stay inside it.
	got := setMobileRailModal(true)
	if !got.Truthy() {
		t.Fatal("narrow open did not resolve the rail element")
	}
	if rail.Get("attrs").Get("role").String() != "dialog" || rail.Get("attrs").Get("aria-modal").String() != "true" {
		t.Fatalf("open narrow rail attrs = %#v", rail.Get("attrs"))
	}
	for name, el := range map[string]js.Value{"main": main, "side": side, "skip": skip} {
		if !el.Get("inert").Bool() {
			t.Fatalf("%s stayed reachable while the narrow rail drawer was open", name)
		}
	}

	// Closing it restores a plain navigation landmark and un-inerts the rest
	// of the shell.
	setMobileRailModal(false)
	if rail.Get("attrs").Get("role").String() != "navigation" {
		t.Fatalf("closed rail role = %q, want navigation", rail.Get("attrs").Get("role").String())
	}
	if rail.Get("attrs").Get("aria-modal").Truthy() {
		t.Fatal("closed rail still advertised itself as a modal")
	}
	for name, el := range map[string]js.Value{"main": main, "side": side, "skip": skip} {
		if el.Get("inert").Bool() {
			t.Fatalf("%s stayed inert after the narrow rail drawer closed", name)
		}
	}

	// Desktop layout: even with the rail's open state true, the workspace
	// never presents it as a modal drawer, and nothing outside it is inerted.
	narrow = false
	setMobileRailModal(true)
	if rail.Get("attrs").Get("role").String() != "navigation" {
		t.Fatalf("desktop rail role = %q, want navigation even when open", rail.Get("attrs").Get("role").String())
	}
	if rail.Get("attrs").Get("aria-modal").Truthy() {
		t.Fatal("desktop layout advertised the rail as a modal")
	}
	for name, el := range map[string]js.Value{"main": main, "side": side, "skip": skip} {
		if el.Get("inert").Bool() {
			t.Fatalf("desktop layout inerted %s outside the rail", name)
		}
	}
}
