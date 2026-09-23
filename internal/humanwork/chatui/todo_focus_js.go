//go:build js && wasm

package chatui

import "syscall/js"

var channelPollFocusGeneration uint64

// FocusChannelTodo moves keyboard users from the pinned shortcut into the
// checklist after the details pane has rendered.
func FocusChannelTodo() {
	var attempt func(int)
	attempt = func(remaining int) {
		var frame js.Func
		frame = js.FuncOf(func(js.Value, []js.Value) any {
			frame.Release()
			defer func() { _ = recover() }()
			section := js.Global().Get("document").Call("getElementById", "chat-todo-section")
			if !section.Truthy() {
				if remaining > 0 {
					attempt(remaining - 1)
				}
				return nil
			}
			section.Call("scrollIntoView", js.ValueOf(map[string]any{"block": "start"}))
			section.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
			return nil
		})
		js.Global().Call("requestAnimationFrame", frame)
	}
	attempt(30)
}

func scheduleChannelPollFocus(generation uint64, conversationID string, remaining, stableFrames int, paneSeen bool) {
	if generation != channelPollFocusGeneration || remaining <= 0 {
		return
	}
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		frame.Release()
		defer func() { _ = recover() }()
		if generation != channelPollFocusGeneration {
			return nil
		}
		reschedule := func(stable int) {
			scheduleChannelPollFocus(generation, conversationID, remaining-1, stable, paneSeen)
		}
		doc := js.Global().Get("document")
		selected := doc.Call("querySelector", ".chat-row.selected")
		if !selected.Truthy() || selected.Call("getAttribute", "data-id").String() != conversationID {
			return nil
		}
		pane := doc.Call("querySelector", `.chat-side.chat-details[data-pane="details"]`)
		if !pane.Truthy() {
			if paneSeen {
				return nil // The reader closed the pane after it appeared.
			}
			reschedule(0)
			return nil
		}
		paneSeen = true
		if pane.Call("getAttribute", "aria-hidden").String() == "true" {
			return nil
		}
		section := doc.Call("getElementById", "chat-poll-section")
		if !section.Truthy() || !section.Get("isConnected").Bool() || section.Call("getAttribute", "data-loading").String() == "true" {
			reschedule(0)
			return nil
		}
		active := doc.Get("activeElement")
		trigger := doc.Call("querySelector", ".channel-poll-trigger")
		if active.Truthy() && !active.Equal(doc.Get("body")) && !active.Equal(section) && !active.Equal(trigger) {
			return nil // The reader moved focus while this bounded retry was pending.
		}
		paneRect := pane.Call("getBoundingClientRect")
		sectionRect := section.Call("getBoundingClientRect")
		sectionTop, paneTop := sectionRect.Get("top").Float(), paneRect.Get("top").Float()
		visible := sectionTop >= paneTop && sectionTop < paneRect.Get("bottom").Float()
		if !active.Equal(section) || !visible || stableFrames < 2 {
			// The aside is the scroll owner. Adjust it directly because the
			// browser can choose the outer page for scrollIntoView at narrow widths.
			pane.Set("scrollTop", pane.Get("scrollTop").Float()+sectionTop-paneTop)
			section.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
		}
		active = doc.Get("activeElement")
		if active.Equal(section) && visible && stableFrames >= 2 {
			return nil
		}
		nextStable := 0
		if active.Equal(section) {
			nextStable = stableFrames + 1
		}
		reschedule(nextStable)
		return nil
	})
	js.Global().Call("requestAnimationFrame", frame)
}

// FocusChannelPoll retries for at most two seconds while the details and poll
// finish rendering. The channel ID comes from the rendered shortcut, fencing
// the focus work to the room the reader clicked.
func FocusChannelPoll(conversationID string) {
	channelPollFocusGeneration++
	scheduleChannelPollFocus(channelPollFocusGeneration, conversationID, 120, 0, false)
}
