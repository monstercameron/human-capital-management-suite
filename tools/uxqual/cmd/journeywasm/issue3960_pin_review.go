package main

import (
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Issue 3960: the page's half of Review pinned guidance. The review itself is a
// comparison of messages the page already holds, so its state (which pin is
// open, which later messages were chosen, which pins the reader kept) lives
// here only: nothing is sent to the server and nothing outlives the page, which
// is what keeps it private to the reader. State is per conversation.

type pinReviewBrowser struct {
	mu           sync.Mutex
	refresh      func()
	focus        func(selector string) // moves focus after the next draw; browser only
	conversation string
	pinID        string
	selected     map[string]bool
	kept         map[string]bool // conversation \x00 pin
}

var pinReviewActive = &pinReviewBrowser{refresh: func() {}, focus: func(string) {}}

func pinReviewKey(conversation, pin string) string { return conversation + "\x00" + pin }

var pinReviewQuote = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// pinReviewSelector names one control of a pin review by its action and id, with
// the id quoted so it cannot widen the selector.
func pinReviewSelector(action, id string) string {
	return `[data-action="` + action + `"][data-id="` + pinReviewQuote.Replace(id) + `"]`
}

func pinReviewPanelSelector(pin string) string {
	return `[id="` + pinReviewQuote.Replace("pin-review-"+pin) + `"]`
}

// project gives the page's model the review state and its controls.
func (b *pinReviewBrowser) project(model chatui.Model) chatui.Model {
	b.mu.Lock()
	defer b.mu.Unlock()
	if model.SelectedID == "" {
		return model
	}
	conversation := model.SelectedID
	// A review belongs to one conversation and to a pin that is still pinned.
	if b.conversation != conversation {
		b.conversation, b.pinID, b.selected = conversation, "", nil
	}
	if b.pinID != "" {
		found := false
		for _, pin := range model.ChannelPins {
			if pin.PostID == b.pinID {
				found = true
			}
		}
		if !found {
			b.pinID, b.selected = "", nil
		}
	}
	state := chatui.PinReviewState{PinID: b.pinID, Kept: map[string]bool{}}
	if len(b.selected) > 0 {
		state.Selected = make(map[string]bool, len(b.selected))
		for id := range b.selected {
			state.Selected[id] = true
		}
	}
	for _, pin := range model.ChannelPins {
		if b.kept[pinReviewKey(conversation, pin.PostID)] {
			state.Kept[pin.PostID] = true
		}
	}
	state.Open = func(pin string) { b.open(conversation, pin) }
	state.Close = func() { b.close(conversation) }
	state.Toggle = func(source string) { b.toggle(conversation, source) }
	state.Keep = func(pin string) { b.keep(conversation, pin) }
	model.PinReview = state
	return model
}

func (b *pinReviewBrowser) open(conversation, pin string) {
	b.mu.Lock()
	if pin == "" || b.conversation != conversation {
		b.mu.Unlock()
		return
	}
	if b.pinID != pin {
		b.selected = nil
	}
	b.pinID = pin
	b.mu.Unlock()
	b.refresh()
	// Focus moves into the review, so a keyboard reader lands on what opened.
	b.moveFocus(pinReviewPanelSelector(pin))
}

func (b *pinReviewBrowser) close(conversation string) {
	b.mu.Lock()
	pin := ""
	if b.conversation == conversation {
		pin = b.pinID
		b.pinID, b.selected = "", nil
	}
	b.mu.Unlock()
	b.refresh()
	// The panel is gone: focus returns to the button that opened it.
	if pin != "" {
		b.moveFocus(pinReviewSelector("pinreview-open", pin))
	}
}

func (b *pinReviewBrowser) toggle(conversation, source string) {
	b.mu.Lock()
	if b.conversation != conversation || b.pinID == "" || source == "" {
		b.mu.Unlock()
		return
	}
	if b.selected[source] {
		delete(b.selected, source)
	} else {
		if b.selected == nil {
			b.selected = map[string]bool{}
		}
		b.selected[source] = true
	}
	b.mu.Unlock()
	b.refresh()
	b.moveFocus(pinReviewSelector("pinreview-toggle", source))
}

func (b *pinReviewBrowser) keep(conversation, pin string) {
	b.mu.Lock()
	done := false
	if b.conversation == conversation && pin != "" {
		done = true
		if b.kept == nil {
			b.kept = map[string]bool{}
		}
		b.kept[pinReviewKey(conversation, pin)] = true
		b.pinID, b.selected = "", nil
	}
	b.mu.Unlock()
	b.refresh()
	if done {
		b.moveFocus(pinReviewSelector("pinreview-open", pin))
	}
}

// pinReviewProject is the page hook: it offers the review for the open
// conversation's pins.
func pinReviewProject(model chatui.Model) chatui.Model { return pinReviewActive.project(model) }

// moveFocus asks the page to focus a control after the next draw; it does
// nothing outside the browser.
func (b *pinReviewBrowser) moveFocus(selector string) {
	if b.focus != nil {
		b.focus(selector)
	}
}
