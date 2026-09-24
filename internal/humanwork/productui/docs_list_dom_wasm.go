//go:build js && wasm

package productui

import "syscall/js"

// docsSearchFocused reports whether the Docs search box holds focus, so an
// address change made while the person is typing never overwrites the box.
func docsSearchFocused() bool {
	doc := js.Global().Get("document")
	active := doc.Get("activeElement")
	return active.Truthy() && active.Get("id").String() == "docs-browse-query"
}

// setDocsSelectAllMixed shows the list's select-all box as mixed while some,
// but not all, rows are selected. indeterminate is a DOM property with no
// attribute, so it is set here rather than rendered.
func setDocsSelectAllMixed(mixed bool) {
	box := js.Global().Get("document").Call("querySelector", ".docs-row-head .docs-cell-select input[type=checkbox]")
	if box.Truthy() && box.Get("indeterminate").Bool() != mixed {
		box.Set("indeterminate", mixed)
	}
}
