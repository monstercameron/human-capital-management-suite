//go:build js && wasm

package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// chatsearchOpenGate opens the gate form of a channel from a search result. The
// form is the one the Browse list opens: the questions, and the person's own
// answers and request below them. Focus returns to the result on close.
func chatsearchOpenGate(conversation string) {
	if conversation == "" {
		return
	}
	go func() {
		cfg := chatBrowser.config(journeyclient.Config{})
		locale := chatBrowser.snapshot().Locale
		reply, _ := chatgateFetch(cfg, chatgateClientRequest{Conversation: conversation, Action: "get", Locale: locale})
		if reply.View == nil {
			reply.View = &chatui.GateView{Conversation: conversation, Locale: locale, Error: "unavailable"}
		}
		mountChatgate(cfg, *reply.View)
	}()
}
