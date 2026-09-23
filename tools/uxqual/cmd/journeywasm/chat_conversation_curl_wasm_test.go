//go:build js && wasm

package main

import (
	"strings"
	"syscall/js"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestCopyChatConversationAPICurlUsesKnownConversationAndCurrentOrigin(t *testing.T) {
	global, object := js.Global(), js.Global().Get("Object")
	replace := func(name string, value any) {
		old := object.Call("getOwnPropertyDescriptor", global, name)
		t.Cleanup(func() {
			if old.Truthy() {
				object.Call("defineProperty", global, name, old)
			} else {
				global.Get("Reflect").Call("deleteProperty", global, name)
			}
		})
		object.Call("defineProperty", global, name, map[string]any{"value": value, "configurable": true})
	}
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "reader", Bearer: "current-user-secret", Locale: "de-DE"}
	chatBrowser.reset(nil, cfg, nil)
	t.Cleanup(func() { chatBrowser.reset(nil, journeyclient.Config{}, nil) })
	chatBrowser.mutate(func(model *chatui.Model) {
		model.Conversations = []chatui.Conversation{{ID: "room-42", Name: "Engineering"}}
		model.SelectedID = "another-room"
	})
	replace("location", js.ValueOf(map[string]any{"origin": "https://chat.example.test"}))
	thenable := object.New()
	resolve := js.FuncOf(func(_ js.Value, args []js.Value) any { args[0].Invoke(); return nil })
	t.Cleanup(resolve.Release)
	thenable.Set("then", resolve)
	var copied []string
	writeText := js.FuncOf(func(_ js.Value, args []js.Value) any {
		copied = append(copied, args[0].String())
		return thenable
	})
	t.Cleanup(writeText.Release)
	clipboard := object.New()
	clipboard.Set("writeText", writeText)
	navigator := object.New()
	navigator.Set("clipboard", clipboard)
	replace("navigator", navigator)

	copyChatConversationAPICurl("room-42")
	if len(copied) != 1 {
		t.Fatalf("clipboard writes = %d, want 1", len(copied))
	}
	command := copied[0]
	for _, want := range []string{
		"https://chat.example.test/v1/conversations/room-42/posts",
		"Authorization: Bearer ${HCM_CHAT_TOKEN}",
		"Content-Type: application/json",
		"Idempotency-Key:",
		`--data '{"body":"Hello from an agent"}'`,
	} {
		if !strings.Contains(command, want) {
			t.Errorf("copied command missing %q:\n%s", want, command)
		}
	}
	if strings.Contains(command, cfg.Bearer) {
		t.Fatal("copied command contains the current user's bearer token")
	}
	if got, want := chatBrowser.currentNotice(), productui.ResolveProductLocale(cfg.Locale).Text(chatui.KeyCopyConversationAPICurlSuccess); got != want {
		t.Fatalf("success notice = %q, want %q", got, want)
	}
	copyChatConversationAPICurl("unknown")
	if len(copied) != 1 {
		t.Fatal("unknown conversation reached clipboard")
	}
}
