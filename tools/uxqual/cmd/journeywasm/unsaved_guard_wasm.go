//go:build js && wasm

package main

import "syscall/js"

// bindUnsavedFormGuard protects long admin editors without making the
// presentation layer an authority. The server-rendered form declares its
// dirty state; this enhancer only asks for confirmation before leaving it.
// Delegated click handling also covers keyboard activation because browsers
// dispatch the same click event for Enter/Space on links.
func bindUnsavedFormGuard() {
	document := js.Global().Get("document")
	window := js.Global()
	click := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		event := args[0]
		target := event.Get("target")
		if !target.Truthy() || target.Get("nodeType").Int() != 1 {
			return nil
		}
		link := target.Call("closest", "a[href]")
		if !link.Truthy() || link.Get("target").String() == "_blank" || link.Get("download").Truthy() {
			return nil
		}
		root := document.Call("querySelector", "[data-unsaved-protection='true']")
		if !root.Truthy() {
			return nil
		}
		form := root.Call("querySelector", "[data-unsaved-form]")
		state := unsavedGuardState{
			Protected:   true,
			Dirty:       domDataset(root, "unsaved") == "true",
			RootMessage: domDataset(root, "unsavedMessage"),
		}
		if form.Truthy() {
			state.FormMessage = domDataset(form, "unsavedMessage")
		}
		message, guarded := state.prompt()
		if !guarded {
			return nil
		}
		href := link.Get("href").String()
		if !shouldBlockUnsavedNavigation(true, href, window.Get("location").Get("href").String()) {
			return nil
		}
		if !window.Call("confirm", message).Bool() {
			event.Call("preventDefault")
			event.Call("stopPropagation")
		}
		return nil
	})
	document.Call("addEventListener", "click", click, true)

	beforeUnload := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		root := document.Call("querySelector", "[data-unsaved-protection='true']")
		if root.Truthy() && domDataset(root, "unsaved") == "true" {
			event := args[0]
			event.Call("preventDefault")
			event.Set("returnValue", "")
		}
		return nil
	})
	window.Call("addEventListener", "beforeunload", beforeUnload)
}
