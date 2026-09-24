//go:build js && wasm

package productui

import "syscall/js"

// docsWatchLayout keeps the pins and the connector beside their passages
// while the page scrolls or resizes. One animation-frame callback is made up
// front and scheduled at most once per frame; the cleanup removes both
// listeners, cancels a pending frame and releases the callbacks, so nothing
// keeps running once the document page is gone.
func docsWatchLayout() func() {
	scheduled := false
	var frameID js.Value
	frame := js.FuncOf(func(js.Value, []js.Value) any {
		scheduled = false
		docsPlacePins()
		docsDrawConnector()
		return nil
	})
	redraw := js.FuncOf(func(js.Value, []js.Value) any {
		if !scheduled {
			scheduled = true
			frameID = js.Global().Call("requestAnimationFrame", frame)
		}
		return nil
	})
	window := js.Global()
	window.Call("addEventListener", "scroll", redraw, map[string]any{"passive": true, "capture": true})
	window.Call("addEventListener", "resize", redraw, map[string]any{"passive": true})
	return func() {
		window.Call("removeEventListener", "scroll", redraw, map[string]any{"capture": true})
		window.Call("removeEventListener", "resize", redraw)
		if scheduled {
			window.Call("cancelAnimationFrame", frameID)
			scheduled = false
		}
		redraw.Release()
		frame.Release()
	}
}

// docsSetLinked shows which comment the pointer is on without re-rendering
// the page: the pin and the thread card get is-linked, the passage gets the
// active highlight, and the connector is redrawn. The layout effect calls it
// after every render as well, so classes the reconciler did not write are
// brought back in line with what is linked now.
func docsSetLinked(id string) {
	docsLinkState.linked = id
	doc := js.Global().Get("document")
	stale := doc.Call("querySelectorAll", ".docs-detail .docs-anchor-pin.is-linked,.docs-detail .docs-thread.is-linked")
	for i := 0; i < stale.Get("length").Int(); i++ {
		el := stale.Index(i)
		if el.Get("id").String() != "docs-pin-"+id && el.Get("id").String() != "docs-thread-"+id {
			el.Get("classList").Call("remove", "is-linked")
		}
	}
	if id != "" {
		for _, elementID := range []string{"docs-pin-" + id, "docs-thread-" + id} {
			if el := doc.Call("getElementById", elementID); el.Truthy() {
				el.Get("classList").Call("add", "is-linked")
			}
		}
	}
	if css := js.Global().Get("CSS"); css.Truthy() && css.Get("highlights").Truthy() && js.Global().Get("Highlight").Truthy() {
		current := js.Global().Get("Highlight").New()
		if r, ok := docsAnchorRanges[id]; ok {
			current.Call("add", r)
		}
		css.Get("highlights").Call("set", "docs-anchor-active", current)
	}
	docsDrawConnector()
}

// docsPaintDraft highlights the passage a new comment is being written
// about, so it stays marked after the selection itself is cleared.
func docsPaintDraft(quote, prefix, suffix string) {
	css := js.Global().Get("CSS")
	if !css.Truthy() || !css.Get("highlights").Truthy() || !js.Global().Get("Highlight").Truthy() {
		return
	}
	if quote == "" {
		css.Get("highlights").Call("delete", "docs-anchor-draft")
		return
	}
	index, ok := docsIndexText()
	if !ok {
		return
	}
	start, end, found := docsLocateQuote([]rune(index.text), quote, prefix, suffix)
	if !found {
		css.Get("highlights").Call("delete", "docs-anchor-draft")
		return
	}
	r, ok := index.rangeFor(start, end)
	if !ok {
		return
	}
	css.Get("highlights").Call("set", "docs-anchor-draft", js.Global().Get("Highlight").New(r))
}

// docsRevealComposer scrolls the comment composer just far enough to be
// seen; the passage it quotes keeps its draft highlight.
func docsRevealComposer() {
	if form := js.Global().Get("document").Call("getElementById", "docs-comment-form"); form.Truthy() {
		form.Call("scrollIntoView", map[string]any{"block": "nearest"})
	}
}
