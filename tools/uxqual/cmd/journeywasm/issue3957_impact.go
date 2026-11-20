package main

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Issue 3957: the page half of Check downstream impact. The check is a few
// choices over the thread message the person picked, so it lives only here:
// nothing is stored and nothing outlives the thread pane, which keeps it
// private. It reads the one source this product can query for the person, the
// work linked to the conversation, through the same viewer-authorized read as
// the work panel; the server decides what this person may see. A draft is made
// only after the work is read again, so access and freshness are rechecked
// when something shareable is prepared, and it is handed to the reply box for
// the person to edit and send. Nothing is sent from here.

const impactReadTimeout = 15 * time.Second

var impactQuote = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

type impactBrowser struct {
	mu      sync.Mutex
	refresh func()
	focus   func(selector string)
	// spawn runs a read away from the caller; post returns to the page's thread.
	spawn func(func())
	post  func(func())
	// read is the viewer-authorized read of a conversation's work; nil means the
	// page cannot read it and the check is not offered.
	read func(ctx context.Context, conversation string) (chatui.LinkedWorkState, error)

	conversation, thread, postID string
	locale                       string
	date, population             string
	stage, errCode, notice       string
	items                        []chatui.LinkedWorkItem
	incomplete                   []string
	asOf                         time.Time
	dismissed                    map[string]bool
	gen                          int
}

var impactActive = &impactBrowser{
	refresh: func() {}, focus: func(string) {},
	spawn: func(fn func()) { fn() }, post: func(fn func()) { fn() },
}

func impactSelector(action, id string) string {
	return `[data-action="` + action + `"][data-id="` + impactQuote.Replace(id) + `"]`
}

// reset forgets the whole check. Callers hold the lock.
func (b *impactBrowser) reset() {
	b.postID, b.date, b.population, b.stage, b.errCode, b.notice = "", "", "", "", "", ""
	b.items, b.incomplete, b.asOf, b.dismissed = nil, nil, time.Time{}, nil
	b.gen++
}

// project gives the page model the check and its controls. It is offered only
// in an open thread, where there is a reply box to draft into.
func (b *impactBrowser) project(model chatui.Model) chatui.Model {
	b.mu.Lock()
	defer b.mu.Unlock()
	if model.SelectedID != b.conversation || model.ThreadParentID != b.thread {
		b.conversation, b.thread = model.SelectedID, model.ThreadParentID
		b.reset()
	}
	b.locale = model.Locale
	if model.SelectedID == "" || model.ThreadParentID == "" || b.read == nil {
		return model
	}
	conversation := model.SelectedID
	state := chatui.ImpactState{Enabled: true, PostID: b.postID, Date: b.date, Population: b.population, Stage: b.stage, Error: b.errCode, Notice: b.notice}
	if len(b.dismissed) > 0 {
		state.Dismissed = make(map[string]bool, len(b.dismissed))
		for id := range b.dismissed {
			state.Dismissed[id] = true
		}
	}
	if b.stage == "ready" {
		state.Result = chatui.ImpactAssess(b.items, b.incomplete, b.asOf, b.date)
	}
	state.Open = func(post, suggested string) { b.open(conversation, post, suggested) }
	state.Close = func() { b.close(conversation) }
	state.Run = func(post, date, population string) { b.run(conversation, post, date, population) }
	state.Dismiss = func(id string) { b.toggle(conversation, id, true) }
	state.Restore = func(id string) { b.toggle(conversation, id, false) }
	state.Draft = func(post, date, population string, deliver func(string)) {
		b.draft(conversation, post, date, population, deliver)
	}
	model.Impact = state
	return model
}

func (b *impactBrowser) open(conversation, post, suggested string) {
	b.mu.Lock()
	if post == "" || b.conversation != conversation {
		b.mu.Unlock()
		return
	}
	if b.postID != post {
		b.reset()
		b.date = suggested
	}
	b.postID = post
	b.mu.Unlock()
	b.refresh()
	b.focus(`[id="` + impactQuote.Replace("impact-"+post) + `"]`)
}

