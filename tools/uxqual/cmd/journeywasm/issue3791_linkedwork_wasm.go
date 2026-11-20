//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3791: the browser half of "Work from this conversation". It reads the
// work of the open conversation each time Conversation details is opened (and
// again when the person presses Refresh, or a minute has passed while it stays
// open), so access that changed since the last look is never shown from memory.
// A conversation the server will not answer for hides the section; a read that
// failed shows an unknown state with Try again, never an empty list.

const linkedworkStale = time.Minute

type linkedworkBrowser struct {
	mu       sync.Mutex
	cfg      journeyclient.Config
	disposed bool
	// state is the last read for conv; hidden conversations are not offered.
	state    chatui.LinkedWorkState
	conv     string
	loadedAt time.Time
	loading  string
	hidden   map[string]bool
	failedAt time.Time
	wasShown bool
	refocus  bool
	click    js.Func
}

var linkedworkActive *linkedworkBrowser

func installLinkedWork(cfg journeyclient.Config) func() {
	cfg = personaChatHTTPConfig(cfg)
	b := &linkedworkBrowser{cfg: cfg, hidden: map[string]bool{}}
	linkedworkActive = b
	doc := js.Global().Get("document")
	b.click = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && !b.disposed {
			b.onClick(args[0])
		}
		return nil
	})
	doc.Call("addEventListener", "click", b.click)
	return func() {
		b.disposed = true
		doc.Call("removeEventListener", "click", b.click)
		b.click.Release()
		if linkedworkActive == b {
			linkedworkActive = nil
		}
	}
}

// linkedworkProject gives the page's model the work panel for the open
// conversation. The read starts here when the details pane has just opened, the
// conversation changed, or the last read is a minute old.
func linkedworkProject(model chatui.Model, _ journeyclient.Config) chatui.Model {
	b := linkedworkActive
	if b == nil || b.disposed {
		return model
	}
	b.mu.Lock()
	selected := model.SelectedID
	opened := model.ShowDetails && !b.wasShown
	b.wasShown = model.ShowDetails
	same := b.conv == selected
	state := chatui.LinkedWorkState{}
	if selected != "" && !b.hidden[selected] && same {
		state = b.state
		state.Enabled = true
	} else if selected != "" && !b.hidden[selected] {
		state = chatui.LinkedWorkState{Enabled: true, State: "loading"}
	}
	want := model.ShowDetails && selected != "" && !b.hidden[selected] && b.loading != selected &&
		(!same || opened || time.Since(b.loadedAt) > linkedworkStale) &&
		(b.state.State != "error" || !same || time.Since(b.failedAt) > 10*time.Second || opened)
	if want {
		b.loading = selected
		if !same {
			b.state = chatui.LinkedWorkState{Enabled: true, State: "loading"}
		}
	}
	b.mu.Unlock()
	model.LinkedWork = state
	if want {
		go b.load(selected, false)
	}
	return model
}

func (b *linkedworkBrowser) load(conversation string, refocus bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	state, err := linkedworkRead(ctx, http.DefaultClient, b.cfg, conversation)
	ui.PostAsync(func() {
		b.mu.Lock()
		if b.loading == conversation {
			b.loading = ""
		}
		switch {
		case err == nil:
			b.state, b.conv, b.loadedAt = state, conversation, time.Now()
			delete(b.hidden, conversation)
		case linkedworkHides(err):
			b.hidden[conversation] = true
			b.state, b.conv = chatui.LinkedWorkState{}, conversation
		default:
			// Keep nothing the person could mistake for current: the panel says
			// the work is unknown and offers Try again.
			b.state, b.conv = chatui.LinkedWorkState{Enabled: true, State: "error", Error: chatworkErrorCode(err)}, conversation
			b.failedAt, b.loadedAt = time.Now(), time.Now()
		}
		b.refocus = b.refocus || refocus
		b.mu.Unlock()
		refreshChatRoute()
		b.restoreFocus()
	})
}

// restoreFocus returns focus to Refresh after a pressed Refresh redraws the pane.
func (b *linkedworkBrowser) restoreFocus() {
	b.mu.Lock()
	want := b.refocus
	b.refocus = false
	b.mu.Unlock()
	if !want {
		return
	}
	ui.PostAsync(func() {
		if button := js.Global().Get("document").Call("querySelector", "[data-linked-work-refresh]"); button.Truthy() {
			button.Call("focus")
		}
	})
}

func (b *linkedworkBrowser) onClick(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	if button := target.Call("closest", "[data-linked-work-refresh]"); button.Truthy() {
		event.Call("preventDefault")
		model := chatBrowser.snapshot()
		if model.SelectedID == "" {
			return
		}
		b.mu.Lock()
		if b.loading == model.SelectedID {
			b.mu.Unlock()
			return
		}
		b.loading = model.SelectedID
		b.state = chatui.LinkedWorkState{Enabled: true, State: "loading"}
		b.conv = model.SelectedID
		b.mu.Unlock()
		refreshChatRoute()
		go b.load(model.SelectedID, true)
	}
}
