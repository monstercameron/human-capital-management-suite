package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// agentUX027Counts is what the sidebar shows a person for the channel.
func agentUX027Counts(t *testing.T, room *agentUX070Room, subject string) (unread, mentions uint64) {
	t.Helper()
	counts, err := chatstore.NewRecipientStateStore(room.store.Store).ChatscaleSidebarCounts(room.ctx, "tenant-a", "tenant-a", subject, []string{room.channel})
	if err != nil {
		t.Fatal(err)
	}
	return counts[room.channel].Unread, counts[room.channel].Mentions
}

// A private answer leaves no trace for another member of the channel: no post,
// no thread reply under the question, no change to their unread or mention
// counts, nothing their search can find, no private card, and no wording that
// says a private answer exists. Real chat store, real private delivery.
func TestTodo_AGENTUX_027_Security(t *testing.T) {
	room := newAgentUX070Room(t, agentUX070Options{question: "@Policy Helper how many PTO hours carry over? Keep this private."})
	employee := chat.Principal{TenantID: "tenant-a", SubjectID: "employee"}
	search := func(who chat.Principal, query string) []chat.SearchResult {
		t.Helper()
		found, err := room.store.Search(room.ctx, chat.SearchRequest{Principal: who, TenantID: "tenant-a", Query: query, SkipChannels: true, Page: chat.Page{PageSize: 20}})
		if err != nil {
			t.Fatalf("search %q as %s: %v", query, who.SubjectID, err)
		}
		return found.Results
	}
	unreadBefore, mentionsBefore := agentUX027Counts(t, room, "employee")

	receipt, err := room.deliver(agentUX070Delivery{})
	card := room.assertPrivate(receipt, err, chat.PrivateReasonAsked)

	// The colleague's channel: the question and nothing else, with no reply.
	posts := room.channelPosts("employee")
	if len(posts) != 1 || posts[0].ID != room.question.ID {
		t.Fatalf("the colleague's channel holds %+v", posts)
	}
	for _, post := range posts {
		if post.ParentID != "" {
			t.Fatalf("a reply stands under the question for the colleague: %+v", post)
		}
		lowered := strings.ToLower(post.Body)
		for _, trace := range []string{"sent privately", "private reply", "persona", "only visible", "chat.persona", "chat.agent"} {
			if strings.Contains(lowered, trace) {
				t.Fatalf("the colleague reads %q in the channel: %s", trace, post.Body)
			}
		}
	}
	// Their counts did not move.
	if unread, mentions := agentUX027Counts(t, room, "employee"); unread != unreadBefore || mentions != mentionsBefore {
		t.Fatalf("the colleague's counts moved from %d/%d to %d/%d", unreadBefore, mentionsBefore, unread, mentions)
	}
	// Their search finds nothing of the answer, by its words or its source; the
	// asker's own search finds the saved copy, so the words are searchable at all.
	for _, query := range []string{"unused PTO", "40 hours", "Paid time off policy"} {
		for _, hit := range search(employee, query) {
			if hit.Post.ID != room.question.ID {
				t.Fatalf("the colleague's search for %q found %+v", query, hit.Post)
			}
		}
	}
	if own := search(room.asker, "unused PTO"); len(own) != 1 || own[0].Post.ConversationID != room.direct {
		t.Fatalf("the asker's own search = %+v, want the copy in their conversation with the agent", own)
	}
	// The saved copy is in a conversation the colleague is not in: the store
	// hands them none of its posts. (This room's chat authority is a fixture that
	// treats everyone as a member, so the service's own refusal is proven by the
	// chat service's tests, not here.)
	if listed, err := room.store.ListPosts(room.ctx, employee, "tenant-a", room.direct, 0, chat.Page{PageSize: 20}, chat.PostWindow{}); err == nil && len(listed.Posts) != 0 {
		t.Fatalf("the colleague read the asker's conversation with the agent: %+v", listed.Posts)
	}
	// The private card is the asker's alone, also through the service.
	if cards, _, err := room.service.ListEphemeralPosts(room.ctx, chat.ListEphemeralPostsRequest{Principal: employee, TenantID: "tenant-a", ConversationID: room.channel, PageSize: 20}); err != nil || len(cards) != 0 {
		t.Fatalf("the colleague was delivered a private card: %+v %v", cards, err)
	}
	// Nothing the asker reads calls the agent a persona either.
	direct := room.directPosts()
	for _, text := range []string{card.Body, direct[0].Body} {
		if strings.Contains(strings.ToLower(text), "persona") {
			t.Fatalf("the answer uses the word persona: %s", text)
		}
	}
}
