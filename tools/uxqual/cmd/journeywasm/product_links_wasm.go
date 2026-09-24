//go:build js && wasm

package main

import "syscall/js"

// bindProductLinkFallback routes every plain click on an in-app link that no
// component handled through the software router. Pages render real hrefs so
// links stay shareable and open in a new tab, and most of them intercept
// their own clicks; this is the backstop for any that do not (a chat message
// link, an unfurl card, a chip rendered before its page wired Navigate).
// Without it the browser follows the href: a full reload that also restarts
// the history ledger, so Back and Forward lose their place.
func bindProductLinkFallback(navigate func(string)) {
	document := js.Global().Get("document")
	click := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		event := args[0]
		href, ok := productLinkFallbackHref(event)
		if !ok {
			return nil
		}
		event.Call("preventDefault")
		navigate(href)
		return nil
	})
	// Bubble phase on the document: component handlers below run first and
	// mark what they handled with preventDefault.
	document.Call("addEventListener", "click", click)
}

func productLinkFallbackHref(event js.Value) (href string, ok bool) {
	defer func() {
		if recover() != nil {
			href, ok = "", false
		}
	}()
	if event.Get("defaultPrevented").Bool() || event.Get("button").Int() != 0 ||
		event.Get("ctrlKey").Bool() || event.Get("metaKey").Bool() || event.Get("shiftKey").Bool() || event.Get("altKey").Bool() {
		return "", false
	}
	target := event.Get("target")
	if !target.Truthy() || !target.Get("closest").Truthy() {
		return "", false
	}
	anchor := target.Call("closest", "a[href]")
	if !anchor.Truthy() {
		return "", false
	}
	location := js.Global().Get("location")
	return productLinkFallbackTarget(productLinkFallbackAnchor{
		Href:     anchor.Get("href").String(),
		Target:   anchor.Get("target").String(),
		Download: anchor.Call("hasAttribute", "download").Bool(),
		Origin:   location.Get("origin").String(),
		Current:  location.Get("pathname").String() + location.Get("search").String(),
	})
}
