package chatui

import "testing"

// TestTodo_CHATBUG_071 holds the composer to one rule: a mentioned person is
// not an agent being asked. The draft keeps the person's reference for the
// send, and neither the agent chip nor the agent answer hint is drawn for it.
func TestTodo_CHATBUG_071(t *testing.T) {
	model := Model{SelectedID: "room", Conversations: []Conversation{{ID: "room", Name: "general", Kind: PublicChannel}}}
	person := ChatReference{Kind: "PERSON_MENTION", ID: "loretta", Display: "Loretta Haynes", ConversationID: "room"}
	mentions := mentionStore{box: &mentionBox{personas: []personaDraftMention{{Target: "chat-composer", ConversationID: "room", Start: 6, End: 21, Reference: person}}}}
	if name := mentions.PersonaDisplay("chat-composer", "room"); name != "" {
		t.Fatalf("a mentioned person was named as the agent being asked: %q", name)
	}
	if hint := mentionReplyHint(model, mentions, "chat-composer"); hint != "" {
		t.Fatalf("a mentioned person got the agent answer hint: %q", hint)
	}
	refs := mentions.PersonaReferences("chat-composer", "room", "hello @Loretta Haynes ")
	if len(refs) != 1 || refs[0].Kind != "PERSON_MENTION" || refs[0].ID != "loretta" {
		t.Fatalf("the person's reference was not kept for the send: %+v", refs)
	}
	agent := ChatReference{Kind: "AGENT_MENTION", TenantID: "t", ID: "policy", Display: "Policy Helper", ConversationID: "room"}
	mentions.box.personas = append(mentions.box.personas, personaDraftMention{Target: "chat-composer", ConversationID: "room", Reference: agent, Detached: true})
	if name := mentions.PersonaDisplay("chat-composer", "room"); name != "Policy Helper" {
		t.Fatalf("the agent being asked = %q, want Policy Helper", name)
	}
	mentions.box.personas = append(mentions.box.personas, personaDraftMention{Target: "chat-composer", ConversationID: "room", Start: 6, End: 21, Reference: person})
	if name := mentions.PersonaDisplay("chat-composer", "room"); name != "Policy Helper" {
		t.Fatalf("a person mentioned after the agent replaced it: %q", name)
	}
}
