//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Live finding: a reader who starts typing into #chat-search or the composer
// during the few seconds this client takes to mount loses it. The server
// renders those inputs live and interactive; this client's first render
// starts every field from an empty model, and fieldsync_js.go's field-sync
// observer then treats that empty model value as authoritative and writes it
// over whatever is already in the box -- it has no way to tell "the app has
// not caught up to what you typed" apart from "the app cleared this on send".
//
// The fix is to read whatever the SSR box already holds before this client
// does anything else, and adopt it into the model the very first render
// builds, so the model and the box already agree by the time field-sync's
// first sweep runs. Capturing happens at the top of start() (main_wasm.go),
// before dial() and before any render or hydrate call could touch the DOM.

// preHydrationChatFieldValues is id -> value for every [data-chat-value]
// input the server rendered with non-empty text still in it. Consumed one id
// at a time by takePreHydrationChatFieldValue; once taken, an id never comes
// back, so a later, legitimate empty state is never overwritten by a stale
// capture.
var preHydrationChatFieldValues map[string]string

// preHydrationChatFocusID is the id of whichever such input had focus at
// capture time, if any. A reader who was mid-keystroke when this client
// mounted keeps their place, not just their text.
var preHydrationChatFocusID string

var preHydrationChatCaptured bool

// capturePreHydrationChatFieldValues reads every SSR text input's live value
// straight off the DOM the server sent. Call once, as early as possible.
func capturePreHydrationChatFieldValues() {
	if preHydrationChatCaptured {
		return
	}
	preHydrationChatCaptured = true
	doc := js.Global().Get("document")
	if !doc.Truthy() || doc.Get("querySelectorAll").Type() != js.TypeFunction {
		return
	}
	found := doc.Call("querySelectorAll", "[data-chat-value]")
	length := found.Length()
	if length == 0 {
		return
	}
	values := make(map[string]string, length)
	active := doc.Get("activeElement")
	for i := 0; i < length; i++ {
		el := found.Index(i)
		id := el.Get("id").String()
		if id == "" {
			continue
		}
		raw := el.Get("value")
		if raw.Type() != js.TypeString {
			continue
		}
		value := raw.String()
		if value == "" {
			continue
		}
		values[id] = value
		if active.Truthy() && active.Equal(el) {
			preHydrationChatFocusID = id
		}
	}
	if len(values) > 0 {
		preHydrationChatFieldValues = values
	}
}

// takePreHydrationChatFieldValue returns id's captured value once. A second
// call for the same id reports ok=false, so this is safe to call from every
// render without re-adopting a value the reader has since cleared themselves.
func takePreHydrationChatFieldValue(id string) (string, bool) {
	if preHydrationChatFieldValues == nil {
		return "", false
	}
	value, ok := preHydrationChatFieldValues[id]
	if ok {
		delete(preHydrationChatFieldValues, id)
	}
	return value, ok
}

// adoptPreHydrationChatSearch folds a captured #chat-search value into
// chatBrowser's model, once, and returns the value this render should use.
// current is passed in so a room already carrying a real search (restored
// from history, or typed after mount) is never overwritten.
func adoptPreHydrationChatSearch(current string) string {
	value, ok := takePreHydrationChatFieldValue("chat-search")
	if !ok || current != "" {
		return current
	}
	chatBrowser.mutate(func(m *chatui.Model) { m.Search = value })
	return value
}

// adoptPreHydrationChatDraft is adoptPreHydrationChatSearch's counterpart for
// the composer. The draft store is per conversation, so this needs a
// resolved conversation id and is only meaningful once one exists.
func adoptPreHydrationChatDraft(conversationID, current string) string {
	if conversationID == "" {
		return current
	}
	value, ok := takePreHydrationChatFieldValue("chat-composer")
	if !ok || current != "" {
		return current
	}
	chatBrowser.setDraft(conversationID, value)
	return value
}

// restorePreHydrationChatFocus refocuses whichever SSR input had focus
// before this client mounted, since the first render can replace that DOM
// node out from under the reader. Caret goes to the end of the adopted text,
// the same place a draft restored on a conversation switch lands.
func restorePreHydrationChatFocus() {
	if preHydrationChatFocusID == "" {
		return
	}
	id := preHydrationChatFocusID
	preHydrationChatFocusID = ""
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	el := doc.Call("getElementById", id)
	if !el.Truthy() || el.Get("focus").Type() != js.TypeFunction {
		return
	}
	el.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	if el.Get("setSelectionRange").Type() != js.TypeFunction {
		return
	}
	n := len([]rune(el.Get("value").String()))
	func() {
		defer func() { _ = recover() }()
		el.Call("setSelectionRange", n, n)
	}()
}
