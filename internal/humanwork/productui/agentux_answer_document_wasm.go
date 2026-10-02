//go:build js && wasm

package productui

import (
	"net/url"
	"strings"
	"syscall/js"
)

func agentAnswerDocumentRoute() AgentAnswerDocumentCitation {
	location := js.Global().Get("location")
	query, _ := url.ParseQuery(strings.TrimPrefix(location.Get("search").String(), "?"))
	anchor, _ := url.PathUnescape(strings.TrimPrefix(location.Get("hash").String(), "#"))
	return AgentAnswerDocumentCitation{VersionID: query.Get("version"), Anchor: anchor}
}

func agentAnswerRevealSection(anchor string) func() {
	document := js.Global().Get("document")
	target := document.Call("getElementById", anchor)
	headings := document.Call("querySelectorAll", "[data-document-section]")
	for index := 0; index < headings.Length(); index++ {
		heading := headings.Index(index)
		if heading.Call("getAttribute", "data-document-section").String() == strings.TrimPrefix(anchor, "sec-") {
			target = heading
			break
		}
	}
	if !target.Truthy() {
		target = document.Call("getElementById", "sec-"+anchor)
	}
	if !target.Truthy() {
		target = document.Call("getElementById", "page-title")
	}
	if !target.Truthy() {
		return nil
	}
	target.Get("style").Set("scrollMarginTop", "8rem")
	target.Call("setAttribute", "tabindex", "-1")
	target.Call("scrollIntoView", map[string]any{"block": "start", "behavior": "auto"})
	target.Call("focus", map[string]any{"preventScroll": true})
	target.Get("classList").Call("add", "docs-cited-section")
	remove := js.FuncOf(func(js.Value, []js.Value) any {
		target.Get("classList").Call("remove", "docs-cited-section")
		return nil
	})
	timer := js.Global().Call("setTimeout", remove, 4000)
	return func() {
		js.Global().Call("clearTimeout", timer)
		target.Get("classList").Call("remove", "docs-cited-section")
		remove.Release()
	}
}
