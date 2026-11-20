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

// Issue 3788: the Scheduled tray and Send later controls' browser side. Each
// change is sent to the server first; the tray then shows what the server holds.

var chatScheduledBrowser struct {
	sync.Mutex
	serial uint64
}

func withIssue3788Callbacks(callbacks chatui.Callbacks, cfg journeyclient.Config) chatui.Callbacks {
	callbacks.LoadScheduled = func() { go loadChatScheduled(cfg) }
	callbacks.ScheduleSend = func(conversation, body string, at time.Time, done func(chatui.SendLaterResult)) {
		go scheduleChatSend(cfg, conversation, body, at, done)
	}
	callbacks.EditScheduled = func(id, body string, at time.Time, revision uint64, done func(chatui.SendLaterResult)) {
		go editChatScheduled(cfg, id, body, at, revision, done)
	}
	callbacks.CancelScheduled = func(id string) { go cancelChatScheduled(cfg, id) }
	return callbacks
}

// loadChatScheduled reads the tray. A refresh that fails after a good load keeps
// the rows on screen; only a first load that fails shows the unavailable state.
func loadChatScheduled(cfg journeyclient.Config) {
	chatScheduledBrowser.Lock()
	chatScheduledBrowser.serial++
	serial := chatScheduledBrowser.serial
	chatScheduledBrowser.Unlock()
	active := chatBrowser.config(cfg)
	chatBrowser.mutate(func(m *chatui.Model) {
		m.SendLater.Loading, m.SendLater.Fail = !m.SendLater.Loaded, false
	})
	refreshChatRoute()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	reply, err := chatScheduledList(ctx, http.DefaultClient, active)
	chatScheduledBrowser.Lock()
	stale := serial != chatScheduledBrowser.serial
	chatScheduledBrowser.Unlock()
	if stale {
		// A newer read is in flight; it will have the last word.
		return
	}
	var rows []chatui.ScheduledRow
	if err == nil {
		rows = chatScheduledRows(reply, chatBrowser.snapshot())
	}
	chatBrowser.mutate(func(m *chatui.Model) {
		m.SendLater.Loading = false
		if err != nil {
			m.SendLater.Fail = !m.SendLater.Loaded
			return
		}
		m.SendLater.Rows, m.SendLater.Loaded, m.SendLater.Fail, m.SendLater.Note = rows, true, false, ""
	})
	refreshChatRoute()
}

// scheduleChatSend keeps one message for later. done runs on the page's own
// thread with the outcome; the tray is read again either way it ends.
func scheduleChatSend(cfg journeyclient.Config, conversation, body string, at time.Time, done func(chatui.SendLaterResult)) {
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	post, outcome := chatScheduledSend(ctx, http.DefaultClient, active, http.MethodPost, scheduledPostBody{ConversationID: conversation, Body: body, SendAt: at.UTC().Format(time.RFC3339), Zone: chatcmd002TimeZone()})
	result := chatui.SendLaterResult{Outcome: outcome, SendAt: post.SendAt, QuietPeople: post.QuietPeople}
	ui.PostAsync(func() { done(result) })
	loadChatScheduled(cfg)
}

func editChatScheduled(cfg journeyclient.Config, id, body string, at time.Time, revision uint64, done func(chatui.SendLaterResult)) {
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	post, outcome := chatScheduledSend(ctx, http.DefaultClient, active, http.MethodPut, scheduledPostBody{ID: id, Body: body, SendAt: at.UTC().Format(time.RFC3339), Zone: chatcmd002TimeZone(), Revision: revision})
	result := chatui.SendLaterResult{Outcome: outcome, SendAt: post.SendAt, QuietPeople: post.QuietPeople}
	ui.PostAsync(func() { done(result) })
	loadChatScheduled(cfg)
}

// cancelChatScheduled removes one message and its text. The row goes at once
// when the server agrees; a refusal keeps it, says so, and reads the list again.
func cancelChatScheduled(cfg journeyclient.Config, id string) {
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, outcome := chatScheduledSend(ctx, http.DefaultClient, active, http.MethodDelete, scheduledPostBody{ID: id}); outcome != "saved" {
		chatBrowser.mutate(func(m *chatui.Model) { m.SendLater.Note = "failed" })
		refreshChatRoute()
		loadChatScheduled(cfg)
		return
	}
	chatBrowser.mutate(func(m *chatui.Model) {
		m.SendLater.Note = ""
		m.SendLater.Rows = chatScheduledWithout(m.SendLater.Rows, id)
	})
	refreshChatRoute()
	loadChatScheduled(cfg)
}
