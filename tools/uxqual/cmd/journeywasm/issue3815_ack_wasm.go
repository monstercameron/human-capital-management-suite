//go:build js && wasm

package main

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3815: the browser half of acknowledgements. The page asks the server
// about the messages of the open conversation (does one require acknowledgement,
// may this reader acknowledge it, who has not), keeps the last word per message,
// sends the Acknowledge form when it is submitted and the Require request from
// the message menu. The server decides everything; a message the server did not
// describe shows nothing. A required message that is still open is asked about
// again, so an acknowledgement given elsewhere shows without a reload.

const (
	ackStaleOpen  = 20 * time.Second
	ackStalePlain = 2 * time.Minute
	ackBatchSize  = 50
)

type ackBrowser struct {
	mu         sync.Mutex
	cfg        journeyclient.Config
	disposed   bool
	views      map[string]chatui.AckView // required messages only
	askedAt    map[string]time.Time
	loading    map[string]bool
	canRequire map[string]bool
	noticePost string
	notice     string
	submit     js.Func
}

var ackActive *ackBrowser

func ackKey(conversation, post string) string { return conversation + "\x00" + post }

func installPostAcks(cfg journeyclient.Config) func() {
	cfg = personaChatHTTPConfig(cfg)
	b := &ackBrowser{cfg: cfg, views: map[string]chatui.AckView{}, askedAt: map[string]time.Time{}, loading: map[string]bool{}, canRequire: map[string]bool{}}
	ackActive = b
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
		if ackActive == b {
			ackActive = nil
		}
	}
}

// ackProject gives the page's model what is known of the acknowledgements on
// screen, and starts the read for the messages not yet known or gone stale.
func ackProject(model chatui.Model, _ journeyclient.Config) chatui.Model {
	b := ackActive
	if b == nil || b.disposed || model.SelectedID == "" {
		return model
	}
	conversation := model.SelectedID
	var want []string
	b.mu.Lock()
	state := chatui.AcksState{Views: map[string]chatui.AckView{}, CanRequire: b.canRequire[conversation], NoticePost: b.noticePost, Notice: b.notice}
	if state.CanRequire {
		state.Require = b.requireFor
	}
	seen := map[string]bool{}
	for _, list := range [][]chatui.Message{model.Messages, model.ThreadMessages} {
		for i := len(list) - 1; i >= 0; i-- {
			msg := list[i]
			if seen[msg.ID] || msg.ID == "" {
				continue
			}
			seen[msg.ID] = true
			key := ackKey(conversation, msg.ID)
			view, required := b.views[key]
			if required {
				state.Views[msg.ID] = view
			}
			stale := ackStalePlain
			if required {
				stale = ackStaleOpen
			}
			// A message the reader acknowledged, and cannot manage, is final.
			final := required && view.Acknowledged && !view.CanManage
			asked, known := b.askedAt[key]
			if !final && !b.loading[key] && !view.Busy && (!known || time.Since(asked) > stale) && len(want) < ackBatchSize {
				want = append(want, msg.ID)
				b.loading[key] = true
			}
		}
	}
	model.Acks = state
	b.mu.Unlock()
	if len(want) > 0 {
		sort.Strings(want)
		go b.load(conversation, want)
	}
	return model
}

func (b *ackBrowser) load(conversation string, posts []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	views, canRequire, err := ackRead(ctx, http.DefaultClient, b.cfg, conversation, posts)
	ui.PostAsync(func() {
		b.mu.Lock()
		for _, id := range posts {
			key := ackKey(conversation, id)
			delete(b.loading, key)
			// An unreadable answer is not "nothing is required": keep what was
			// known and ask again once the stale time has passed.
			if err != nil {
				b.askedAt[key] = time.Now()
				continue
			}
			b.askedAt[key] = time.Now()
			if view, ok := views[id]; ok {
				if old, has := b.views[key]; !has || !old.Busy {
					b.views[key] = view
				}
			} else {
				delete(b.views, key)
			}
		}
		if err == nil {
			b.canRequire[conversation] = canRequire
		}
		b.mu.Unlock()
		refreshChatRoute()
	})
}

