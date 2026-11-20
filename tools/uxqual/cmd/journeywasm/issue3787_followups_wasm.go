//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3787: the Follow-ups panel's browser side. The list comes from the
// server's own-person read; a choice is posted back, then the row is updated at
// once and the list is read again so the screen matches what is stored.

var chatFollowUpsBrowser struct {
	sync.Mutex
	serial uint64
}

func withIssue3787Callbacks(callbacks chatui.Callbacks, cfg journeyclient.Config) chatui.Callbacks {
	callbacks.LoadFollowUps = func() { go loadChatFollowUps(cfg) }
	callbacks.OpenFollowUp = func(conversation, post string, sequence uint64, threadRoot string) {
		openChatFollowUp(cfg, conversation, post, sequence, threadRoot)
	}
	callbacks.SetFollowUp = func(conversation, post, direction, action string) {
		go chooseChatFollowUp(cfg, conversation, post, direction, action)
	}
	return callbacks
}

// loadChatFollowUps reads the list. A refresh that fails after a good load keeps
// the rows on screen; only a first load that fails shows the unavailable state.
func loadChatFollowUps(cfg journeyclient.Config) {
	chatFollowUpsBrowser.Lock()
	chatFollowUpsBrowser.serial++
	serial := chatFollowUpsBrowser.serial
	chatFollowUpsBrowser.Unlock()
	active := chatBrowser.config(cfg)
	chatBrowser.mutate(func(m *chatui.Model) {
		m.FollowUpsLoading, m.FollowUpsError = !m.FollowUpsLoaded, false
	})
	refreshChatRoute()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	reply, err := chatFollowUpsRequest(ctx, http.DefaultClient, active)
	chatFollowUpsBrowser.Lock()
	stale := serial != chatFollowUpsBrowser.serial
	chatFollowUpsBrowser.Unlock()
	if stale {
		// A newer read is in flight; it will have the last word.
		return
	}
	var rows []chatui.FollowUpRow
	if err == nil {
		rows = chatFollowUpRows(reply, active, chatBrowser.snapshot(), chatDirectorySnapshot(), time.Now())
	}
	chatBrowser.mutate(func(m *chatui.Model) {
		m.FollowUpsLoading = false
		if err != nil {
			m.FollowUpsError = !m.FollowUpsLoaded
			return
		}
		m.FollowUps, m.FollowUpsLoaded, m.FollowUpsError = rows, true, false
	})
	refreshChatRoute()
}

// openChatFollowUp opens the message in its conversation. A message inside a
// thread opens that thread; a top-level one is scrolled to in the timeline.
// Opening reads the conversation the way choosing it anywhere else does.
func openChatFollowUp(cfg journeyclient.Config, conversation, post string, sequence uint64, threadRoot string) {
	model := chatBrowser.snapshot()
	if model.Callbacks.OpenSearchMessage == nil {
		return
	}
	model.Callbacks.OpenSearchMessage(conversation, post, sequence)
	if threadRoot == "" {
		return
	}
	generation := chatBrowser.currentGeneration()
	go func() {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if !chatBrowser.generationActive(generation) {
				return
			}
			current := chatBrowser.snapshot()
			if current.SelectedID == conversation && !chatBrowser.alreadyOpening(conversation) && (current.State == chatui.StateReady || current.State == chatui.StateEmpty) {
				if current.ShowThread && current.ThreadParentID == threadRoot {
					return
				}
				if current.Callbacks.OpenThread != nil {
					open := current.Callbacks.OpenThread
					ui.PostAsync(func() { open(threadRoot) })
				}
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
}

// chooseChatFollowUp stores one choice, then shows it: a row that the choice
// hides goes away at once, Keep open marks it. A refusal keeps the row, says
// so, and reads the list again.
func chooseChatFollowUp(cfg journeyclient.Config, conversation, post, direction, action string) {
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := chatFollowUpChoose(ctx, http.DefaultClient, active, conversation, post, direction, action, time.Now()); err != nil {
		chatBrowser.mutate(func(m *chatui.Model) { m.FollowUpsNote = "failed" })
		refreshChatRoute()
		loadChatFollowUps(cfg)
		return
	}
	chatBrowser.mutate(func(m *chatui.Model) {
		m.FollowUpsNote = ""
		if action == "keep" {
			m.FollowUps = chatFollowUpsMarkKept(m.FollowUps, conversation, post, direction)
			return
		}
		m.FollowUps = chatFollowUpsWithout(m.FollowUps, conversation, post, direction)
	})
	refreshChatRoute()
	loadChatFollowUps(cfg)
}
