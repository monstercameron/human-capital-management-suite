//go:build js && wasm

package main

import (
	"context"
	"strings"
	"syscall/js"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func validChatEmbedResult(result *chatv1.ResolveShareLinkResponse, tenant string) bool {
	if result == nil || result.GetConversation() == nil || result.GetPost() == nil {
		return false
	}
	conversation, post := result.GetConversation(), result.GetPost()
	return tenant != "" && conversation.GetTenantId() == tenant && post.GetTenantId() == tenant && post.GetConversationId() == conversation.GetId() && !post.GetDeleted()
}

// resolveVisibleChatEmbeds is called after a chat render. The claim operation
// is bounded and deduplicated; every token is resolved as the current viewer.
func resolveVisibleChatEmbeds(cfg journeyclient.Config) {
	startChatEmbedRefreshTimer(cfg)
	tokens, epoch := chatBrowser.claimChatEmbeds(time.Now())
	if len(tokens) == 0 {
		return
	}
	refreshChatRoute()
	client := chatBrowser.conversationClient()
	active := chatBrowser.config(cfg)
	semaphore := make(chan struct{}, 4)
	for _, token := range tokens {
		go func(token string) {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			value := chatui.LinkEmbed{Token: token, State: "unavailable"}
			if client != nil && active.Bearer != "" {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				result, err := client.ResolveShareLink(chatRPCContext(ctx, active), &chatv1.ResolveShareLinkRequest{Token: token})
				cancel()
				if err == nil && validChatEmbedResult(result, active.Tenant) {
					post := chatMessage(result.GetPost(), active.Locale, chatDirectorySnapshot(), time.Now())
					value = chatui.LinkEmbed{Token: token, State: "ready", SourceRoom: result.GetConversation().GetId(), SourcePost: post.ID, Channel: result.GetConversation().GetName(), Author: post.Author, Body: post.Body, TimeLabel: post.TimeLabel, AttachmentCount: len(post.Attachments)}
				}
			}
			if active.Tenant != chatBrowser.config(cfg).Tenant || active.Subject != chatBrowser.config(cfg).Subject {
				return
			}
			if chatBrowser.finishChatEmbed(epoch, token, value, time.Now()) {
				refreshChatRoute()
			}
		}(token)
	}
}

func startChatEmbedRefreshTimer(cfg journeyclient.Config) {
	epoch, start := chatBrowser.startChatEmbedTimer()
	if !start {
		return
	}
	var tick func()
	tick = func() {
		time.AfterFunc(time.Second, func() {
			if !chatBrowser.continueChatEmbedTimer(epoch) {
				return
			}
			resolveVisibleChatEmbeds(cfg)
			tick()
		})
	}
	tick()
}

// scheduleChatDraftEmbeds waits for paste/input to settle, then repaints only
// if this room still owns that exact draft. Ordinary keystrokes remain local.
func scheduleChatDraftEmbeds(cfg journeyclient.Config, room, body string) {
	if !chatBrowser.markChatEmbedDraftChange(room, body) {
		return
	}
	sig := chatEmbedDraftSignature(body, chatBrowser.snapshot().EmbedOrigin)
	time.AfterFunc(180*time.Millisecond, func() {
		if chatBrowser.selectedID() != room || chatEmbedDraftSignature(chatBrowser.draft(room), chatBrowser.snapshot().EmbedOrigin) != sig {
			return
		}
		refreshChatRoute()
	})
}

func openChatEmbeddedMessage(cfg journeyclient.Config, key string) {
	m := chatBrowser.snapshot()
	room, post := "", ""
	if strings.HasPrefix(key, "legacy:") {
		id := strings.TrimPrefix(key, "legacy:")
		for _, message := range m.Messages {
			if message.ID == id {
				room, post = m.SelectedID, id
				break
			}
		}
	} else if embed, ok := m.Embeds[key]; ok && embed.State == "ready" {
		room, post = embed.SourceRoom, embed.SourcePost
	}
	if room == "" || post == "" {
		return
	}
	if room != m.SelectedID {
		openChatConversation(cfg, room)
	}
	focusChatOriginalPost(room, post)
}

func focusChatOriginalPost(room, post string) {
	frames := js.Global().Get("requestAnimationFrame")
	if frames.Type() != js.TypeFunction {
		return
	}
	attempts := 0
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		if chatBrowser.selectedID() != room {
			frame.Release()
			return nil
		}
		rows := js.Global().Get("document").Call("querySelectorAll", ".chat-workspace [data-message-id]")
		for i := 0; i < rows.Get("length").Int(); i++ {
			row := rows.Index(i)
			if row.Get("dataset").Get("messageId").String() == post {
				row.Call("scrollIntoView", js.ValueOf(map[string]any{"block": "center"}))
				row.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
				frame.Release()
				return nil
			}
		}
		attempts++
		if attempts >= 90 {
			frame.Release()
			return nil
		}
		frames.Invoke(frame)
		return nil
	})
	frames.Invoke(frame)
}

func openChatShareFragment(cfg journeyclient.Config) {
	location := js.Global().Get("location")
	if !location.Truthy() {
		return
	}
	hash := location.Get("hash").String()
	if !strings.HasPrefix(hash, "#share=") {
		return
	}
	origin := location.Get("origin").String()
	locators := chatui.ShareLocators("/workspace/app/chat"+hash, origin)
	if len(locators) != 1 || locators[0].Token == "" {
		return
	}
	token := locators[0].Token
	if !chatBrowser.claimOpenShareFragment(token) {
		return
	}
	go func() {
		client := chatBrowser.conversationClient()
		active := chatBrowser.config(cfg)
		if client == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result, err := client.ResolveShareLink(chatRPCContext(ctx, active), &chatv1.ResolveShareLinkRequest{Token: token})
		if err != nil || !validChatEmbedResult(result, active.Tenant) {
			noteChatAction("Message preview unavailable")
			return
		}
		if active.Tenant != chatBrowser.config(cfg).Tenant || active.Subject != chatBrowser.config(cfg).Subject {
			return
		}
		room := result.GetConversation().GetId()
		chatBrowser.mutate(func(m *chatui.Model) {
			found := false
			for _, c := range m.Conversations {
				if c.ID == room {
					found = true
					break
				}
			}
			if !found {
				m.Conversations = append(m.Conversations, chatConversation(result.GetConversation()))
			}
		})
		openChatConversation(active, room)
		focusChatOriginalPost(room, result.GetPost().GetId())
	}()
}
