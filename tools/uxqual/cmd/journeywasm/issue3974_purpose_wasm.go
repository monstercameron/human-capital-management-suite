//go:build js && wasm

package main

import "syscall/js"

// showAgentPickerPurposeWording (#3974) swaps the General agent's description to
// the wording that points at the document picker. The page is drawn without
// that wording; the client calls this only after it has mounted the picker, so
// the text never sends anyone to a control that is not there.
func showAgentPickerPurposeWording() {
	nodes := js.Global().Get("document").Call("querySelectorAll", "[data-agent-general-purpose]")
	for index := 0; index < nodes.Get("length").Int(); index++ {
		node := nodes.Call("item", index)
		text := domDataset(node, "purposeDocuments")
		if text == "" {
			continue
		}
		if inner := node.Call("querySelector", "bdi"); inner.Truthy() {
			inner.Set("textContent", text)
		} else {
			node.Set("textContent", text)
		}
	}
}
