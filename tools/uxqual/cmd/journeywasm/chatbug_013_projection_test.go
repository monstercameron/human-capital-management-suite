//go:build !(js && wasm)

package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// A projection that knows only a name and an agent id (an invocation binding or
// a reload) must keep the icon the directory already supplied; wiping it is why
// an agent's row and messages fell back to the shared diamond.
func TestTodo_CHATBUG_013(t *testing.T) {
	icon := agenticon.Generate(agenticon.Input{Name: "Policy Helper"})
	room := chatui.Conversation{ID: "dm", Kind: chatui.DirectMessage}
	model := chatui.Model{Conversations: []chatui.Conversation{room}, Sections: []chatui.SidebarSection{{Chats: []chatui.Conversation{room}}}, Members: []chatui.Member{{ID: "agent"}}}
	if !applyAgentDirectConversation(&model, "dm", "agent", "Policy Helper", agentDirectIdentity{Icon: icon, Revision: 2}) {
		t.Fatal("identity not applied")
	}
	if applyAgentDirectConversation(&model, "dm", "agent", "Policy Helper") {
		t.Fatal("a name-only projection repainted an unchanged row")
	}
	for _, kept := range []chatui.Conversation{model.Conversations[0], model.Sections[0].Chats[0]} {
		if kept.Icon != icon || kept.IconRevision != 2 {
			t.Fatalf("name-only projection erased the icon: %+v", kept)
		}
	}
	if model.Members[0].Icon != icon || model.Members[0].IconRevision != 2 {
		t.Fatalf("name-only projection erased the member icon: %+v", model.Members[0])
	}
	// A renamed agent keeps its icon through a name-only projection too.
	if !applyAgentDirectConversation(&model, "dm", "agent", "Policy Desk") || model.Conversations[0].Icon != icon || model.Conversations[0].Name != "Policy Desk" {
		t.Fatalf("rename lost the icon: %+v", model.Conversations[0])
	}

	// Reloading the conversation list starts from rows with no icon; the
	// previous model's stored icon is carried over.
	previous := model
	reloaded := chatui.Model{Conversations: []chatui.Conversation{room}, Members: []chatui.Member{{ID: "agent"}}}
	preserveAgentConversationIdentity(previous, &reloaded)
	if got := reloaded.Conversations[0]; !got.Agent || got.Icon != icon || got.IconRevision != 2 {
		t.Fatalf("reload dropped the stored icon: %+v", got)
	}
}
