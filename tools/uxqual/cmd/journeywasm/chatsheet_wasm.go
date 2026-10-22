//go:build js && wasm

package main

import (
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatSheetLoadTimeout is how long a navigation into Chat waits for the sheet
// before it goes ahead without it. A sheet that is slow is worth waiting for;
// one that never answers must not keep the person on the page they are leaving.
const chatSheetLoadTimeout = 5 * time.Second

// newChatSheetGateFromDocument reads what the document says about Chat's sheet:
// a link to it (this is a Chat page, the sheet is in), a meta element naming it
// (load it on the way into Chat), or neither.
func newChatSheetGateFromDocument() *chatSheetGate {
	document := js.Global().Get("document")
	linked := document.Call("querySelector", `link[rel="stylesheet"][href^="`+productui.StylesheetAssetPath+productui.ChatStylesheetAssetPrefix+`"]`)
	if linked.Truthy() {
		return newChatSheetGate(chatSheetReady, nil)
	}
	note := document.Call("querySelector", `meta[name="`+productui.ChatStylesheetMetaName+`"]`)
	if !note.Truthy() {
		return newChatSheetGate(chatSheetUnnamed, nil)
	}
	href := note.Call("getAttribute", "content").String()
	integrity := note.Call("getAttribute", "data-integrity").String()
	return newChatSheetGate(chatSheetNamed, func(done func(loaded bool)) {
		loadChatSheet(document, href, integrity, done)
	})
}

// holdChatSheetHistoryArrivals keeps a Back or Forward arrival at Chat from
// the router until the sheet is in the page. The listener is on the window in
// the capture phase, which runs before the router's own popstate listener; it
// stops the event and, once the gate releases it, dispatches a copy for the
// router to take as if it had just happened.
func holdChatSheetHistoryArrivals(gate *chatSheetGate) {
	window := js.Global()
	redispatching := false
	listener := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if redispatching || len(args) == 0 {
			return nil
		}
		event := args[0]
		location := window.Get("location")
		href := location.Get("pathname").String() + location.Get("search").String()
		state := event.Get("state")
		held := gate.Arrive(href, func() {
			// The release may run on the load's goroutine, so the copy is
			// dispatched from one of its own.
			go func() {
				redispatching = true
				copyOfEvent := window.Get("PopStateEvent").New("popstate", map[string]any{"state": state})
				window.Call("dispatchEvent", copyOfEvent)
				redispatching = false
			}()
		})
		if held {
			event.Call("stopImmediatePropagation")
		}
		return nil
	})
	window.Call("addEventListener", "popstate", listener, true)
}

// loadChatSheet adds the sheet's link to the head, with the integrity value the
// document named, and reports the browser's answer once.
func loadChatSheet(document js.Value, href, integrity string, done func(loaded bool)) {
	link := document.Call("createElement", "link")
	link.Set("rel", "stylesheet")
	link.Call("setAttribute", "integrity", integrity)
	link.Set("href", href)

	var once sync.Once
	var onLoad, onError js.Func
	var timer *time.Timer
	report := func(loaded bool) {
		once.Do(func() {
			// A js.Func must not block or release itself while it runs, and
			// done releases a navigation, so the report is made from a
			// goroutine of its own.
			go func() {
				timer.Stop()
				link.Call("removeEventListener", "load", onLoad)
				link.Call("removeEventListener", "error", onError)
				onLoad.Release()
				onError.Release()
				done(loaded)
			}()
		})
	}
	onLoad = js.FuncOf(func(js.Value, []js.Value) any { report(true); return nil })
	onError = js.FuncOf(func(js.Value, []js.Value) any { report(false); return nil })
	timer = time.AfterFunc(chatSheetLoadTimeout, func() { report(false) })
	link.Call("addEventListener", "load", onLoad)
	link.Call("addEventListener", "error", onError)
	document.Get("head").Call("appendChild", link)
}
