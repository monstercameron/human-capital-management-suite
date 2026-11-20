//go:build js && wasm

package main

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3942, browser half. The pure parts (the bounded private store, where a
// return lands) are in issue3942_reading_anchor.go.

// rememberChatReadingPosition notes where the person is reading in the open
// conversation, just before another one replaces it. Reading at the newest
// messages leaves nothing to come back to, so it clears the place instead.
func rememberChatReadingPosition(cfg journeyclient.Config) {
	model := chatBrowser.snapshot()
	room := model.SelectedID
	if room == "" || model.State != chatui.StateReady || len(model.Messages) == 0 {
		return
	}
	active := chatBrowser.config(cfg)
	postID, offset, ok := chatui.ReadingPosition()
	if !ok {
		chatReadingAnchors.forget(room)
		return
	}
	anchor, ok := chatReadingAnchorFor(model.Messages, postID, offset, time.Now())
	if !ok {
		chatReadingAnchors.forget(room)
		return
	}
	chatReadingAnchors.remember(active.Tenant, active.Subject, room, anchor)
}

// takeChatReadingPlace returns the place to resume in a conversation being
// opened, when nothing else claims the open. The place is consumed either way,
// so it is never offered a second time.
func takeChatReadingPlace(cfg journeyclient.Config, conversationID string, explicitTarget bool) (chatReadingAnchor, bool) {
	active := chatBrowser.config(cfg)
	anchor, ok := chatReadingAnchors.take(active.Tenant, active.Subject, conversationID, time.Now())
	if !ok {
		return chatReadingAnchor{}, false
	}
	model := chatBrowser.snapshot()
	if !chatReadingMayResume(chatConversationUnread(model.Conversations, conversationID), explicitTarget, true) {
		return chatReadingAnchor{}, false
	}
	return anchor, true
}

// setChatReadingResume shows (or, with an empty status, clears) the bar.
func setChatReadingResume(conversationID, status string) {
	chatBrowser.mutate(func(m *chatui.Model) {
		if m.SelectedID == conversationID {
			m.ReadingResume = chatui.ReadingResumeView{ConversationID: conversationID, Status: status}
		}
	})
	refreshChatRoute()
}

// dismissChatReadingResume closes the bar and leaves the timeline where it is.
func dismissChatReadingResume() {
	setChatReadingResume(chatBrowser.selectedID(), "")
}

// jumpFromChatReadingResume closes the bar and goes to the newest messages.
func jumpFromChatReadingResume(cfg journeyclient.Config) {
	setChatReadingResume(chatBrowser.selectedID(), "")
	jumpToNewestChatMessages(cfg)
}