func (b *ackBrowser) onSubmit(event js.Value) {
	form := event.Get("target")
	if !form.Truthy() || form.Get("hasAttribute").Type() != js.TypeFunction || !form.Call("hasAttribute", "data-ack-form").Bool() {
		return
	}
	event.Call("preventDefault")
	post := form.Call("getAttribute", "data-ack-form").String()
	conversation := chatBrowser.snapshot().SelectedID
	if post == "" || conversation == "" {
		return
	}
	key := ackKey(conversation, post)
	b.mu.Lock()
	view, required := b.views[key]
	if !required || view.Busy || !view.CanAcknowledge {
		b.mu.Unlock()
		return
	}
	view.Busy, view.Notice = true, ""
	b.views[key] = view
	b.mu.Unlock()
	refreshChatRoute()
	go b.acknowledge(conversation, post)
}

func (b *ackBrowser) acknowledge(conversation, post string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done, err := ackAcknowledge(ctx, http.DefaultClient, b.cfg, conversation, post)
	ui.PostAsync(func() {
		key := ackKey(conversation, post)
		b.mu.Lock()
		view := b.views[key]
		view.Busy = false
		focus := ""
		var refused *ackError
		switch {
		case err == nil:
			view = done
			focus = "done"
		case errors.As(err, &refused) && refused.Code == "permission_denied":
			view.CanAcknowledge, view.Notice = false, "denied"
		case errors.As(err, &refused) && refused.Code == "not_found":
			// The message is gone or no longer asks for acknowledgement.
			delete(b.views, key)
			delete(b.askedAt, key)
			view = chatui.AckView{}
		default:
			view.Notice = "failed"
		}
		if view.Loaded {
			b.views[key] = view
		}
		b.mu.Unlock()
		refreshChatRoute()
		if focus == "done" {
			ui.PostAsync(func() {
				if el := js.Global().Get("document").Call("getElementById", "ack-done-"+post); el.Truthy() {
					el.Call("focus")
				}
			})
		}
	})
}

// requireFor marks one message of the reader's own as needing acknowledgement.
func (b *ackBrowser) requireFor(post string) {
	conversation := chatBrowser.snapshot().SelectedID
	if post == "" || conversation == "" || b.disposed {
		return
	}
	go b.require(conversation, post)
}

func (b *ackBrowser) require(conversation, post string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done, err := ackRequire(ctx, http.DefaultClient, b.cfg, conversation, post)
	ui.PostAsync(func() {
		key := ackKey(conversation, post)
		b.mu.Lock()
		var refused *ackError
		switch {
		case err == nil:
			b.views[key] = done
			b.askedAt[key] = time.Now()
			b.noticePost, b.notice = "", ""
		case errors.As(err, &refused) && refused.Code == "permission_denied":
			b.noticePost, b.notice = post, "denied"
		default:
			b.noticePost, b.notice = post, "failed"
		}
		b.mu.Unlock()
		if err == nil {
			b.announce(chatui.Issue3815Text(b.cfg.Locale, chatui.Issue3815RequireDoneKey))
		}
		refreshChatRoute()
	})
}

// announce tells a screen reader that acknowledgement is now required.
func (b *ackBrowser) announce(text string) {
	doc := js.Global().Get("document")
	live := doc.Call("querySelector", "[data-chatack-live]")
	if !live.Truthy() {
		live = doc.Call("createElement", "div")
		live.Call("setAttribute", "data-chatack-live", "true")
		live.Call("setAttribute", "role", "status")
		live.Call("setAttribute", "aria-live", "polite")
		live.Call("setAttribute", "style", "position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap")
		doc.Get("body").Call("appendChild", live)
	}
	live.Set("textContent", text)
}
