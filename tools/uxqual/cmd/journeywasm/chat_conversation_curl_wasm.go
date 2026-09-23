//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func copyChatConversationAPICurl(conversationID string) {
	model := chatBrowser.snapshot()
	if !chatConversationIsVisible(model, conversationID) {
		return
	}
	active := chatBrowser.config(journeyclient.Config{})
	location, navigator := js.Global().Get("location"), js.Global().Get("navigator")
	locale := productui.ResolveProductLocale(active.Locale)
	valid := func() bool {
		current := chatBrowser.config(journeyclient.Config{})
		return current.Tenant == active.Tenant && current.Subject == active.Subject && chatConversationIsVisible(chatBrowser.snapshot(), conversationID)
	}
	failure := func() {
		if valid() {
			noteChatAction(locale.Text(chatui.KeyCopyConversationAPICurlFailure))
		}
	}
	finished := false
	defer func() {
		if recover() != nil && !finished {
			failure()
		}
	}()
	if !location.Truthy() || !navigator.Truthy() || !navigator.Get("clipboard").Truthy() || navigator.Get("clipboard").Get("writeText").Type() != js.TypeFunction {
		failure()
		return
	}
	command, err := chatui.ConversationPostCurl(location.Get("origin").String(), conversationID)
	if err != nil {
		failure()
		return
	}
	// The write happens inside the click activation so browsers that gate
	// clipboard access do not lose permission while the snippet is prepared.
	promise := navigator.Get("clipboard").Call("writeText", command)
	var onDone, onFail js.Func
	onDone = js.FuncOf(func(js.Value, []js.Value) any {
		finished = true
		onDone.Release()
		onFail.Release()
		if valid() {
			chatActionSucceeded(locale.Text(chatui.KeyCopyConversationAPICurlSuccess))
		}
		return nil
	})
	onFail = js.FuncOf(func(js.Value, []js.Value) any {
		finished = true
		onDone.Release()
		onFail.Release()
		failure()
		return nil
	})
	promise.Call("then", onDone, onFail)
}

func chatConversationIsVisible(model chatui.Model, conversationID string) bool {
	if conversationID == "" {
		return false
	}
	for _, conversation := range model.Conversations {
		if conversation.ID == conversationID {
			return true
		}
	}
	return false
}
