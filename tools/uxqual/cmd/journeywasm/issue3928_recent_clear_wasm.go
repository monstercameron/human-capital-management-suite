//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"syscall/js"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// chatsearchClearRecent deletes the account's stored recent searches from the
// sidebar popover (ISSUE-3928). The list is emptied only after the server
// confirms; a failure keeps the list and says so. Focus goes back to the search
// box either way, because the Clear button may disappear with the list.
func chatsearchClearRecent(cfg journeyclient.Config) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := chatsearchRequest(ctx, http.DefaultClient, chatBrowser.config(cfg), "/api/chat/search/recent", http.MethodDelete, nil, nil)
		chatBrowser.mutate(func(m *chatui.Model) {
			if err != nil {
				m.SearchRecentStatus = "failed"
				return
			}
			m.SearchRecent = nil
			m.SearchRecentStatus = "cleared"
		})
		chatsearchBrowser.Lock()
		if err == nil {
			chatsearchBrowser.view.Recent = nil
		}
		chatsearchBrowser.Unlock()
		refreshChatRoute()
		if field := js.Global().Get("document").Call("getElementById", "chat-search"); field.Truthy() {
			field.Call("focus")
		}
	}()
}
