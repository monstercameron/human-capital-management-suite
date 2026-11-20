//go:build js && wasm

package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3812: the browser half of a conversation's governed case. The page asks
// the server about the open conversation (is it bound to a case, is it
// restricted, who owns it, may this person change the binding), keeps the last
// word per conversation, and sends the bind form and the lift button. The server
// decides everything; a conversation the server did not describe shows nothing.
// The answer is asked for again after a while, so a binding made elsewhere shows
// without a reload.

const caseStale = 30 * time.Second

type caseBrowser struct {
	mu       sync.Mutex
	cfg      journeyclient.Config
	disposed bool
	states   map[string]chatui.CaseState
	askedAt  map[string]time.Time
	loading  map[string]bool
	busy     map[string]bool
	notice   map[string]string
	submit   js.Func
}

var caseActive *caseBrowser

func installConversationCase(cfg journeyclient.Config) func() {
	cfg = personaChatHTTPConfig(cfg)
	b := &caseBrowser{cfg: cfg, states: map[string]chatui.CaseState{}, askedAt: map[string]time.Time{}, loading: map[string]bool{}, busy: map[string]bool{}, notice: map[string]string{}}
	caseActive = b
	doc := js.Global().Get("document")
	b.submit = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && !b.disposed {
			b.onSubmit(args[0])
		}
		return nil
	})
	doc.Call("addEventListener", "submit", b.submit)
	return func() {
		b.disposed = true
		doc.Call("removeEventListener", "submit", b.submit)
		b.submit.Release()
		if caseActive == b {
			caseActive = nil
		}
	}
}

// caseProject gives the page's model what is known of the open conversation's
// case, and starts the read when it is not known or has gone stale.
func caseProject(model chatui.Model, _ journeyclient.Config) chatui.Model {
	b := caseActive
	if b == nil || b.disposed || model.SelectedID == "" {
		return model
	}
	conversation := model.SelectedID
	b.mu.Lock()
	state, known := b.states[conversation]
	asked, askedBefore := b.askedAt[conversation]
	load := !b.loading[conversation] && !b.busy[conversation] && (!askedBefore || time.Since(asked) > caseStale)
	if load {
		b.loading[conversation] = true
	}
	if known {
		state.Busy, state.Notice = b.busy[conversation], b.notice[conversation]
		state.Bind, state.Unbind = b.bindFor, b.unbindFor
		model.Case = state
	}
	b.mu.Unlock()
	if load {
		go b.load(conversation)
	}
	return model
}

func (b *caseBrowser) load(conversation string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	state, err := caseRead(ctx, http.DefaultClient, b.cfg, conversation)
	ui.PostAsync(func() {
		b.mu.Lock()
		delete(b.loading, conversation)
		b.askedAt[conversation] = time.Now()
		switch {
		case err == nil:
			b.states[conversation] = state
		default:
			// An unreadable answer is not "not bound": show nothing it did not
			// say. The server refuses whatever the page offers.
			delete(b.states, conversation)
		}
		b.mu.Unlock()
		refreshChatRoute()
	})
}

func (b *caseBrowser) onSubmit(event js.Value) {
	form := event.Get("target")
	if !form.Truthy() || form.Get("hasAttribute").Type() != js.TypeFunction || !form.Call("hasAttribute", "data-case-form").Bool() {
		return
	}
	event.Call("preventDefault")
	field := func(name string) js.Value { return form.Call("querySelector", "[name="+name+"]") }
	ref, owner, restricted := field("case_ref"), field("owner"), field("restricted")
	if !ref.Truthy() || !owner.Truthy() || !restricted.Truthy() {
		return
	}
	b.bindFor(strings.TrimSpace(ref.Get("value").String()), owner.Get("value").String(), restricted.Get("checked").Bool())
}

// bindFor sends the bind form for the open conversation.
func (b *caseBrowser) bindFor(caseRef, owner string, restricted bool) {
	conversation := chatBrowser.snapshot().SelectedID
	if conversation == "" || b.disposed {
		return
	}
	b.mu.Lock()
	if b.busy[conversation] {
		b.mu.Unlock()
		return
	}
	if caseRef == "" || owner == "" {
		b.notice[conversation] = "invalid"
		b.mu.Unlock()
		refreshChatRoute()
		return
	}
	b.busy[conversation], b.notice[conversation] = true, ""
	b.mu.Unlock()
	refreshChatRoute()
	go b.send(conversation, func(ctx context.Context) (chatui.CaseState, error) {
		return caseBind(ctx, http.DefaultClient, b.cfg, conversation, caseRef, owner, restricted)
	}, chatui.Issue3812DoneKey, "done")
}

// unbindFor lifts the binding of the open conversation.
func (b *caseBrowser) unbindFor() {
	conversation := chatBrowser.snapshot().SelectedID
	if conversation == "" || b.disposed {
		return
	}
	b.mu.Lock()
	if b.busy[conversation] {
		b.mu.Unlock()
		return
	}
	b.busy[conversation], b.notice[conversation] = true, ""
	b.mu.Unlock()
	refreshChatRoute()
	go b.send(conversation, func(ctx context.Context) (chatui.CaseState, error) {
		return caseLift(ctx, http.DefaultClient, b.cfg, conversation)
	}, chatui.Issue3812LiftedKey, "lifted")
}

func (b *caseBrowser) send(conversation string, call func(context.Context) (chatui.CaseState, error), doneKey, doneNotice string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	state, err := call(ctx)
	ui.PostAsync(func() {
		b.mu.Lock()
		delete(b.busy, conversation)
		var refused *caseError
		switch {
		case err == nil:
			b.states[conversation] = state
			b.askedAt[conversation] = time.Now()
			b.notice[conversation] = doneNotice
		case errors.As(err, &refused) && refused.Code == "permission_denied":
			b.notice[conversation] = "denied"
		case errors.As(err, &refused) && (refused.Code == "invalid_request"):
			b.notice[conversation] = "invalid"
		case errors.As(err, &refused) && refused.Code == "not_found":
			// The conversation is gone, or the person can no longer open it.
			delete(b.states, conversation)
			b.notice[conversation] = "failed"
		default:
			b.notice[conversation] = "failed"
		}
		b.mu.Unlock()
		if err == nil {
			b.announce(chatui.Issue3812Text(b.cfg.Locale, doneKey))
		}
		refreshChatRoute()
	})
}

// announce tells a screen reader that the binding changed.
func (b *caseBrowser) announce(text string) {
	doc := js.Global().Get("document")
	live := doc.Call("querySelector", "[data-chatcase-live]")
	if !live.Truthy() {
		live = doc.Call("createElement", "div")
		live.Call("setAttribute", "data-chatcase-live", "true")
		live.Call("setAttribute", "role", "status")
		live.Call("setAttribute", "aria-live", "polite")
		live.Call("setAttribute", "style", "position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap")
		doc.Get("body").Call("appendChild", live)
	}
	live.Set("textContent", text)
}
