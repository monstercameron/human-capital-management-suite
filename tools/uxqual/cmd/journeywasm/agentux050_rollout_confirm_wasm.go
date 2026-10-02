//go:build js && wasm

package main

import "syscall/js"

// rolloutConfirmedInPage asks for a rollout step's confirmation on the page
// (AGENTUX-050). The first press turns the button into the confirmation
// sentence and does nothing else; a second press on it goes ahead, and moving
// focus away puts the original label back. The browser's own dialog, which
// every step used to open, is not used.
//
// sentence is the confirmation text the server rendered into the button or its
// form; with none, the press goes ahead at once.
func rolloutConfirmedInPage(sentence string, button js.Value) bool {
	if sentence == "" || !button.Truthy() {
		return true
	}
	if domDataset(button, "confirming") == "true" {
		rolloutDisarm(button)
		return true
	}
	button.Get("dataset").Set("confirmLabel", button.Get("textContent").String())
	button.Call("setAttribute", "data-confirming", "true")
	button.Set("textContent", sentence)
	var disarm js.Func
	disarm = js.FuncOf(func(js.Value, []js.Value) any {
		defer disarm.Release()
		if domDataset(button, "confirming") == "true" {
			rolloutDisarm(button)
		}
		return nil
	})
	button.Call("addEventListener", "blur", disarm, map[string]any{"once": true})
	return false
}

// rolloutDisarm takes a button out of its confirming state and gives it back its
// own label.
func rolloutDisarm(button js.Value) {
	label := domDataset(button, "confirmLabel")
	button.Call("removeAttribute", "data-confirming")
	button.Call("removeAttribute", "data-confirm-label")
	if label != "" {
		button.Set("textContent", label)
	}
	// A destructive confirmation took the danger style while it asked.
	if domDataset(button, "confirmDanger") == "true" {
		button.Call("removeAttribute", "data-confirm-danger")
		if class := button.Get("dataset").Get("confirmClass"); class.Type() == js.TypeString {
			button.Set("className", class.String())
		}
		button.Call("removeAttribute", "data-confirm-class")
	}
}
