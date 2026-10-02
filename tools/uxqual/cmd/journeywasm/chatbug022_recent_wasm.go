//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// chatsearchRefreshRecent reads the person's recent searches after a search has
// answered (CHATBUG-022). The read used to sit between the search and its
// results, so every search was two requests in a row and the results waited for
// the second one; now the results are drawn first, and the list is refreshed
// when it arrives, if the search it follows is still the one on screen.
func chatsearchRefreshRecent(cfg journeyclient.Config, generation uint64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		recent := []string{}
		if err := chatsearchRequest(ctx, http.DefaultClient, chatBrowser.config(cfg), "/api/chat/search/recent", http.MethodGet, nil, &recent); err != nil {
			return
		}
		chatsearchBrowser.Lock()
		current := chatsearchBrowser.flow.current(generation)
		if current {
			chatsearchBrowser.view.Recent = recent
		}
		view := chatsearchBrowser.view
		chatsearchBrowser.Unlock()
		if !current {
			return
		}
		chatBrowser.mutate(func(m *chatui.Model) { chatsearchModelShow(m, view) })
		refreshChatRoute()
	}()
}
