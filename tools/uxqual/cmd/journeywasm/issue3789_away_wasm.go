//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3789: the away setting's browser side. A change goes to the server
// first; the row then shows what the server holds.

var chatAwayBrowser struct {
	sync.Mutex
	write    sync.Mutex
	revision uint64
	serial   uint64
	looked   uint64
}

func withIssue3789Callbacks(callbacks chatui.Callbacks, cfg journeyclient.Config) chatui.Callbacks {
	callbacks.SaveAway = func(edit chatui.AwayEdit) { go saveChatAway(cfg, edit) }
	return callbacks
}

// loadChatAway reads the person's own setting. A refresh that fails after a good
// load keeps what is on screen; only a first load that fails says unavailable.
func loadChatAway(cfg journeyclient.Config) {
	chatAwayBrowser.Lock()
	chatAwayBrowser.serial++
	serial := chatAwayBrowser.serial
	chatAwayBrowser.Unlock()
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	view, err := chatAwayRead(ctx, http.DefaultClient, active, time.Now())
	chatAwayBrowser.Lock()
	stale := serial != chatAwayBrowser.serial
	if err == nil && !stale {
		chatAwayBrowser.revision = view.Revision
	}
	chatAwayBrowser.Unlock()
	if stale {
		return
	}
	chatBrowser.mutate(func(m *chatui.Model) {
		if err != nil {
			m.Away.Failed = !m.Away.Loaded
			return
		}
		view.Note = m.Away.Note
		m.Away = view
	})
	refreshChatRoute()
}

// loadChatAwayPeople looks up the absences colleagues share: the people in view,
// plus any named. The answer replaces what the page held for those people, so an
// absence that ended or was hidden disappears.
func loadChatAwayPeople(cfg journeyclient.Config, extra ...string) {
	chatAwayBrowser.Lock()
	chatAwayBrowser.looked++
	serial := chatAwayBrowser.looked
	chatAwayBrowser.Unlock()
	model := chatBrowser.snapshot()
	ids := chatAwayIDs(model, extra...)
	if len(ids) == 0 {
		return
	}
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	people, err := chatAwayLookup(ctx, http.DefaultClient, active, ids, time.Now())
	if err != nil {
		return
	}
	chatAwayBrowser.Lock()
	stale := serial != chatAwayBrowser.looked
	chatAwayBrowser.Unlock()
	if stale {
		return
	}
	chatBrowser.mutate(func(m *chatui.Model) {
		next := map[string]chatui.AwayPersonView{}
		for id, p := range m.AwayPeople {
			next[id] = p
		}
		for _, id := range ids {
			delete(next, id)
		}
		for id, p := range people {
			next[id] = p
		}
		m.AwayPeople = next
	})
	refreshChatRoute()
}

// saveChatAway keeps one change. One save runs at a time, each with the
// revision the last answer gave; a conflict reads the setting again and says
// so. A date the page could not use is said without asking the server.
func saveChatAway(cfg journeyclient.Config, edit chatui.AwayEdit) {
	if edit.Problem != "" {
		chatBrowser.mutate(func(m *chatui.Model) { m.Away.Note = edit.Problem })
		refreshChatRoute()
		return
	}
	chatAwayBrowser.write.Lock()
	defer chatAwayBrowser.write.Unlock()
	chatAwayBrowser.Lock()
	revision := chatAwayBrowser.revision
	chatAwayBrowser.Unlock()
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	view, outcome := chatAwayWrite(ctx, http.DefaultClient, active, edit, revision, time.Now())
	if outcome == "saved" {
		chatAwayBrowser.Lock()
		chatAwayBrowser.serial++
		chatAwayBrowser.revision = view.Revision
		chatAwayBrowser.Unlock()
	}
	chatBrowser.mutate(func(m *chatui.Model) {
		if outcome == "saved" {
			view.Note = "saved"
			if edit.Until.IsZero() && m.Away.Active {
				view.Note = "ended"
			}
			m.Away = view
			return
		}
		m.Away.Note = outcome
	})
	refreshChatRoute()
	if outcome == "conflict" || outcome == "failed" {
		loadChatAway(cfg)
	}
}
