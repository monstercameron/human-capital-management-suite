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

// Issue 3920: the weekly quiet-hours schedule's browser side. A change goes to
// the server first; the row then shows what the server holds.

var chatQuietScheduleBrowser struct {
	sync.Mutex
	write    sync.Mutex
	revision uint64
	serial   uint64
}

func withIssue3920Callbacks(callbacks chatui.Callbacks, cfg journeyclient.Config) chatui.Callbacks {
	callbacks.SaveQuietSchedule = func(edit chatui.QuietScheduleEdit) { go saveChatQuietSchedule(cfg, edit) }
	return callbacks
}

// loadChatQuietSchedule reads the schedule. A refresh that fails after a good
// load keeps what is on screen; only a first load that fails says unavailable.
func loadChatQuietSchedule(cfg journeyclient.Config) {
	chatQuietScheduleBrowser.Lock()
	chatQuietScheduleBrowser.serial++
	serial := chatQuietScheduleBrowser.serial
	chatQuietScheduleBrowser.Unlock()
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	view, err := chatQuietScheduleRead(ctx, http.DefaultClient, active)
	chatQuietScheduleBrowser.Lock()
	stale := serial != chatQuietScheduleBrowser.serial
	if err == nil && !stale {
		chatQuietScheduleBrowser.revision = view.Revision
	}
	chatQuietScheduleBrowser.Unlock()
	if stale {
		// A newer read is in flight; it has the last word.
		return
	}
	chatBrowser.mutate(func(m *chatui.Model) {
		if err != nil {
			m.QuietSchedule.Failed = !m.QuietSchedule.Loaded
			return
		}
		view.Note, view.NoteDay = m.QuietSchedule.Note, m.QuietSchedule.NoteDay
		m.QuietSchedule = view
	})
	refreshChatRoute()
}

// saveChatQuietSchedule keeps one change. One save runs at a time, each with
// the revision the last answer gave; a conflict reads the schedule again and
// says so, a refusal names the day.
func saveChatQuietSchedule(cfg journeyclient.Config, edit chatui.QuietScheduleEdit) {
	chatQuietScheduleBrowser.write.Lock()
	defer chatQuietScheduleBrowser.write.Unlock()
	chatQuietScheduleBrowser.Lock()
	revision := chatQuietScheduleBrowser.revision
	chatQuietScheduleBrowser.Unlock()
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	view, outcome, day := chatQuietScheduleWrite(ctx, http.DefaultClient, active, edit, revision, time.Now())
	if outcome == "saved" {
		chatQuietScheduleBrowser.Lock()
		chatQuietScheduleBrowser.serial++
		chatQuietScheduleBrowser.revision = view.Revision
		chatQuietScheduleBrowser.Unlock()
	}
	chatBrowser.mutate(func(m *chatui.Model) {
		if outcome == "saved" {
			view.Note = "saved"
			m.QuietSchedule = view
			return
		}
		m.QuietSchedule.Note, m.QuietSchedule.NoteDay = outcome, day
	})
	refreshChatRoute()
	if outcome == "conflict" || outcome == "failed" {
		loadChatQuietSchedule(cfg)
	}
}
