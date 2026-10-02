//go:build js && wasm

package main

import (
	"context"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"time"
)

var integrate1ChatCleanups []func()

func disposeIntegrate1Chat() {
	for _, cleanup := range integrate1ChatCleanups {
		if cleanup != nil {
			cleanup()
		}
	}
	integrate1ChatCleanups = nil
}
func configureIntegrate1Chat(cfg journeyclient.Config) {
	disposeIntegrate1Chat()
	configureChattoneBrowser(cfg)
	integrate1ChatCleanups = []func(){configureIntegrate2Chat(cfg), disposeChattoneBrowser, installSavedMessages(cfg), configureChatremove(cfg, cfg.Locale), installChatVoice(personaChatHTTPConfig(cfg), func(post chat.Post) {
		current := chatBrowser.snapshot()
		if current.SelectedID == post.ConversationID {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				_, _ = loadChatProjection(ctx)
				refreshChatRoute()
			}()
		}
	})}
}
