//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// chatsearchWorkspace answers the workspace search box from Chat's search. A
// search that fails, or a session Chat is not composed for, answers with no
// rows: the box keeps its own results and says nothing about Chat.
func chatsearchWorkspace(words string, done func([]productui.GlobalSearchItem)) {
	cfg := chatBrowser.config(journeyclient.Config{})
	model := chatBrowser.snapshot()
	request, err := chatsearchWorkspaceRequest(model, words)
	if err != nil || cfg.Bearer == "" || cfg.TunnelURL == "" {
		done(nil)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var response chatsearch.Response
		var items []productui.GlobalSearchItem
		if err := chatsearchRequest(ctx, http.DefaultClient, cfg, "/api/chat/search", http.MethodPost, request, &response); err == nil {
			items = chatsearchWorkspaceItems(model, response, words)
		}
		ui.PostAsync(func() { done(items) })
	}()
}

// chatsearchOpenAddressed opens conversation id at the message its address
// names ("#channel=general&message=…&at=…", written by the workspace search),
// with the thread or the voice sentence the address asks for. It is false when
// the address names no message, and the caller opens the conversation as usual.
func chatsearchOpenAddressed(cfg journeyclient.Config, id, hash string) bool {
	address, ok := chatui.ParseChatSearchAddress(hash)
	if !ok {
		return false
	}
	chatBrowser.mutate(func(m *chatui.Model) { m.FocusMessageID = address.MessageID })
	openChatConversationAt(cfg, id, address.Sequence, address.MessageID)
	if address.Thread {
		go chatsearchOpenAddressedThread(id, address.MessageID)
	}
	if address.Sentence > 0 {
		go chatsearchSeekVoice(chatsearch.Target{ConversationID: id, MessageID: address.MessageID}, address.Sentence)
	}
	return true
}

// chatsearchOpenAddressedThread opens the thread of message parent once the
// conversation has been read, the way choosing "Reply in thread" would.
func chatsearchOpenAddressedThread(id, parent string) {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		model := chatBrowser.snapshot()
		if model.SelectedID != id {
			return
		}
		if !chatBrowser.alreadyOpening(id) && (model.State == chatui.StateReady || model.State == chatui.StateEmpty) {
			open := model.Callbacks.OpenThread
			if open == nil {
				return
			}
			for _, message := range model.Messages {
				if message.ID == parent {
					ui.PostAsync(func() { open(parent) })
					return
				}
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}
