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

// Issue 3795: the browser half of form cards. The page asks the server about
// the form cards of the open conversation (who may answer, whether it was
// answered), keeps the last word per card, and sends a form when it is
// submitted. A card is asked about again while it is still open, so an answer
// given elsewhere shows without a reload. The server decides everything; a card
// the client has not heard about yet says it is being checked rather than
// offering a form.

const formcardStale = 20 * time.Second

type formcardBrowser struct {
	mu       sync.Mutex
	cfg      journeyclient.Config
	disposed bool
	views    map[string]chatui.FormCardView
	loadedAt map[string]time.Time
	loading  map[string]bool
	submit   js.Func
}

var formcardActive *formcardBrowser

func formcardKey(conversation, post string) string { return conversation + "\x00" + post }

func installFormCards(cfg journeyclient.Config) func() {
	cfg = personaChatHTTPConfig(cfg)
	b := &formcardBrowser{cfg: cfg, views: map[string]chatui.FormCardView{}, loadedAt: map[string]time.Time{}, loading: map[string]bool{}}
	formcardActive = b
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
		if formcardActive == b {
			formcardActive = nil
		}
	}
}

// formcardProject gives the page's model what is known of the form cards on
// screen, and starts the read for the ones not yet known or gone stale.
func formcardProject(model chatui.Model, _ journeyclient.Config) chatui.Model {
	b := formcardActive
	if b == nil || b.disposed || model.SelectedID == "" {
		return model
	}
	conversation := model.SelectedID
	var want []string
	b.mu.Lock()
	model.Forms.Views = map[string]chatui.FormCardView{}
	seen := map[string]bool{}
	for _, list := range [][]chatui.Message{model.Messages, model.ThreadMessages} {
		for _, msg := range list {
			if seen[msg.ID] || msg.ID == "" || !chatui.IsFormCardBody(msg.Body) {
				continue
			}
			seen[msg.ID] = true
			key := formcardKey(conversation, msg.ID)
			view, known := b.views[key]
			if known {
				model.Forms.Views[msg.ID] = view
			}
			// A card never heard about, or still open, is asked about again once
			// its last read is stale (a missing read counts as stale); an answered
			// card is final.
			if !b.loading[key] && !view.Busy && (!known || !view.Submitted) && time.Since(b.loadedAt[key]) > formcardStale && len(want) < 50 {
				want = append(want, msg.ID)
				b.loading[key] = true
			}
		}
	}
	b.mu.Unlock()
	if len(want) > 0 {
		sort.Strings(want)
		go b.load(conversation, want)
	}
	return model
}

func (b *formcardBrowser) load(conversation string, posts []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	views, err := formcardRead(ctx, http.DefaultClient, b.cfg, conversation, posts)
	ui.PostAsync(func() {
		b.mu.Lock()
		for _, id := range posts {
			key := formcardKey(conversation, id)
			delete(b.loading, key)
			b.loadedAt[key] = time.Now()
			// An unreadable answer is not an empty form: keep what was known, and
			// leave a card never heard about showing "checking" until the next
			// read, which starts once the stale time has passed.
			if view, ok := views[id]; err == nil && ok {
				if old, has := b.views[key]; !has || !old.Busy {
					b.views[key] = view
				}
			}
		}
		b.mu.Unlock()
		refreshChatRoute()
	})
}

func (b *formcardBrowser) onSubmit(event js.Value) {
	form := event.Get("target")
	if !form.Truthy() || form.Get("hasAttribute").Type() != js.TypeFunction || !form.Call("hasAttribute", "data-form-card").Bool() {
		return
	}
	event.Call("preventDefault")
	post := form.Call("getAttribute", "data-form-card").String()
	conversation := chatBrowser.snapshot().SelectedID
	if post == "" || conversation == "" {
		return
	}
	key := formcardKey(conversation, post)
	b.mu.Lock()
	view := b.views[key]
	if view.Busy || !view.CanSubmit {
		b.mu.Unlock()
		return
	}
	values := map[string]string{}
	fields := form.Call("querySelectorAll", "[name]")
	for i := 0; i < fields.Length(); i++ {
		field := fields.Index(i)
		if name := field.Get("name").String(); name != "" {
			values[name] = field.Get("value").String()
		}
	}
	view.Busy, view.Errors, view.Notice = true, nil, ""
	b.views[key] = view
	b.mu.Unlock()
	refreshChatRoute()
	go b.send(conversation, post, values)
}

func (b *formcardBrowser) send(conversation, post string, values map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done, err := formcardSubmit(ctx, http.DefaultClient, b.cfg, conversation, post, values)
	ui.PostAsync(func() {
		key := formcardKey(conversation, post)
		b.mu.Lock()
		view := b.views[key]
		view.Busy = false
		focus := ""
		var refused *formcardError
		switch {
		case err == nil:
			view = done
		case errors.As(err, &refused) && refused.Code == "invalid_form":
			view.Errors, view.Notice = refused.Fields, "invalid"
			focus = "invalid"
		case errors.As(err, &refused) && refused.Code == "already_answered":
			// Answered elsewhere: ask again so the card shows the truth.
			view.Notice = "conflict"
			delete(b.loadedAt, key)
		case errors.As(err, &refused) && refused.Code == "permission_denied":
			view.CanSubmit, view.Notice = false, "denied"
		default:
			view.Notice = "failed"
			focus = "notice"
		}
		b.views[key] = view
		b.mu.Unlock()
		refreshChatRoute()
		if focus == "invalid" {
			ui.PostAsync(func() {
				if field := js.Global().Get("document").Call("querySelector", "[data-form-card='"+post+"'] [aria-invalid='true']"); field.Truthy() {
					field.Call("focus")
				}
			})
		}
	})
}
