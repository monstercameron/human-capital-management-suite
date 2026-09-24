//go:build js && wasm

package main

import (
	"syscall/js"
	"time"
)

// productMainScrollOwnerID is the product shell's one main content scroll
// owner (productui's <main id="main-content" class="main-scroll">).
const productMainScrollOwnerID = "main-content"

// productRestoreFrames bounds how long a Back/Forward restore waits for the
// destination's content to grow tall enough to hold the saved position.
// A list returning from a document can take most of a second to render.
const productRestoreFrames = 60

var productScroll *browserProductScrollController

// browserProductScrollController adapts the browser to the router's scroll
// ledger (UXLIVE-028). It takes over the browser's own history scroll
// restoration, records the reader's position in the main region per history
// entry, and applies the ledger's decision after each routed render.
type browserProductScrollController struct {
	ledger    *productScrollLedger
	listeners []js.Func
}

func newBrowserProductScrollController() *browserProductScrollController {
	return &browserProductScrollController{ledger: newProductScrollLedger(time.Now)}
}

// Bind installs the document-level listeners once. Scroll does not bubble,
// so the listener captures it and keeps only the main region's own events.
func (controller *browserProductScrollController) Bind() {
	if controller == nil {
		return
	}
	if history, ok := browserHistory(); ok {
		// The document never scrolls (the shell clips it); the main region
		// is restored by this controller rather than by the browser.
		history.Set("scrollRestoration", "manual")
	}
	scroll := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target, ok := browserProperty(args[0], "target")
		if !ok || target.Type() != js.TypeObject {
			return nil
		}
		if id, idOK := browserProperty(target, "id"); !idOK || id.Type() != js.TypeString || id.String() != productMainScrollOwnerID {
			return nil
		}
		if top, topOK := browserProperty(target, "scrollTop"); topOK && top.Type() == js.TypeNumber {
			controller.ledger.Observe(top.Float())
		}
		return nil
	})
	popstate := js.FuncOf(func(js.Value, []js.Value) any {
		controller.BeginTraversal()
		return nil
	})
	controller.listeners = append(controller.listeners, scroll, popstate)
	if document, ok := browserProperty(js.Global(), "document"); ok && document.Type() == js.TypeObject {
		browserCall(document, "addEventListener", "scroll", scroll, map[string]any{"capture": true, "passive": true})
	}
	// Capture: at the window target a capturing listener runs before the
	// router's own popstate handler, so the traversal is marked before any
	// render could settle it.
	browserCall(js.Global(), "addEventListener", "popstate", popstate, map[string]any{"capture": true})
}

// BeginSoftwareNavigation is called before the router pushes a new entry.
func (controller *browserProductScrollController) BeginSoftwareNavigation() {
	if controller == nil {
		return
	}
	if top, ok := productMainScrollTop(); ok {
		controller.ledger.Observe(top)
	}
	controller.ledger.BeginNavigation(false)
}

// BeginTraversal is called when the browser moves through history. The
// departing entry's position is the last one the reader produced; the
// region may already be clamped against the next render.
func (controller *browserProductScrollController) BeginTraversal() {
	if controller == nil {
		return
	}
	controller.ledger.BeginNavigation(true)
}

// Settle adopts the rendered entry and returns the ledger's decision.
func (controller *browserProductScrollController) Settle(route string) (productScrollAction, float64) {
	if controller == nil {
		return productScrollKeep, 0
	}
	return controller.ledger.Settle(currentProductScrollEntryKey(route), route)
}

func currentProductScrollEntryKey(route string) string {
	ledgerID, index := "", 0
	if history, ok := browserHistory(); ok {
		if state, stateOK := browserHistoryState(history); stateOK {
			if id, entryIndex, entryOK := productHistoryState(state); entryOK {
				ledgerID, index = id, entryIndex
			}
		}
	}
	return productScrollEntryKey(ledgerID, index, route)
}

func productMainScrollOwner() (js.Value, bool) {
	document, ok := browserProperty(js.Global(), "document")
	if !ok || document.Type() != js.TypeObject {
		return js.Undefined(), false
	}
	main, ok := browserCall(document, "getElementById", productMainScrollOwnerID)
	if !ok || main.Type() != js.TypeObject {
		return js.Undefined(), false
	}
	return main, true
}

func productMainScrollTop() (float64, bool) {
	main, ok := productMainScrollOwner()
	if !ok {
		return 0, false
	}
	top, ok := browserProperty(main, "scrollTop")
	if !ok || top.Type() != js.TypeNumber {
		return 0, false
	}
	return top.Float(), true
}

// applyProductScroll puts the main region where the ledger decided. It is
// the only place the product router writes the main region's position.
func applyProductScroll(action productScrollAction, top float64) {
	main, ok := productMainScrollOwner()
	if !ok {
		return
	}
	switch action {
	case productScrollTop:
		main.Set("scrollTop", 0)
	case productScrollKeep:
		// A re-mounted region starts at 0; put the reader back. An intact
		// region already holds the position and is left untouched.
		if current, currentOK := productMainScrollTop(); currentOK && current+1 < top {
			main.Set("scrollTop", top)
		}
	case productScrollRestore:
		restoreProductScroll(top, 0)
	}
}

// restoreProductScroll writes the saved position and retries for a bounded
// number of frames while the destination's content is still growing.
func restoreProductScroll(top float64, frame int) {
	main, ok := productMainScrollOwner()
	if !ok {
		return
	}
	main.Set("scrollTop", top)
	current, currentOK := productMainScrollTop()
	if !currentOK || current+1 >= top || frame >= productRestoreFrames {
		if currentOK && productScroll != nil {
			productScroll.ledger.Observe(current)
		}
		return
	}
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		defer callback.Release()
		restoreProductScroll(top, frame+1)
		return nil
	})
	js.Global().Call("requestAnimationFrame", callback)
}
