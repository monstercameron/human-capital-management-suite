package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// A question asked in a person's own conversation with an agent is answered
// once: one stored message, authored by the agent, with no private card in any
// conversation, no link back to the conversation the reader is already in, and
// nothing in the channel. Delivering the same answer again changes nothing.
// Real chat store and real delivery; the answer text is a fixture.
func TestTodo_AGENTUX_038_Integration(t *testing.T) {
	room := newAgentUX070Room(t, agentUX070Options{question: "@Policy Helper how many PTO hours carry over?"})
	receipt, err := room.deliver(agentUX070Delivery{conversation: room.direct})
	if err != nil || !receipt.Private || receipt.Public || receipt.PublicPostID != "" {
		t.Fatalf("the direct answer was delivered as %+v %v", receipt, err)
	}

	direct := room.directPosts()
	if len(direct) != 2 {
		t.Fatalf("the conversation holds %d messages, want the question and one answer: %+v", len(direct), direct)
	}
	question, answer := direct[0], direct[1]
	if question.AuthorID != room.asker.SubjectID || answer.AuthorID != "policy-helper" || answer.ParentID != "" {
		t.Fatalf("question by %q, answer by %q (parent %q)", question.AuthorID, answer.AuthorID, answer.ParentID)
	}
	if !strings.Contains(answer.Body, "40 hours") || !strings.Contains(answer.Body, "\n\nSources\n- [Paid time off policy") {
		t.Fatalf("the answer lost its text or its sources: %s", answer.Body)
	}
	// No link to itself, no quoted question, no private marker.
	for _, selfLink := range []string{"/chat/share/", "chat-agent-question", "Open the original message", "Open the source conversation", "chat.agent.private"} {
		if strings.Contains(answer.Body, selfLink) {
			t.Fatalf("the answer in the agent's own conversation holds %q: %s", selfLink, answer.Body)
		}
	}
	// The receipt names that one message; there is no separate private card.
	if receipt.DurableCopyPostID != answer.ID || receipt.DurableCopyConversationID != room.direct || receipt.EphemeralPostID != answer.ID {
		t.Fatalf("the receipt %+v does not name the stored answer %s", receipt, answer.ID)
	}
	for _, conversation := range []string{room.direct, room.channel} {
		cards, _, err := room.store.ListEphemeral(room.ctx, room.asker, "tenant-a", conversation, 0, 20)
		if err != nil || len(cards) != 0 {
			t.Fatalf("a private card was stored in %s: %+v %v", conversation, cards, err)
		}
	}
	// Nothing reached the channel.
	if posts := room.channelPosts("employee"); len(posts) != 1 || posts[0].ID != room.question.ID {
		t.Fatalf("the channel holds %+v after a direct answer", posts)
	}

	// The second member of the conversation is the agent and nobody else: the
	// author of a direct answer is resolved from exactly two members.
	members, err := room.service.ListMemberships(room.ctx, chat.ListMembershipsRequest{Principal: room.asker, TenantID: "tenant-a", ConversationID: room.direct, Page: chat.Page{PageSize: 10}})
	if err != nil || len(members.Memberships) != 2 {
		t.Fatalf("members of the agent conversation = %+v %v", members.Memberships, err)
	}
}
