//go:build js && wasm

package main

import (
	"sync"
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatsearchOpened remembers which result was opened and how far the results
// were scrolled, so "Return to results" can put both back (#3896).
var chatsearchOpened struct {
	sync.Mutex
	origin chatsearchOrigin
}

// chatsearchRememberOrigin is called with the open control that was activated,
// before the conversation replaces the results.
func chatsearchRememberOrigin(button js.Value) {
	origin := chatsearchOrigin{
		Target: button.Call("getAttribute", "data-target").String(),
		Kind:   button.Call("getAttribute", "data-kind").String(),
	}
	if list := js.Global().Get("document").Call("querySelector", ".chat-search-results"); list.Truthy() {
		origin.Scroll = list.Get("scrollTop").Float()
	}
	chatsearchOpened.Lock()
	chatsearchOpened.origin = origin
	chatsearchOpened.Unlock()
}

// chatsearchFocusAfterReturn puts focus and scroll back on the result that was
// opened, or on the summary when it is gone. The results are drawn a moment
// after the model changes, so it waits for the target a few frames.
func chatsearchFocusAfterReturn(view chatui.ChatSearchView) {
	chatsearchOpened.Lock()
	origin := chatsearchOpened.origin
	chatsearchOpened.Unlock()
	selector, scroll := chatsearchReturnPlan(origin, view)
	chatsearchFocusSelector(selector, scroll, false)
}

// chatsearchRepairLostFocus gives focus to the summary when a search update
// replaced the control that held it (a filter chip, Search only in, Try again, a
// recent search) and focus fell to the page (#3857, #3896). Focus that is
// anywhere else, such as the search box a person is typing in, is left alone.
func chatsearchRepairLostFocus() {
	if !chatsearchFocusIsLost() {
		return
	}
	chatsearchFocusSelector("#"+chatui.Issue3857SummaryID, 0, true)
}

// chatsearchFocusAttempts is how many 40 ms ticks a redraw may take (about 4 s,
// a loaded machine or a long list) before the focus repair gives up.
const chatsearchFocusAttempts = 100

func chatsearchRefocusIfLost(selector string, delayMS int) {
	var again js.Func
	again = js.FuncOf(func(js.Value, []js.Value) any {
		defer again.Release()
		if !chatsearchFocusIsLost() {
			return nil
		}
		if target := js.Global().Get("document").Call("querySelector", selector); target.Truthy() {
			target.Call("focus", map[string]any{"preventScroll": true})
		}
		return nil
	})
	js.Global().Call("setTimeout", again, delayMS)
}

func chatsearchFocusIsLost() bool {
	doc := js.Global().Get("document")
	active := doc.Get("activeElement")
	return !active.Truthy() || active.Equal(doc.Get("body")) || active.Equal(doc.Get("documentElement"))
}

// chatsearchFocusSelector focuses the first element matching selector once it is
// drawn. onlyIfLost stops it taking focus a person has since moved elsewhere.
func chatsearchFocusSelector(selector string, scroll float64, onlyIfLost bool) {
	attempts := 0
	var tick js.Func
	tick = js.FuncOf(func(js.Value, []js.Value) any {
		attempts++
		doc := js.Global().Get("document")
		if onlyIfLost && !chatsearchFocusIsLost() {
			tick.Release()
			return nil
		}
		target := doc.Call("querySelector", selector)
		if !target.Truthy() {
			if attempts < chatsearchFocusAttempts {
				js.Global().Call("setTimeout", tick, 40)
				return nil
			}
			tick.Release()
			return nil
		}
		if scroll > 0 {
			if list := doc.Call("querySelector", ".chat-search-results"); list.Truthy() {
				list.Set("scrollTop", scroll)
			}
		}
		// The saved offset already shows the result; focusing must not move it.
		target.Call("focus", map[string]any{"preventScroll": scroll > 0})
		tick.Release()
		// A slow redraw can replace the target right after it took focus; look
		// again shortly and put focus back only if it fell to the page.
		for _, delay := range []int{250, 900} {
			chatsearchRefocusIfLost(selector, delay)
		}
		return nil
	})
	js.Global().Call("setTimeout", tick, 0)
}
