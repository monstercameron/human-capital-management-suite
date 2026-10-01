//go:build js && wasm

// This file is the product entrypoint. It only builds under
// GOOS=js GOARCH=wasm because ui.Render mounts into the real DOM via
// syscall/js: the journey page ships as a GoWebComponents client that the
// server's shell loads, and everything below it happens in the browser.
//
// It mirrors tools/uxqual/render/gwc/mount_wasm.go in shape and adds the
// one thing a live surface needs that a static render does not: a mount
// that re-renders when the client's state changes, so an RPC answer
// redraws through GWC's reconciler instead of replacing the document.
package journey

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Mount renders one immutable Page into the given CSS selector. It is the
// simplest possible entrypoint: useful for a shell, a static preview or a
// smoke test, and correct only while nothing changes.
//
// Anything that answers an RPC wants MountLive.
func Mount(p Page, selector string) error {
	return MountLive(NewStore(p), selector)
}

// MountLive renders the store's page and keeps it rendered: every Set,
// Update or SetValue on the store re-renders through GWC's reconciler, so
// the DOM is patched rather than rebuilt and focus, scroll position and
// text selection survive an engine answer arriving mid-typing.
//
// The client's loop is: mount once, then call store.Set with each new Page
// the gRPC stream produces. Nothing here knows about the transport.
func MountLive(store *Store, selector string) error {
	if store == nil {
		return ErrMountFailed
	}
	var stopIdentity func()
	var stopViewport func()
	return defaultMount.Mount(selector, func() error {
		injectStylesheet()
		ui.Render(LiveComponent(store), selector)
		applyDocumentIdentity(store.Page())
		stopIdentity = store.Subscribe(func() {
			ui.PostAsync(func() { applyDocumentIdentity(store.Page()) })
		})
		stopViewport = bindNavigationViewport(selector)
		return nil
	}, func() {
		if stopViewport != nil {
			stopViewport()
		}
		if stopIdentity != nil {
			stopIdentity()
		}
		// Rendering an empty root is GWC v5's public unmount path. It runs
		// effect cleanups (including LiveComponent's Store unsubscribe) and
		// removes the owned DOM tree without reaching into runtime internals.
		ui.Render(nil, selector)
	})
}

func applyDocumentIdentity(page Page) {
	title := page.Title
	if title == "" {
		title = page.Brand
	}
	if page.TenantLabel != "" {
		title += " · " + page.TenantLabel
	} else if page.Brand != "" && page.Brand != title {
		title += " · " + page.Brand
	}
	if title != "" {
		js.Global().Get("document").Set("title", title)
	}
}

// bindNavigationViewport makes the mount the single scroll owner for route
// changes. Ordinary hash navigation starts at the top; browser Back/Forward
// restores the position recorded for the destination. A focused control gets
// one frame of protection around reconciler mutations so a controlled input
// can never make the document jump while the user is typing.
func bindNavigationViewport(selector string) func() {
	window := js.Global()
	document := window.Get("document")
	positions := map[string]float64{}
	lastHash := document.Get("location").Get("hash").String()
	traversing := false
	focusedScroll := 0.0
	focusedControl := false
	scrollY := func() float64 {
		root := document.Call("querySelector", selector+" .jn-main")
		if root.Truthy() {
			return root.Get("scrollTop").Float()
		}
		return window.Get("scrollY").Float()
	}
	setScroll := func(y float64) {
		root := document.Call("querySelector", selector+" .jn-main")
		if root.Truthy() {
			root.Call("scrollTo", 0, y)
		}
		window.Call("scrollTo", 0, y)
	}
	save := func(hash string) { positions[hash] = scrollY() }
	hashChanged := js.FuncOf(func(js.Value, []js.Value) any {
		save(lastHash)
		lastHash = document.Get("location").Get("hash").String()
		if traversing {
			if y, ok := positions[lastHash]; ok {
				setScroll(y)
			} else {
				setScroll(0)
			}
		} else {
			setScroll(0)
		}
		traversing = false
		return nil
	})
	popState := js.FuncOf(func(js.Value, []js.Value) any { traversing = true; return nil })
	window.Call("addEventListener", "hashchange", hashChanged)
	window.Call("addEventListener", "popstate", popState)
	focusIn := js.FuncOf(func(js.Value, []js.Value) any { focusedControl = true; focusedScroll = scrollY(); return nil })
	focusOut := js.FuncOf(func(js.Value, []js.Value) any { focusedControl = false; return nil })
	window.Call("addEventListener", "focusin", focusIn)
	window.Call("addEventListener", "focusout", focusOut)

	var observer js.Value
	var observerCallback js.Func
	observerAttached := false
	observerCallbackCreated := false
	if ctor := window.Get("MutationObserver"); ctor.Truthy() {
		observerCallback = js.FuncOf(func(js.Value, []js.Value) any {
			active := document.Get("activeElement")
			if focusedControl && active.Truthy() && active.Call("matches", "input,textarea,select,[contenteditable='true']").Truthy() {
				y := focusedScroll
				var restore js.Func
				restore = js.FuncOf(func(js.Value, []js.Value) any { setScroll(y); restore.Release(); return nil })
				window.Call("requestAnimationFrame", restore)
			}
			return nil
		})
		observerCallbackCreated = true
		observer = ctor.New(observerCallback)
		root := document.Call("querySelector", selector)
		if root.Truthy() {
			observer.Call("observe", root, map[string]any{"childList": true, "subtree": true})
			observerAttached = true
		}
	}
	return func() {
		window.Call("removeEventListener", "hashchange", hashChanged)
		window.Call("removeEventListener", "popstate", popState)
		window.Call("removeEventListener", "focusin", focusIn)
		window.Call("removeEventListener", "focusout", focusOut)
		hashChanged.Release()
		popState.Release()
		focusIn.Release()
		focusOut.Release()
		if observerAttached {
			observer.Call("disconnect")
		}
		if observerCallbackCreated {
			observerCallback.Release()
		}
	}
}

var defaultMount = NewMountLifecycle()

// StopMount releases the process-wide mount. It is primarily used by host
// teardown and tests; repeated calls are safe.
func StopMount() { defaultMount.Stop() }

// injectStylesheet puts the one hashed stylesheet into document.head via
// textContent, never innerHTML: Stylesheet() is our own static constant, so
// this is not an injection sink, and textContent cannot become one if that
// ever stops being true.
//
// It is idempotent by marker attribute rather than by a package-level bool,
// because the shell may already have inlined the same sheet (its sha256 is
// what the content-security-policy pins) and a second copy would be dead
// weight in the cascade.
func injectStylesheet() {
	document := js.Global().Get("document")
	if existing := document.Call("querySelector", "style[data-journey-stylesheet]"); existing.Truthy() {
		return
	}
	style := document.Call("createElement", "style")
	style.Call("setAttribute", "data-journey-stylesheet", "1")
	style.Set("textContent", Stylesheet())
	document.Get("head").Call("appendChild", style)
}
