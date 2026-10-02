//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// chatvoiceAccess remembers which conversation the composer last asked about, so
// a conversation is asked once per visit and again when a voice switch changes.
var chatvoiceAccess struct {
	sync.Mutex
	room string
	once sync.Once
}

// chatvoiceSyncAccess asks the server where voice messages may be sent from the
// open conversation (CHATVOICE-002): the Add menu lists the voice item, or shows
// it off with the reason, from the answer. A failed read keeps the last answer
// and is tried again with backoff; the server enforces the switches at send
// whatever this page believes.
func chatvoiceSyncAccess(parent context.Context, cfg journeyclient.Config) {
	model := chatBrowser.snapshot()
	room := model.SelectedID
	if room == "" || parent.Err() != nil {
		return
	}
	tenant := model.CurrentTenantID
	for _, c := range model.Conversations {
		if c.ID == room && c.HostTenantID != "" {
			tenant = c.HostTenantID
		}
	}
	chatvoiceAccess.once.Do(func() {
		// A switch saved in Conversation details or Chat preferences changes the
		// answer for the open conversation: ask again.
		changed := js.FuncOf(func(_ js.Value, _ []js.Value) any {
			chatvoiceAccess.Lock()
			chatvoiceAccess.room = ""
			chatvoiceAccess.Unlock()
			integrate2SyncProjection()
			return nil
		})
		js.Global().Get("document").Call("addEventListener", "chat-voice-switches-changed", changed)
	})
	chatvoiceAccess.Lock()
	if chatvoiceAccess.room == room {
		chatvoiceAccess.Unlock()
		return
	}
	chatvoiceAccess.room = room
	chatvoiceAccess.Unlock()
	cfg = chatBrowser.config(cfg)
	ask := func() bool {
		call, stop := context.WithTimeout(parent, 15*time.Second)
		defer stop()
		var view chatui.VoiceSettingsData
		err := voiceRequest(call, http.DefaultClient, cfg, "policy", map[string]string{"TenantID": tenant, "ConversationID": room}, &view)
		if parent.Err() != nil {
			return true
		}
		if err != nil {
			return chatux012HTTPFinal(err)
		}
		ui.PostAsync(func() {
			chatBrowser.mutate(func(m *chatui.Model) {
				next := map[string]chatui.VoiceAccessView{}
				for k, old := range m.VoiceAccess {
					next[k] = old
				}
				next[room] = chatui.VoiceAccessView{Allowed: view.Allowed, Reason: view.Reason}
				m.VoiceAccess = next
			})
			chatStreamRender.Schedule()
		})
		return true
	}
	go func() {
		if !ask() {
			chatux012Retry("chatvoice|access|"+room, func() bool { return parent.Err() == nil && chatBrowser.selectedID() == room }, ask)
		}
	}()
}