func (b *impactBrowser) close(conversation string) {
	b.mu.Lock()
	post := ""
	if b.conversation == conversation {
		post = b.postID
		b.reset()
	}
	b.mu.Unlock()
	b.refresh()
	if post != "" {
		// The check is gone; focus returns to the message's More button.
		b.focus(impactSelector("menu", "thread:"+post))
	}
}

func (b *impactBrowser) toggle(conversation, id string, dismiss bool) {
	b.mu.Lock()
	if b.conversation != conversation || b.postID == "" || id == "" {
		b.mu.Unlock()
		return
	}
	if dismiss {
		if b.dismissed == nil {
			b.dismissed = map[string]bool{}
		}
		b.dismissed[id] = true
	} else {
		delete(b.dismissed, id)
	}
	b.notice = ""
	b.mu.Unlock()
	b.refresh()
	// The pressed row moved to the other list; follow it.
	if dismiss {
		b.focus(impactSelector("impact-restore", id))
	} else {
		b.focus(impactSelector("impact-dismiss", id))
	}
}

// fetch reads the work again and applies it, if the check is still the one
// that asked. done is told whether the read succeeded.
func (b *impactBrowser) fetch(conversation, post, date, population string, done func(ok bool)) {
	b.mu.Lock()
	gen := b.gen
	b.date, b.population, b.stage, b.errCode, b.notice = date, population, "loading", "", ""
	read := b.read
	b.mu.Unlock()
	b.refresh()
	b.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), impactReadTimeout)
		defer cancel()
		state, err := read(ctx, conversation)
		b.post(func() {
			b.mu.Lock()
			if b.gen != gen || b.postID != post {
				b.mu.Unlock()
				return
			}
			ok := err == nil
			if ok {
				b.items, b.incomplete, b.asOf, b.stage = state.Items, state.Incomplete, state.AsOf, "ready"
			} else {
				// Nothing from an earlier read is kept: it could be mistaken for current.
				b.items, b.incomplete, b.asOf, b.stage, b.errCode = nil, nil, time.Time{}, "error", chatworkErrorCode(err)
			}
			b.mu.Unlock()
			done(ok)
			b.refresh()
		})
	})
}

func (b *impactBrowser) run(conversation, post, date, population string) {
	b.mu.Lock()
	if b.conversation != conversation || b.postID != post || post == "" || b.stage == "loading" {
		b.mu.Unlock()
		return
	}
	if !chatui.ImpactValidDate(date) {
		b.date, b.population, b.stage, b.errCode, b.notice = date, population, "", "invalid_date", ""
		b.mu.Unlock()
		b.refresh()
		b.focus(`[id="` + impactQuote.Replace("impact-date-"+post) + `"]`)
		return
	}
	b.mu.Unlock()
	b.fetch(conversation, post, date, population, func(bool) {
		b.focus(impactSelector("impact-run", post))
	})
}

func (b *impactBrowser) draft(conversation, post, date, population string, deliver func(string)) {
	b.mu.Lock()
	if b.conversation != conversation || b.postID != post || post == "" || b.stage == "loading" {
		b.mu.Unlock()
		return
	}
	if !chatui.ImpactValidDate(date) {
		b.stage, b.errCode = "", "invalid_date"
		b.mu.Unlock()
		b.refresh()
		return
	}
	b.mu.Unlock()
	b.fetch(conversation, post, date, population, func(ok bool) {
		b.mu.Lock()
		locale := b.locale
		kept := chatui.ImpactKept(chatui.ImpactAssess(b.items, b.incomplete, b.asOf, b.date), b.dismissed)
		switch {
		case !ok:
			b.notice = "draft_failed"
		case kept == 0:
			b.notice = "nothing_to_ask"
		default:
			b.notice = "draft_ready"
		}
		notice := b.notice
		b.mu.Unlock()
		if notice == "draft_ready" && deliver != nil {
			deliver(chatui.ImpactDraft(locale, date, population, kept))
			return
		}
		b.focus(impactSelector("impact-draft", post))
	})
}

// impactProject is the page hook.
func impactProject(model chatui.Model) chatui.Model { return impactActive.project(model) }
