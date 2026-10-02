//go:build js && wasm

package chatui

import (
	"context"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chatlangOpenSettings opens the Chat preferences panel, where the reading
// language is, by pressing the gear in the Conversations heading, and opens the
// Reading language row's settings in it (CHATBUG-045). A panel that is already
// open stays open, and a row that is already expanded stays expanded.
func chatlangOpenSettings() {
	doc := js.Global().Get("document")
	if gear := doc.Call("querySelector", ".chatux002-gear"); gear.Truthy() {
		if gear.Call("getAttribute", "aria-expanded").String() != "true" {
			gear.Call("click")
		}
	}
	if change := doc.Call("querySelector", `[data-prefs-section="reading-languages"] .chat-prefs-change`); change.Truthy() {
		if change.Call("getAttribute", "aria-expanded").String() != "true" {
			change.Call("click")
		}
	}
}

const chatlangBarStorageKey = "hcm.chat.chatlang.bar.dismissed"

// chatlangBarStoredDismissed reports whether the person dismissed the
// conversation bar before. Storage can be missing or refuse (a private window,
// blocked site data); then the bar simply shows.
func chatlangBarStoredDismissed() (dismissed bool) {
	defer func() {
		if recover() != nil {
			dismissed = false
		}
	}()
	store := js.Global().Get("localStorage")
	if !store.Truthy() {
		return false
	}
	value := store.Call("getItem", chatlangBarStorageKey)
	return value.Type() == js.TypeString && value.String() == "1"
}

// chatlangRememberBarDismissed remembers a dismissal in the browser, best effort.
func chatlangRememberBarDismissed() {
	defer func() { _ = recover() }()
	if store := js.Global().Get("localStorage"); store.Truthy() {
		store.Call("setItem", chatlangBarStorageKey, "1")
	}
}

// chatlangCorrect sends the writer's correction and, when the server accepted
// it, asks the page to read every message's selection again.
func chatlangCorrect(room, post string, revision uint64, language string, done func(error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := renderingSettingsClient().CorrectLanguage(ctx, room, post, revision, language)
		if err == nil {
			ui.PostAsync(func() {
				js.Global().Get("document").Call("dispatchEvent", js.Global().Get("Event").New("chat-reading-settings-changed"))
			})
		}
		done(err)
	}()
}
