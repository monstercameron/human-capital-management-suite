//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3916: the Followed threads panel's browser side. The list comes from
// the server's own-person read; Stop following uses the thread-follow RPC the
// thread header uses, so both write the same stored row.

var chatFollowedBrowser struct {
	sync.Mutex
	reply  followedThreadsReply
	serial uint64
}

func withIssue3916Callbacks(callbacks chatui.Callbacks, cfg journeyclient.Config) chatui.Callbacks {
	callbacks.LoadFollowedThreads = func() { go loadChatFollowed(cfg) }
	callbacks.OpenFollowedThread = func(conversation, root string, sequence uint64) {
		openChatFollowed(cfg, conversation, root, sequence)
	}
	callbacks.UnfollowThread = func(conversation, root string) { go unfollowChatFollowed(cfg, conversation, root) }
	return callbacks
}

// loadChatFollowed reads the list. A refresh that fails after a good load keeps
// the rows on screen; only a first load that fails shows the unavailable state.
func loadChatFollowed(cfg journeyclient.Config) {
	chatFollowedBrowser.Lock()
	chatFollowedBrowser.serial++
	serial := chatFollowedBrowser.serial
	chatFollowedBrowser.Unlock()
	active := chatBrowser.config(cfg)
	chatBrowser.mutate(func(m *chatui.Model) {
		m.FollowedLoading, m.FollowedError = !m.FollowedLoaded, false
	})
	refreshChatRoute()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	reply, err := chatFollowedRequest(ctx, http.DefaultClient, active)
	chatFollowedBrowser.Lock()
	if serial != chatFollowedBrowser.serial {
		// A newer read is in flight; it will have the last word.
		chatFollowedBrowser.Unlock()
		return
	}
	if err == nil {
		chatFollowedBrowser.reply = reply
	}
	chatFollowedBrowser.Unlock()
	model := chatBrowser.snapshot()
	var rows []chatui.FollowedThreadRow
	if err == nil {
		rows = chatFollowedRows(reply, active, model, chatDirectorySnapshot(), time.Now())
	}
	chatBrowser.mutate(func(m *chatui.Model) {
		m.FollowedLoading = false
		if err != nil {
			m.FollowedError = !m.FollowedLoaded
			return
		}
		m.FollowedThreads, m.FollowedLoaded, m.FollowedError = rows, true, false
	})
	refreshChatRoute()
}

// openChatFollowed opens a followed thread in its conversation. Opening reads
// the conversation the way choosing it anywhere else does; the list itself
// never marks anything read.
func openChatFollowed(cfg journeyclient.Config, conversation, root string, sequence uint64) {
	model := chatBrowser.snapshot()
	hasRoot := func(m chatui.Model) bool {
		for _, message := range m.Messages {
			if message.ID == root {
				return true
			}
		}
		return false
	}
	if model.SelectedID == conversation && hasRoot(model) && model.Callbacks.OpenThread != nil {
		chatBrowser.mutate(func(m *chatui.Model) { m.SidebarOpen = false })
		model.Callbacks.OpenThread(root)
		return
	}
	if model.Callbacks.OpenSearchMessage == nil {
		return
	}
	model.Callbacks.OpenSearchMessage(conversation, root, sequence)
	generation := chatBrowser.currentGeneration()
	go func() {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if !chatBrowser.generationActive(generation) {
				return
			}
			current := chatBrowser.snapshot()
			if current.SelectedID == conversation && !chatBrowser.alreadyOpening(conversation) && (current.State == chatui.StateReady || current.State == chatui.StateEmpty) {
				if current.ShowThread && current.ThreadParentID == root {
					return
				}
				if hasRoot(current) && current.Callbacks.OpenThread != nil {
					open := current.Callbacks.OpenThread
					ui.PostAsync(func() { open(root) })
					return
				}
				// The conversation opened but the root is not in it any more.
				noteChatAction(chatui.ThreadUnavailableNotice(chatBrowser.localeTag()))
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		noteChatAction(chatui.ThreadUnavailableNotice(chatBrowser.localeTag()))
	}()
}

// unfollowChatFollowed turns one follow off with the revision the list carried,
// then drops the row and, when that thread is open, the header's state. A
// refusal keeps the row, says so, and reads the list again.
func unfollowChatFollowed(cfg journeyclient.Config, conversation, root string) {
	chatFollowedBrowser.Lock()
	revision, known := chatFollowedRevision(chatFollowedBrowser.reply, conversation, root)
	chatFollowedBrowser.Unlock()
	chatRecipientBrowser.Lock()
	client := chatRecipientBrowser.client
	generation := chatRecipientBrowser.generation
	chatRecipientBrowser.Unlock()
	fail := func() {
		chatBrowser.mutate(func(m *chatui.Model) { m.FollowedNote = "unfollow_failed" })
		refreshChatRoute()
		loadChatFollowed(cfg)
	}
	if client == nil || !known {
		fail()
		return
	}
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := client.PutThreadFollow(chatRPCContext(ctx, active), &chatv1.PutThreadFollowRequest{Follow: &chatv1.ThreadFollow{TenantId: active.Tenant, ConversationId: conversation, RootPostId: root, Followed: false}, ExpectedRevision: revision})
	if err != nil {
		fail()
		return
	}
	chatRecipientBrowser.Lock()
	if generation == chatRecipientBrowser.generation {
		chatRecipientBrowser.follows[conversation+"\x00"+root] = result.GetFollow()
	}
	chatRecipientBrowser.Unlock()
	chatBrowser.mutate(func(m *chatui.Model) {
		m.FollowedThreads = chatFollowedWithout(m.FollowedThreads, conversation, root)
		m.FollowedNote = ""
		if m.SelectedID == conversation && m.ThreadParentID == root {
			m.ThreadFollowed = false
		}
	})
	refreshChatRoute()
	// The stored list is read again so the next unfollow has a fresh revision.
	loadChatFollowed(cfg)
}
