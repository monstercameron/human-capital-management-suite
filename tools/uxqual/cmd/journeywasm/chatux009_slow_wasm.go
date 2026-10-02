//go:build js && wasm

package main

import (
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATUX-009: a load that takes longer than five seconds says so, once. The
// skeleton carries the line from the start, hidden; this puts an attribute on
// the document element when the five seconds are up, which reveals it, and takes
// it off again when the load ends.

var chatux009Slow struct {
	sync.Mutex
	timer *time.Timer
	shown bool
}

// chatux009TrackLoading is called with true when a load begins or is still going
// and with false when the page has what it was loading.
func chatux009TrackLoading(loading bool) {
	chatux009Slow.Lock()
	defer chatux009Slow.Unlock()
	root := js.Global().Get("document").Get("documentElement")
	if loading {
		if chatux009Slow.timer != nil || chatux009Slow.shown {
			return
		}
		chatux009Slow.timer = time.AfterFunc(chatui.ChatSlowLoadAfter, func() {
			chatux009Slow.Lock()
			defer chatux009Slow.Unlock()
			if chatux009Slow.timer == nil {
				return
			}
			chatux009Slow.timer, chatux009Slow.shown = nil, true
			if root := js.Global().Get("document").Get("documentElement"); root.Truthy() {
				root.Call("setAttribute", chatui.ChatSlowLoadAttribute, "")
			}
		})
		return
	}
	if chatux009Slow.timer != nil {
		chatux009Slow.timer.Stop()
		chatux009Slow.timer = nil
	}
	if chatux009Slow.shown {
		chatux009Slow.shown = false
		if root.Truthy() {
			root.Call("removeAttribute", chatui.ChatSlowLoadAttribute)
		}
	}
}
