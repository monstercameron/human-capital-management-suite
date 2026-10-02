//go:build js && wasm

package main

import (
	"strconv"
	"sync"
	"syscall/js"
)

var personaEditorCloseListeners sync.Once

// installPersonaEditorCloseHandlers lets Escape close the open version editor.
// It is installed after the document picker's own key handler, which closes an
// open result list first and marks the event handled.
func installPersonaEditorCloseHandlers() {
	personaEditorCloseListeners.Do(func() {
		document := js.Global().Get("document")
		if !document.Truthy() {
			return
		}
		keydown := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 || !args[0].Truthy() {
				return nil
			}
			event, target := args[0], args[0].Get("target")
			if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
				return nil
			}
			form := target.Call("closest", "form.persona-admin-version-editor")
			if !form.Truthy() || form.Get("hidden").Bool() {
				return nil
			}
			listOpen := domAttribute(target, "aria-expanded") == "true"
			if !personaEditorEscapeCloses(event.Get("key").String(), listOpen, event.Get("defaultPrevented").Bool()) {
				return nil
			}
			event.Call("preventDefault")
			personaEditorRequestClose(form)
			return nil
		})
		document.Call("addEventListener", "keydown", keydown)
	})
}

func personaEditorDocumentCount(form js.Value) int {
	return form.Call("querySelectorAll", "[data-agentdoc-reference]").Get("length").Int()
}

// personaEditorControls lists the fields a person edits. What is typed in the
// document search box is a query, not an edit, so it is left out.
func personaEditorControls(form js.Value) []js.Value {
	nodes := form.Call("querySelectorAll", "input:not([type=hidden]),textarea,select")
	controls := make([]js.Value, 0, nodes.Get("length").Int())
	for index := 0; index < nodes.Get("length").Int(); index++ {
		control := nodes.Call("item", index)
		if control.Get("dataset").Get("agentdocSearch").Type() != js.TypeString {
			controls = append(controls, control)
		}
	}
	return controls
}

func personaEditorControlValue(control js.Value) string {
	if kind := control.Get("type").String(); kind == "checkbox" || kind == "radio" {
		return strconv.FormatBool(control.Get("checked").Bool())
	}
	return control.Get("value").String()
}

func personaEditorSetExpanded(form js.Value, open bool) js.Value {
	opener := js.Global().Get("document").Call("querySelector", `[data-persona-editor-toggle="`+form.Get("id").String()+`"][aria-controls]`)
	if opener.Truthy() {
		opener.Call("setAttribute", "aria-expanded", strconv.FormatBool(open))
	}
	return opener
}

// personaEditorOpen shows the editor in the card, remembers what each field
// holds, and puts focus in the first field a person can type in.
func personaEditorOpen(form js.Value) {
	form.Set("hidden", false)
	form.Get("dataset").Set("personaEditorDocuments", strconv.Itoa(personaEditorDocumentCount(form)))
	for _, control := range personaEditorControls(form) {
		control.Get("dataset").Set("personaEditorInitial", personaEditorControlValue(control))
	}
	personaEditorSetExpanded(form, true)
	if first := form.Call("querySelector", personaEditorFirstField); first.Truthy() {
		first.Call("focus")
	}
}

// personaEditorRequestClose closes the editor for Cancel and Escape. When the
// person changed something it asks before discarding, and puts the fields back
// to what they held so the editor does not reopen half-edited.
func personaEditorRequestClose(form js.Value) {
	controls := personaEditorControls(form)
	fields := make([]personaEditorField, 0, len(controls))
	for _, control := range controls {
		initial := control.Get("dataset").Get("personaEditorInitial")
		if initial.Type() != js.TypeString {
			// A field that appeared after the editor opened has no remembered
			// value; it cannot have been changed away from one.
			continue
		}
		fields = append(fields, personaEditorField{Value: personaEditorControlValue(control), Initial: initial.String()})
	}
	initialDocuments, err := strconv.Atoi(domDataset(form, "personaEditorDocuments"))
	if err != nil {
		initialDocuments = personaEditorDocumentCount(form)
	}
	dirty := personaEditorDirty(fields, personaEditorDocumentCount(form), initialDocuments)
	// The Cancel button turns into the question (AGENTUX-050: no browser dialog);
	// pressing it again discards, and moving focus away keeps the edit.
	if !personaEditorClose(dirty, func() bool {
		cancel := form.Call("querySelector", "[data-persona-editor-toggle]")
		return inPageConfirmed(domDataset(form, "personaEditorDiscard"), cancel, true)
	}) {
		return
	}
	for _, control := range controls {
		initial := control.Get("dataset").Get("personaEditorInitial")
		if initial.Type() != js.TypeString {
			continue
		}
		if kind := control.Get("type").String(); kind == "checkbox" || kind == "radio" {
			control.Set("checked", initial.String() == "true")
		} else {
			control.Set("value", initial.String())
		}
	}
	form.Set("hidden", true)
	if opener := personaEditorSetExpanded(form, false); opener.Truthy() {
		opener.Call("focus")
	}
}
