package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// An identifier is not a name: the projection must not adopt one for an agent
// conversation, and the row keeps its neutral placeholder until a name exists.
func TestTodo_CHATBUG_011(t *testing.T) {
	id := "673214ec-4402-5f09-9a5c-000000000001"
	model := chatui.Model{Conversations: []chatui.Conversation{{ID: id, Name: id, Kind: chatui.DirectMessage}}}
	if applyAgentDirectConversation(&model, id, "policy-helper", id) || applyAgentDirectConversation(&model, id, "policy-helper", "policy-helper") {
		t.Fatal("an identifier was adopted as the agent conversation's name")
	}
	if conversation := model.Conversations[0]; conversation.Agent || conversation.Name != id {
		t.Fatalf("conversation changed: %+v", conversation)
	}
	if !applyAgentDirectConversation(&model, id, "policy-helper", "Policy Helper") || model.Conversations[0].Name != "Policy Helper" || !model.Conversations[0].Agent {
		t.Fatalf("a real name was not adopted: %+v", model.Conversations[0])
	}
}
