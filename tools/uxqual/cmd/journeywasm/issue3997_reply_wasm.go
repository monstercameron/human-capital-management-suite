//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// threadReplyLedger is the page's memory of which half of an "also send to the
// conversation" reply already committed (#3997).
var threadReplyLedger replyLedger

// sendThreadReply posts a reply under parentID in conversationID, both captured
// when the reader pressed Send, and, when alsoChannel is set, the same words in
// the conversation after the reply has posted. A retry sends only the part that
// is missing; each part keeps its own idempotency key, so an uncertain timeout
// replayed by the same text cannot post twice.
func sendThreadReply(cfg journeyclient.Config, conversationID, parentID, body string, alsoChannel bool) {
	client := chatBrowser.conversationClient()
	if client == nil || parentID == "" || conversationID == "" {
		return
	}
	active := chatBrowser.config(cfg)
	threadScope := conversationID + "\x00" + parentID
	copyScope := conversationID + "\x00copy\x00" + parentID
	// A reply keys its idempotency on the thread, not the room, so the same text
	// in the conversation and under a message are two different attempts.
	chatBrowser.clearAuthorBlocked(chatui.ModAuthorKeyReply(parentID))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	callCtx := chatRPCContext(ctx, active)

	outcome, err := deliverReply(&threadReplyLedger, conversationID, parentID, body, alsoChannel,
		func() error {
			key := chatBrowser.sendKey(threadScope, body, func() string {
				return fmt.Sprintf("wasm-reply-%d", time.Now().UnixNano())
			})
			_, threadErr := client.SendPost(callCtx, &chatv1.SendPostRequest{
				TenantId: active.Tenant, ConversationId: conversationID, Body: body, ParentId: parentID, IdempotencyKey: key,
			})
			if threadErr == nil {
				chatBrowser.clearSendKey(threadScope)
			}
			return threadErr
		},
		func() error {
			key := chatBrowser.sendKey(copyScope, body, func() string {
				return fmt.Sprintf("wasm-reply-copy-%d", time.Now().UnixNano())
			})
			sent, copyErr := client.SendPost(callCtx, &chatv1.SendPostRequest{
				TenantId: active.Tenant, ConversationId: conversationID, Body: body, IdempotencyKey: key,
			})
			if copyErr != nil {
				return copyErr
			}
			chatBrowser.clearSendKey(copyScope)
			if _, gapped := chatBrowser.applySentChatPost(conversationID, sent.GetPost(), active.Locale, chatDirectorySnapshot(), time.Now()); gapped {
				catchUpChatConversation(ctx, conversationID, chatBrowser.currentGeneration(), active)
			}
			return nil
		})

	switch outcome {
	case replyThreadFailed:
		chatmod002Failed("post this reply", err, conversationID, chatui.ModAuthorKeyReply(parentID), chatui.ModAuthorSurfaceMessage, body)
		chatStreamRender.Schedule()
		return
	case replyChannelCopyFail:
		// The reply is in the thread; say so, and put the words back so the
		// reader can retry just the copy.
		refreshChatThread(callCtx, client, active, conversationID, parentID)
		if box := chatBrowser.keepFailedText(conversationID, parentID, body); box != "" {
			chatui.RestoreFieldValue(box, body)
		}
		noteChatAction(chatui.ThreadChannelCopyFailedNotice(chatBrowser.localeTag()))
		chatStreamRender.Schedule()
		return
	}
	refreshChatThread(callCtx, client, active, conversationID, parentID)
	invalidateChatRecipientProjection()
	chatBrowser.clearNotice(0)
	chatStreamRender.Schedule()
}
