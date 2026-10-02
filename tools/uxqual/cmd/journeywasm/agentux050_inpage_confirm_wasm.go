//go:build js && wasm

package main

import (
	"strings"
	"syscall/js"
)

// inPageConfirmed is the in-page two-press confirmation of AGENTUX-050 for any
// button that used to open the browser's confirm dialog: the first press turns
// the button into the confirmation sentence and returns false, a second press
// on it returns true, and moving focus away puts the label back. A destructive
// action also takes the danger style while it asks. With no sentence the press
// goes ahead at once.
func inPageConfirmed(sentence string, button js.Value, danger bool) bool {
	if sentence == "" {
		return true
	}
	if !button.Truthy() {
		// Nothing to turn into the question: the action does not go ahead unasked.
		return false
	}
	if rolloutConfirmedInPage(sentence, button) {
		return true
	}
	// Arming: keyboard users are already on the button; a press by Escape or from
	// another control brings focus to it so the next press lands on it.
	if danger {
		class := button.Get("className").String()
		button.Get("dataset").Set("confirmClass", class)
		button.Call("setAttribute", "data-confirm-danger", "true")
		kept := make([]string, 0, 4)
		for _, name := range strings.Fields(class) {
			if name != "secondary" && name != "destructive" {
				kept = append(kept, name)
			}
		}
		button.Set("className", strings.Join(append(kept, "destructive"), " "))
	}
	button.Call("focus", map[string]any{"preventScroll": true})
	return false
}

// inPageReason asks for the reason of a rejection without the browser's prompt
// dialog. The first press opens a labelled field beside the button and returns
// false; once a reason is typed, the next press returns it. An empty field keeps
// the press from going ahead.
func inPageReason(wrapper, button js.Value, question string) (string, bool) {
	if !wrapper.Truthy() {
		return "", false
	}
	field := wrapper.Call("querySelector", "input[data-review-reason-input]")
	if field.Truthy() {
		if reason := strings.TrimSpace(field.Get("value").String()); reason != "" {
			field.Call("remove")
			if label := wrapper.Call("querySelector", "label[data-review-reason-label]"); label.Truthy() {
				label.Call("remove")
			}
			return reason, true
		}
		field.Call("focus")
		return "", false
	}
	doc := js.Global().Get("document")
	id := "review-reason-" + strings.ReplaceAll(domDataset(button, "personaCommand")+"-"+domDataset(wrapper, "reviewDecisionWrapper"), " ", "")
	label := doc.Call("createElement", "label")
	label.Set("htmlFor", id)
	label.Set("textContent", question)
	label.Call("setAttribute", "data-review-reason-label", "true")
	field = doc.Call("createElement", "input")
	field.Set("id", id)
	field.Set("type", "text")
	field.Set("maxLength", 500)
	field.Set("className", "chat-input")
	field.Call("setAttribute", "data-review-reason-input", "true")
	field.Call("setAttribute", "autocomplete", "off")
	button.Call("before", label, field)
	field.Call("focus")
	return "", false
}
