//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

// fakeUndoDocument is a document whose execCommand edits the field the way a
// browser does, and records the commands: the browser puts only edits made
// through a command on the undo stack.
func fakeUndoDocument(t *testing.T, value string) (doc, field js.Value, commands *[]string, inputs *int) {
	t.Helper()
	global := js.Global()
	field = global.Get("Object").New()
	field.Set("value", value)
	field.Set("selectionStart", len(value))
	field.Set("selectionEnd", len(value))
	field.Set("focus", js.FuncOf(func(js.Value, []js.Value) any { return nil }))
	field.Set("setSelectionRange", js.FuncOf(func(_ js.Value, a []js.Value) any {
		field.Set("selectionStart", a[0])
		field.Set("selectionEnd", a[1])
		return nil
	}))
	count := 0
	inputs, commands = &count, new([]string)
	field.Set("dispatchEvent", js.FuncOf(func(js.Value, []js.Value) any { count++; return true }))
	doc = global.Get("Object").New()
	doc.Set("getElementById", js.FuncOf(func(js.Value, []js.Value) any { return field }))
	doc.Set("execCommand", js.FuncOf(func(_ js.Value, a []js.Value) any {
		*commands = append(*commands, a[0].String())
		text := ""
		if a[0].String() == "insertText" {
			text = a[2].String()
		}
		old := []rune(field.Get("value").String())
		from, to := field.Get("selectionStart").Int(), field.Get("selectionEnd").Int()
		field.Set("value", string(old[:from])+text+string(old[to:]))
		count++ // a real command fires the input event itself
		return true
	}))
	return doc, field, commands, inputs
}

func withFakeDocument(t *testing.T, doc js.Value) {
	global := js.Global()
	previous := global.Get("document")
	global.Set("document", doc)
	t.Cleanup(func() { global.Set("document", previous) })
}

// TestIssue3902_FormatGoesThroughTheEditingCommand: Bold on a selection is one
// insertText command, so Ctrl+Z reverses it; the field ends with the same text.
func TestIssue3902_FormatGoesThroughTheEditingCommand(t *testing.T) {
	doc, field, commands, inputs := fakeUndoDocument(t, "Please review this draft")
	withFakeDocument(t, doc)
	field.Set("selectionStart", 7)
	field.Set("selectionEnd", 13)
	applyComposerFormat("chat-composer", "bold")
	if got := field.Get("value").String(); got != "Please **review** this draft" {
		t.Fatalf("value = %q", got)
	}
	if len(*commands) != 1 || (*commands)[0] != "insertText" {
		t.Fatalf("commands = %v, want one insertText", *commands)
	}
	if *inputs != 1 {
		t.Fatalf("input events = %d, want exactly the command's own", *inputs)
	}
}

// TestIssue3902_EmojiGoesThroughTheEditingCommand: the picker's emoji is one
// insertText command at the caret.
func TestIssue3902_EmojiGoesThroughTheEditingCommand(t *testing.T) {
	doc, field, commands, _ := fakeUndoDocument(t, "Test ")
	withFakeDocument(t, doc)
	insertComposerEmoji("chat-composer", "🍕", false)
	if got := field.Get("value").String(); got != "Test 🍕" {
		t.Fatalf("value = %q", got)
	}
	if len(*commands) != 1 || (*commands)[0] != "insertText" {
		t.Fatalf("commands = %v, want one insertText", *commands)
	}
}

// TestIssue3902_MentionGoesThroughTheEditingCommand: the @ button is one
// insertText command at the caret, so Ctrl+Z removes the @.
func TestIssue3902_MentionGoesThroughTheEditingCommand(t *testing.T) {
	doc, field, commands, _ := fakeUndoDocument(t, "Hello ")
	withFakeDocument(t, doc)
	insertComposerMention("chat-composer")
	if got := field.Get("value").String(); got != "Hello @" {
		t.Fatalf("value = %q", got)
	}
	if len(*commands) != 1 || (*commands)[0] != "insertText" {
		t.Fatalf("commands = %v, want one insertText", *commands)
	}
}

// TestIssue3902_RefusedCommandFallsBackToTheValueWrite: where the editing
// command is refused the edit still lands and the draft hears one input event.
func TestIssue3902_RefusedCommandFallsBackToTheValueWrite(t *testing.T) {
	doc, field, _, inputs := fakeUndoDocument(t, "Test ")
	doc.Set("execCommand", js.FuncOf(func(js.Value, []js.Value) any { return false }))
	withFakeDocument(t, doc)
	insertComposerEmoji("chat-composer", "🍕", false)
	if got := field.Get("value").String(); got != "Test 🍕" {
		t.Fatalf("value = %q, want the fallback write", got)
	}
	if *inputs != 1 {
		t.Fatalf("input events = %d, want the fallback's one", *inputs)
	}
}
