//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

// fakeOpener is a control the person pane can be opened from.
func issue3874Opener(t *testing.T, name, id string, inDetails, inPane, connected bool, focused *[]string) js.Value {
	t.Helper()
	focus := js.FuncOf(func(js.Value, []js.Value) any { *focused = append(*focused, name); return nil })
	t.Cleanup(focus.Release)
	opener := map[string]any{"isConnected": connected, "focus": focus}
	obj := js.ValueOf(opener)
	closest := js.FuncOf(func(_ js.Value, args []js.Value) any {
		switch args[0].String() {
		case "[data-action='open-person']":
			return obj
		case ".person-pane":
			if inPane {
				return js.ValueOf(map[string]any{})
			}
		case ".chat-details":
			if inDetails {
				return js.ValueOf(map[string]any{})
			}
		}
		return js.Null()
	})
	t.Cleanup(closest.Release)
	getAttribute := js.FuncOf(func(js.Value, []js.Value) any { return id })
	t.Cleanup(getAttribute.Release)
	obj.Set("closest", closest)
	obj.Set("getAttribute", getAttribute)
	return obj
}

func issue3874Reset() {
	personPaneWasOpen, personPanePersonID = false, ""
	personTrigger, personTriggerID, personTriggerScope = js.Undefined(), "", ""
	personFocusComposer = false
}

func issue3874Frames(t *testing.T, query func(string) js.Value) {
	t.Helper()
	global := js.Global()
	oldDocument, oldFrame := global.Get("document"), global.Get("requestAnimationFrame")
	t.Cleanup(func() { global.Set("document", oldDocument); global.Set("requestAnimationFrame", oldFrame) })
	q := js.FuncOf(func(_ js.Value, args []js.Value) any { return query(args[0].String()) })
	t.Cleanup(q.Release)
	frame := js.FuncOf(func(_ js.Value, args []js.Value) any { args[0].Invoke(); return nil })
	t.Cleanup(frame.Release)
	global.Set("document", js.ValueOf(map[string]any{"querySelector": q}))
	global.Set("requestAnimationFrame", frame)
}

// Issue 3874: following the Manager from a person pane must not replace the
// control Close returns to.
func TestIssue3874_CloseReturnsToTheFirstAuthorAfterFollowingAManager(t *testing.T) {
	issue3874Reset()
	defer issue3874Reset()
	var focused []string
	author := issue3874Opener(t, "author", "camila", false, false, true, &focused)
	manager := issue3874Opener(t, "manager", "mateo", false, true, false, &focused)
	heading := js.ValueOf(map[string]any{"isConnected": true, "focus": js.FuncOf(func(js.Value, []js.Value) any { return nil })})
	issue3874Frames(t, func(string) js.Value { return heading })

	rememberPersonTriggerFrom(author)
	syncPersonFocusFor(true, "camila")
	rememberPersonTriggerFrom(manager) // the Manager link inside the open pane
	syncPersonFocusFor(true, "mateo")
	syncPersonFocus(false)
	if len(focused) != 1 || focused[0] != "author" {
		t.Fatalf("Close moved focus to %v, want the author who opened the pane", focused)
	}
}

// Issue 3874: Back to Conversation details lands on the member that was opened
// even though the panel was drawn again and the old row is gone.
func TestIssue3874_BackReturnsToTheMemberInConversationDetails(t *testing.T) {
	issue3874Reset()
	defer issue3874Reset()
	var focused []string
	staleRow := issue3874Opener(t, "stale", "amina", true, false, false, &focused)
	newRow := issue3874Opener(t, "member-row", "amina", true, false, true, &focused)
	closeButton := issue3874Opener(t, "close-details", "", true, false, true, &focused)
	var asked []string
	issue3874Frames(t, func(sel string) js.Value {
		asked = append(asked, sel)
		switch sel {
		case ".chat-details [data-action='open-person'][data-id=\"amina\"]":
			return newRow
		case ".chat-details [data-action='close-details']":
			return closeButton
		}
		return js.ValueOf(map[string]any{"isConnected": true, "focus": js.FuncOf(func(js.Value, []js.Value) any { return nil })})
	})
	rememberPersonTriggerFrom(staleRow)
	syncPersonFocusFor(true, "amina")
	syncPersonFocus(false)
	if len(focused) != 1 || focused[0] != "member-row" {
		t.Fatalf("Back moved focus to %v (queries %v), want the member row", focused, asked)
	}

	// With no row to return to, the panel's own close button takes focus rather than BODY.
	focused = nil
	issue3874Reset()
	issue3874Frames(t, func(sel string) js.Value {
		if sel == ".chat-details [data-action='close-details']" {
			return closeButton
		}
		return js.Null()
	})
	rememberPersonTriggerFrom(staleRow)
	syncPersonFocusFor(true, "amina")
	syncPersonFocus(false)
	if len(focused) != 1 || focused[0] != "close-details" {
		t.Fatalf("fallback moved focus to %v, want the details close button", focused)
	}
}

// A person opened from elsewhere in the conversation while a pane is open (not
// followed from inside the pane) becomes the control Close returns to.
func TestIssue3874_CloseReturnsToTheLatestAuthorOpenedOutsideThePane(t *testing.T) {
	issue3874Reset()
	defer issue3874Reset()
	var focused []string
	first := issue3874Opener(t, "first", "camila", false, false, true, &focused)
	second := issue3874Opener(t, "second", "mateo", false, false, true, &focused)
	heading := js.ValueOf(map[string]any{"isConnected": true, "focus": js.FuncOf(func(js.Value, []js.Value) any { return nil })})
	issue3874Frames(t, func(string) js.Value { return heading })
	rememberPersonTriggerFrom(first)
	syncPersonFocusFor(true, "camila")
	rememberPersonTriggerFrom(second)
	syncPersonFocusFor(true, "mateo")
	syncPersonFocus(false)
	if len(focused) != 1 || focused[0] != "second" {
		t.Fatalf("Close moved focus to %v, want the author opened last", focused)
	}
}
