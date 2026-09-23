//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestTodo_CHAT_032_Integration(t *testing.T) {
	oldWidth := js.Global().Get("innerWidth")
	js.Global().Set("innerWidth", 1280)
	t.Cleanup(func() { js.Global().Set("innerWidth", oldWidth) })
	model := chatui.Model{Conversations: []chatui.Conversation{{ID: "old", Kind: chatui.PublicChannel}, {ID: "new", Kind: chatui.DirectMessage}}}
	layout := recipientLayout{Sections: []recipientSection{{ID: "all", Chats: []recipientChatRef{{HostTenantID: "home", ConversationID: "old"}}}}}
	applyRecipientLayout(&model, layout, map[string]string{"old": "home", "new": "home"})
	counts := map[string]int{}
	for _, section := range model.Sections {
		for _, chat := range section.Chats {
			counts[chat.ID]++
		}
	}
	if len(model.Sections) != 2 || counts["old"] != 1 || counts["new"] != 1 || model.Sections[0].ID != "channels" || model.Sections[1].ID != "direct" {
		t.Fatalf("legacy sidebar duplicated or lost chats: sections=%+v counts=%v", model.Sections, counts)
	}
	model.Sections = nil
	layout.Sections = []recipientSection{{ID: "custom", Name: "Projects", Chats: []recipientChatRef{{HostTenantID: "home", ConversationID: "old"}}}}
	applyRecipientLayout(&model, layout, map[string]string{"old": "home", "new": "home"})
	if len(model.Sections) != 3 || model.Sections[0].ID != "channels" || model.Sections[1].ID != "custom" || model.Sections[2].ID != "direct" || len(model.Sections[1].Chats) != 1 || len(model.Sections[2].Chats) != 1 {
		t.Fatalf("custom-only sidebar did not restore defaults: %+v", model.Sections)
	}
}
