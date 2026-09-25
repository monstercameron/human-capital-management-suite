//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestPreHydrationChatSearchSurvivesMount is the live finding's regression
// test: a value the server-rendered #chat-search already holds -- set, in
// the real world, by a reader typing during the few seconds this client
// takes to mount -- must not be lost when the client's own model starts
// empty. It must also keep focus and its caret.
func TestPreHydrationChatSearchSurvivesMount(t *testing.T) {
	oldDocument := js.Global().Get("document")
	oldCaptured, oldValues, oldFocus := preHydrationChatCaptured, preHydrationChatFieldValues, preHydrationChatFocusID
	t.Cleanup(func() {
		js.Global().Set("document", oldDocument)
		preHydrationChatCaptured, preHydrationChatFieldValues, preHydrationChatFocusID = oldCaptured, oldValues, oldFocus
	})
	preHydrationChatCaptured, preHydrationChatFieldValues, preHydrationChatFocusID = false, nil, ""

	object := js.Global().Get("Object")
	search := object.New()
	search.Set("id", "chat-search")
	search.Set("value", "budget rev")
	focusCalls := 0
	var selectionArgs []int
	callbacks := make([]js.Func, 0, 4)
	bind := func(target js.Value, name string, fn func(js.Value, []js.Value) any) {
		callback := js.FuncOf(fn)
		callbacks = append(callbacks, callback)
		target.Set(name, callback)
	}
	bind(search, "focus", func(js.Value, []js.Value) any { focusCalls++; return nil })
	bind(search, "setSelectionRange", func(_ js.Value, args []js.Value) any {
		if len(args) == 2 {
			selectionArgs = []int{args[0].Int(), args[1].Int()}
		}
		return nil
	})

	list := js.Global().Get("Array").New()
	list.Call("push", search)

	document := object.New()
	bind(document, "querySelectorAll", func(_ js.Value, args []js.Value) any {
		if len(args) == 1 && args[0].String() == "[data-chat-value]" {
			return list
		}
		return js.Global().Get("Array").New()
	})
	bind(document, "getElementById", func(_ js.Value, args []js.Value) any {
		if len(args) == 1 && args[0].String() == "chat-search" {
			return search
		}
		return js.Null()
	})
	// The reader was mid-keystroke: the box the server rendered is focused
	// when this client's very first line of code runs.
	document.Set("activeElement", search)
	js.Global().Set("document", document)
	t.Cleanup(func() {
		for _, callback := range callbacks {
			callback.Release()
		}
	})

	// This is the exact call start() makes, at the top, before dial() --
	// simulated here since start() itself needs a live gRPC target.
	capturePreHydrationChatFieldValues()

	oldClient, oldCfg, oldModel := chatBrowser.conversationClient(), chatBrowser.config(journeyclient.Config{}), chatBrowser.snapshot()
	chatBrowser.reset(nil, journeyclient.Config{Tenant: "tenant", Subject: "reader"}, nil)
	t.Cleanup(func() {
		chatBrowser.reset(oldClient, oldCfg, nil)
		chatBrowser.mutate(func(m *chatui.Model) { *m = oldModel })
	})

	// This is what renderChatPage does on its first render: fold the
	// capture into whatever Search the model already has (here, none).
	adopted := adoptPreHydrationChatSearch("")
	if adopted != "budget rev" {
		t.Fatalf("adoptPreHydrationChatSearch returned %q, want the text already in the box", adopted)
	}
	if got := chatBrowser.snapshot().Search; got != "budget rev" {
		t.Fatalf("chatBrowser model Search = %q, want the pre-hydration value to have been adopted", got)
	}

	// A second render (or a second field) must not re-adopt an id already
	// taken, so a search the reader has since cleared themselves is not
	// resurrected out from under them.
	if again := adoptPreHydrationChatSearch(""); again != "" {
		t.Fatalf("adoptPreHydrationChatSearch re-adopted a consumed value: %q", again)
	}

	restorePreHydrationChatFocus()
	if focusCalls != 1 {
		t.Fatalf("restorePreHydrationChatFocus called .focus() %d times, want 1", focusCalls)
	}
	if len(selectionArgs) != 2 || selectionArgs[0] != len("budget rev") || selectionArgs[1] != len("budget rev") {
		t.Fatalf("caret was not placed at the end of the adopted text: %v", selectionArgs)
	}
	if preHydrationChatFocusID != "" {
		t.Fatal("restorePreHydrationChatFocus left a focus id pending, so a later call would refocus it again")
	}
}

// TestPreHydrationChatDraftWaitsForAConversation covers the composer's
// half: the draft store is per conversation, so adoption must not fire
// (and must not consume the capture) before one is resolved.
func TestPreHydrationChatDraftWaitsForAConversation(t *testing.T) {
	oldCaptured, oldValues := preHydrationChatCaptured, preHydrationChatFieldValues
	t.Cleanup(func() { preHydrationChatCaptured, preHydrationChatFieldValues = oldCaptured, oldValues })
	preHydrationChatCaptured, preHydrationChatFieldValues = true, map[string]string{"chat-composer": "still typing this"}

	if got := adoptPreHydrationChatDraft("", ""); got != "" {
		t.Fatalf("adopted a composer draft with no conversation resolved yet: %q", got)
	}
	if _, ok := preHydrationChatFieldValues["chat-composer"]; !ok {
		t.Fatal("the capture was consumed before a conversation existed to attach it to")
	}

	oldClient, oldCfg := chatBrowser.conversationClient(), chatBrowser.config(journeyclient.Config{})
	chatBrowser.reset(nil, journeyclient.Config{Tenant: "tenant", Subject: "reader"}, nil)
	t.Cleanup(func() { chatBrowser.reset(oldClient, oldCfg, nil) })

	got := adoptPreHydrationChatDraft("room-1", "")
	if got != "still typing this" {
		t.Fatalf("adoptPreHydrationChatDraft returned %q, want the pre-hydration composer text", got)
	}
	chatBrowser.mutate(func(m *chatui.Model) { m.SelectedID = "room-1" })
	if got := chatBrowser.draft("room-1"); got != "still typing this" {
		t.Fatalf("draft store for room-1 = %q, want the adopted composer text", got)
	}
}
