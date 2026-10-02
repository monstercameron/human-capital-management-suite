package chat

import "testing"

func TestAgentUXAmbient_Invalidation_Security(t *testing.T) {
	if !AgentUXAmbientInvalidation(ConversationEvent{Kind: AgentUXAmbientOffersChanged, Revision: 2}) {
		t.Fatal("content-free invalidation refused")
	}
	for _, e := range []ConversationEvent{
		{Kind: PostCreated},
		{Kind: AgentUXAmbientOffersChanged, Post: &Post{Body: "private card"}},
		{Kind: AgentUXAmbientOffersChanged, Conversation: &Conversation{}},
		{Kind: AgentUXAmbientOffersChanged, Membership: &Membership{}},
		{Kind: AgentUXAmbientOffersChanged, Reaction: &Reaction{}},
		{Kind: AgentUXAmbientOffersChanged, Pin: &Pin{}},
	} {
		if AgentUXAmbientInvalidation(e) {
			t.Fatalf("payload-bearing or unrelated event accepted: %+v", e)
		}
	}
}
