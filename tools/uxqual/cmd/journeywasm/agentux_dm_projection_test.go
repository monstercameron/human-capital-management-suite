package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestTodo_AGENTUX_030_Browser(t *testing.T) {
	model := chatui.Model{
		Conversations: []chatui.Conversation{{ID: "direct", Name: "673214ec-4402", Kind: chatui.DirectMessage}},
		Sections:      []chatui.SidebarSection{{ID: "direct", Chats: []chatui.Conversation{{ID: "direct", Name: "673214ec-4402", Kind: chatui.DirectMessage}}}},
		Members:       []chatui.Member{{ID: "policy-helper", Name: "policy-helper"}},
	}
	if !applyAgentDirectConversation(&model, "direct", "policy-helper", "Policy Helper") {
		t.Fatal("agent direct projection was not adopted")
	}
	if conversation := model.Conversations[0]; conversation.Name != "Policy Helper" || !conversation.Agent || conversation.AgentID != "policy-helper" {
		t.Fatalf("conversation=%+v", conversation)
	}
	if row := model.Sections[0].Chats[0]; row.Name != "Policy Helper" || !row.Agent || row.AgentID != "policy-helper" {
		t.Fatalf("rail row=%+v", row)
	}
	if member := model.Members[0]; member.Name != "Policy Helper" || !member.Agent {
		t.Fatalf("member=%+v", member)
	}
	model.SelectedID = "direct"
	if title := agentDirectConversationTitle("Chat · Ironridge", model); title != "Policy Helper · Chat · Ironridge" {
		t.Fatalf("browser title=%q", title)
	}
}
