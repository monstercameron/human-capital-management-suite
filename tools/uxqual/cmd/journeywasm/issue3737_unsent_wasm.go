//go:build js && wasm

package main

import (
	"context"
	"strings"
	"sync"
	"syscall/js"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3737: the browser side of the "Not sent" row. The store and its rules
// are in issue3737_unsent.go; this file is the page wiring.

// withIssue3737Callbacks gives the row its three controls.
func withIssue3737Callbacks(callbacks chatui.Callbacks, cfg journeyclient.Config) chatui.Callbacks {
	callbacks.RetryUnsent = func(id string) {
		chatUnsent.manual(id)
		go deliverUnsentMessage(cfg, id)
	}
	callbacks.EditUnsent = func(id string) { editUnsentMessage(cfg, id) }
	callbacks.DiscardUnsent = func(id string) {
		if _, sending := chatUnsentInFlight(id); sending {
			// Already on its way: dropping the row now would hide a message the
			// server may still accept.
			return
		}
		if entry, ok := chatUnsent.get(id); ok {
			chatUnsent.remove(id)
			chatBrowser.clearSendKey(entry.Conversation)
		}
		syncChatUnsent()
		refreshChatRoute()
	}
	return callbacks
}

// syncChatUnsent copies the store's rows into the model the page draws.
func syncChatUnsent() {
	rows := chatUnsent.rows()
	setChatUnsentUnloadGuard(len(rows) > 0)
	chatBrowser.mutate(func(model *chatui.Model) { model.Unsent = rows })
}

func chatBrowserOnline() bool {
	navigator := js.Global().Get("navigator")
	if !navigator.Truthy() {
		return true
	}
	online := navigator.Get("onLine")
	if online.Type() != js.TypeBoolean {
		return true
	}
	return online.Bool()
}

// editUnsentMessage takes the text back into the composer, in front of
// anything already typed there, and drops the row.
func editUnsentMessage(cfg journeyclient.Config, id string) {
	entry, ok := chatUnsent.get(id)
	if !ok {
		return
	}
	if _, sending := chatUnsentInFlight(id); sending {
		return
	}
	chatUnsent.remove(id)
	body := entry.Body
	model := chatBrowser.snapshot()
	if model.SelectedID == entry.Conversation && strings.TrimSpace(model.Draft) != "" {
		body = entry.Body + "\n" + model.Draft
	}
	chatBrowser.restoreDraft(entry.Conversation, body)
	flushChatDraftPersist(chatBrowser.config(cfg))
	syncChatUnsent()
	refreshChatRoute()
}

func chatUnsentInFlight(id string) (chatUnsentEntry, bool) {
	chatUnsent.mu.Lock()
	defer chatUnsent.mu.Unlock()
	if e := chatUnsent.find(id); e != nil {
		return *e, e.inFlight
	}
	return chatUnsentEntry{}, false
}

// deliverUnsentMessage makes one delivery attempt for an entry under its own
// idempotency key. It is the only place that posts a main-composer message.
func deliverUnsentMessage(cfg journeyclient.Config, id string) {
	entry, ok := chatUnsent.get(id)
	if !ok {
		return
	}
	active := chatBrowser.config(cfg)
	if !chatDraftIdentityMatches(entry.Tenant, entry.Subject, active.Tenant, active.Subject) {
		// Another person's text is never sent under this session.
		chatUnsent.remove(id)
		syncChatUnsent()
		refreshChatRoute()
		return
	}
	if !chatUnsent.begin(id) {
		return
	}
	syncChatUnsent()
	refreshChatRoute()
	online := chatBrowserOnline()
	client := chatBrowser.conversationClient()
	if client == nil || !online {
		chatUnsent.fail(id, chatui.UnsentOffline)
		syncChatUnsent()
		refreshChatRoute()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sent, err := client.SendPost(chatRPCContext(ctx, active), &chatv1.SendPostRequest{
		TenantId: active.Tenant, ConversationId: entry.Conversation, Body: entry.Body, IdempotencyKey: entry.Key,
		AudienceSegments: entry.Audience, AudienceRestricted: entry.Restricted,
	})
	if err != nil {
		if blocked, _ := chatmod002BlockedWords(err, entry.Body); blocked {
			// A moderation answer names the words in the box and keeps the
			// text there to be edited; it is not a message waiting to send.
			chatUnsent.remove(id)
			syncChatUnsent()
			chatBrowser.restoreDraft(entry.Conversation, entry.Body)
			flushChatDraftPersist(active)
			chatmod002Failed("send this message", err, entry.Conversation, chatui.ModAuthorKeyComposer(entry.Conversation), chatui.ModAuthorSurfaceMessage, entry.Body)
			chatStreamRender.Schedule()
			return
		}
		chatUnsent.fail(id, chatUnsentReason(err, chatBrowserOnline()))
		syncChatUnsent()
		refreshChatRoute()
		return
	}
	chatUnsent.remove(id)
	syncChatUnsent()
	chatgateComposerAudienceReset()
	// The response is the committed post, so the message appears from it; the
	// stream delivers the rest.
	_, gapped := chatBrowser.applySentChatPost(entry.Conversation, sent.GetPost(), active.Locale, chatDirectorySnapshot(), time.Now())
	if gapped {
		catchUpChatConversation(ctx, entry.Conversation, chatBrowser.currentGeneration(), active)
	}
	invalidateChatRecipientProjection()
	chatBrowser.clearNotice(0)
	chatStreamRender.Schedule()
}

// resendChatUnsent sends, oldest first, the entries that failed because the
// connection was down.
func resendChatUnsent(cfg journeyclient.Config) {
	active := chatBrowser.config(cfg)
	for _, id := range chatUnsent.resendable(active.Tenant, active.Subject) {
		deliverUnsentMessage(cfg, id)
	}
	syncChatUnsent()
}

var chatUnsentRecoveryOnce sync.Once

// installChatUnsentRecovery resends offline entries when the browser reports
// the connection back, and now and then while it claims to be online but the
// server was unreachable. Safe to call on every send.
func installChatUnsentRecovery(cfg journeyclient.Config) {
	chatUnsentRecoveryOnce.Do(func() {
		js.Global().Call("addEventListener", "online", js.FuncOf(func(js.Value, []js.Value) any {
			chatUnsent.rearmOffline()
			go resendChatUnsent(cfg)
			return nil
		}))
		go func() {
			for {
				time.Sleep(20 * time.Second)
				if chatBrowserOnline() {
					resendChatUnsent(cfg)
				}
			}
		}()
	})
}

// The text of a message that has not been delivered lives only on this page, so
// leaving or reloading while one exists asks the person first (the same browser
// prompt the draft guard uses, kept apart from it so neither switches the
// other off).
var chatUnsentUnloadGuard struct {
	mu       sync.Mutex
	on       bool
	listener js.Func
}

func setChatUnsentUnloadGuard(on bool) {
	chatUnsentUnloadGuard.mu.Lock()
	defer chatUnsentUnloadGuard.mu.Unlock()
	if chatUnsentUnloadGuard.on == on {
		return
	}
	window := js.Global()
	if !window.Truthy() || window.Get("addEventListener").Type() != js.TypeFunction {
		return
	}
	if on {
		chatUnsentUnloadGuard.listener = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				args[0].Call("preventDefault")
				args[0].Set("returnValue", "")
			}
			return nil
		})
		window.Call("addEventListener", "beforeunload", chatUnsentUnloadGuard.listener)
	} else {
		window.Call("removeEventListener", "beforeunload", chatUnsentUnloadGuard.listener)
		chatUnsentUnloadGuard.listener.Release()
	}
	chatUnsentUnloadGuard.on = on
}
