//go:build js && wasm

package main

import (
	"sync"
	"syscall/js"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var chatAttentionOnce sync.Once

// installChatAttention watches the page's visibility and focus for the life of
// the document (issue 3797). A listener never released is fine: there is one.
func installChatAttention() {
	chatAttentionOnce.Do(func() {
		document := js.Global().Get("document")
		if !document.Truthy() {
			return
		}
		read := func() (hidden, blurred bool) {
			hidden = document.Get("hidden").Truthy()
			if focus := document.Get("hasFocus"); focus.Type() == js.TypeFunction {
				blurred = !document.Call("hasFocus").Bool()
			}
			return hidden, blurred
		}
		apply := func(hidden, blurred bool) {
			was, now := setChatAttention(hidden, blurred)
			if !was && now {
				chatAttentionRegained()
			}
			syncChatTabTitle()
		}
		update := js.FuncOf(func(js.Value, []js.Value) any {
			apply(read())
			return nil
		})
		document.Call("addEventListener", "visibilitychange", update)
		js.Global().Call("addEventListener", "focus", update)
		js.Global().Call("addEventListener", "blur", update)
		// Some embedded frames never report focus. A key or a click on a
		// visible page is the person being here, so reading goes on.
		present := js.FuncOf(func(js.Value, []js.Value) any {
			// Only a page counted as away needs telling; otherwise every key
			// and click would rebuild the model snapshot for nothing.
			if !chatPageAway() {
				return nil
			}
			if hidden, _ := read(); !hidden {
				apply(false, false)
			}
			return nil
		})
		document.Call("addEventListener", "pointerdown", present, true)
		document.Call("addEventListener", "keydown", present, true)
		apply(read())
	})
}

// chatAttentionRegained is the person coming back: the open conversation is
// marked read up to the newest message on screen. The "New" line stays where
// the first unseen message put it.
func chatAttentionRegained() {
	cfg := chatBrowser.config(journeyclient.Config{})
	model := chatBrowser.snapshot()
	id := model.SelectedID
	if id == "" || chatReadHeld(id) {
		return
	}
	last := chatux022LastSequence(model.Messages)
	chatBrowser.mutate(func(m *chatui.Model) {
		if m.SelectedID == id {
			markConversationUnreadInModel(m, id, false)
		}
	})
	markChatRead(cfg, id, []*chatv1.Post{{TenantId: chatConversationHost(cfg, id), Sequence: last}})
	refreshChatRoute()
}

// noteChatUnseenWhileAway keeps the "New" line at the first message that
// arrived while the page was away.
func noteChatUnseenWhileAway(conversation string, posts []*chatv1.Post) {
	if !chatPageAway() || conversation == "" {
		return
	}
	var arrived []chatUnseenPost
	for _, post := range posts {
		if post != nil && post.GetParentId() == "" {
			arrived = append(arrived, chatUnseenPost{ID: post.GetId(), AuthorID: post.GetAuthorId()})
		}
	}
	chatBrowser.mutate(func(m *chatui.Model) {
		if m.SelectedID == conversation && !chatReadHeld(conversation) {
			m.UnreadFromID = chatFirstUnseen(m.UnreadFromID, m.CurrentUser, arrived)
		}
	})
}
