package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// The copy of a private answer that is saved in the asker's conversation with
// the agent is stored as the agent's message, not the asker's, and its text
// ends in one labelled way back to the question, never a bare address. Real
// chat store and real private delivery from a question asked in a channel.
func TestTodo_AGENTUX_031_Integration(t *testing.T) {
	room := newAgentUX070Room(t, agentUX070Options{question: "@Policy Helper how many PTO hours carry over? Keep this private."})
	receipt, err := room.deliver(agentUX070Delivery{})
	room.assertPrivate(receipt, err, chat.PrivateReasonAsked)

	copies := room.directPosts()
	if len(copies) != 1 {
		t.Fatalf("%d messages in the asker's conversation with the agent, want the one saved answer", len(copies))
	}
	saved := copies[0]
	// The author is the conversation's other member, the agent; the asker wrote
	// nothing there.
	if saved.AuthorID != "policy-helper" || saved.AuthorID == room.asker.SubjectID || saved.ConversationID != room.direct || saved.ID != receipt.DurableCopyPostID {
		t.Fatalf("the saved answer is authored by %q in %q (receipt names %q)", saved.AuthorID, saved.ConversationID, receipt.DurableCopyPostID)
	}
	// The text: the answer, its sources, and one marked link back to the question.
	if !strings.Contains(saved.Body, "40 hours") || !strings.Contains(saved.Body, "\n\nSources\n- [Paid time off policy") {
		t.Fatalf("the saved answer lost its text or its sources: %s", saved.Body)
	}
	for _, bare := range []string{"Open the source conversation:", "\n/chat/share/", " /chat/share/", "The persona reply was sent privately", "chat.persona."} {
		if strings.Contains(saved.Body, bare) {
			t.Fatalf("the saved answer holds %q: %s", bare, saved.Body)
		}
	}
	if links := strings.Count(saved.Body, "](/chat/share/"); links != 1 || !strings.Contains(saved.Body, "[chat-agent-question") {
		t.Fatalf("the saved answer has %d ways back to the question, want one marked link: %s", links, saved.Body)
	}
	// The asker reads the same message through the service, as the agent's.
	listed, err := room.service.ListPosts(room.ctx, chat.ListPostsRequest{Principal: room.asker, TenantID: "tenant-a", ConversationID: room.direct, Page: chat.Page{PageSize: 20}})
	if err != nil || len(agentUX070Said(listed.Posts)) != 1 || agentUX070Said(listed.Posts)[0].AuthorID != "policy-helper" {
		t.Fatalf("the asker reads %+v %v", listed.Posts, err)
	}
	// Nothing was posted in the channel for it.
	if posts := room.channelPosts("owner"); len(posts) != 1 || posts[0].ID != room.question.ID {
		t.Fatalf("the channel holds %+v after a private answer", posts)
	}
}
