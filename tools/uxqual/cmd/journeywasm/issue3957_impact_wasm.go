//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// The check reads the work of the open conversation with the work panel's own
// read, redraws the chat page after each step and then moves focus; all of it
// exists only in the browser build.
func init() {
	impactActive.refresh = refreshChatRoute
	impactActive.spawn = func(fn func()) { go fn() }
	impactActive.post = ui.PostAsync
	impactActive.focus = func(selector string) {
		ui.PostAsync(func() {
			if el := js.Global().Get("document").Call("querySelector", selector); el.Truthy() {
				el.Call("focus")
			}
		})
	}
	impactActive.read = func(ctx context.Context, conversation string) (chatui.LinkedWorkState, error) {
		b := linkedworkActive
		if b == nil || b.disposed {
			return chatui.LinkedWorkState{}, chatworkError("unavailable")
		}
		return linkedworkRead(ctx, http.DefaultClient, b.cfg, conversation)
	}
}
