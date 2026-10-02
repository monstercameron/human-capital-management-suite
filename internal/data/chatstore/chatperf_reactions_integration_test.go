package chatstore

import (
	"context"
	"errors"
	"sort"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestTodo_CHATBUG_014_Integration: one query answers what a read per post
// answered, for the posts of one conversation only, to a current member only.
func TestTodo_CHATBUG_014_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	member := func(conversation, subject string, role chat.MembershipRole) chat.Membership {
		return chat.Membership{ConversationID: conversation, TenantID: "host", HomeTenantID: "host", SubjectID: subject, Role: role, HistoryVisibility: chat.FullHistory}
	}
	room := chat.Conversation{ID: "reaction-batch", TenantID: "host", Kind: chat.PrivateChannel, OwnerID: "sam", Revision: 1}
	if _, err := s.CreateConversation(ctx, room, []chat.Membership{member(room.ID, "sam", chat.Manager), member(room.ID, "ann", chat.Member)}, ""); err != nil {
		t.Fatal(err)
	}
	elsewhere := chat.Conversation{ID: "reaction-batch-other", TenantID: "host", Kind: chat.PrivateChannel, OwnerID: "sam", Revision: 1}
	if _, err := s.CreateConversation(ctx, elsewhere, []chat.Membership{member(elsewhere.ID, "sam", chat.Manager)}, ""); err != nil {
		t.Fatal(err)
	}
	post := func(conversation, key string) string {
		t.Helper()
		p, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: "host", ConversationID: conversation, IdempotencyKey: key}, chat.Post{AuthorID: "sam", Body: key})
		if err != nil {
			t.Fatal(err)
		}
		return p.ID
	}
	react := func(conversation, postID, subject, emoji string) {
		t.Helper()
		if _, err := s.PutReaction(ctx, chat.Reaction{TenantID: "host", ConversationID: conversation, PostID: postID, HomeTenantID: "host", SubjectID: subject, Emoji: emoji}); err != nil {
			t.Fatal(err)
		}
	}
	first, second, quiet := post(room.ID, "first"), post(room.ID, "second"), post(room.ID, "quiet")
	foreign := post(elsewhere.ID, "foreign")
	react(room.ID, first, "sam", "+1")
	react(room.ID, first, "ann", "+1")
	react(room.ID, first, "ann", "heart")
	react(room.ID, second, "sam", "+1")
	react(elsewhere.ID, foreign, "sam", "+1")

	sam := chat.Principal{TenantID: "host", SubjectID: "sam"}
	key := func(x chat.Reaction) string {
		return x.PostID + "|" + x.Emoji + "|" + x.HomeTenantID + "|" + x.SubjectID
	}
	keys := func(reactions []chat.Reaction) []string {
		out := make([]string, 0, len(reactions))
		for _, x := range reactions {
			if x.TenantID != "host" || x.ConversationID != room.ID {
				t.Fatalf("a reaction came back addressed to %s/%s", x.TenantID, x.ConversationID)
			}
			out = append(out, key(x))
		}
		sort.Strings(out)
		return out
	}

	// The batch answers exactly what the post-by-post reads answer. A post of
	// another conversation and an unknown post contribute nothing.
	asked := []string{first, second, quiet, foreign, "no-such-post"}
	batch, err := s.ListReactionsForPosts(ctx, sam, "host", room.ID, asked, 200)
	if err != nil {
		t.Fatal(err)
	}
	var single []chat.Reaction
	for _, id := range asked {
		page, err := s.ListReactions(ctx, sam, "host", room.ID, id, chat.Page{})
		if err != nil {
			t.Fatal(err)
		}
		single = append(single, page.Reactions...)
	}
	got, want := keys(batch), keys(single)
	if len(got) != 4 || len(want) != 4 {
		t.Fatalf("batch=%v single=%v, want the four reactions of this conversation", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("batch=%v single=%v", got, want)
		}
	}

	// Each post is bounded on its own.
	bounded, err := s.ListReactionsForPosts(ctx, sam, "host", room.ID, []string{first, second}, 1)
	if err != nil {
		t.Fatal(err)
	}
	perPost := map[string]int{}
	for _, x := range bounded {
		perPost[x.PostID]++
	}
	if len(bounded) != 2 || perPost[first] != 1 || perPost[second] != 1 {
		t.Fatalf("a bound of one per post returned %v", perPost)
	}

	// Someone who is not a member of the conversation reads nothing, and neither
	// does a member of it reading through another conversation's address.
	for name, read := range map[string]func() ([]chat.Reaction, error){
		"a non-member": func() ([]chat.Reaction, error) {
			return s.ListReactionsForPosts(ctx, chat.Principal{TenantID: "host", SubjectID: "zed"}, "host", room.ID, asked[:3], 200)
		},
		"another conversation": func() ([]chat.Reaction, error) {
			return s.ListReactionsForPosts(ctx, sam, "host", elsewhere.ID, asked[:3], 200)
		},
		"no posts": func() ([]chat.Reaction, error) { return s.ListReactionsForPosts(ctx, sam, "host", room.ID, nil, 200) },
	} {
		reactions, err := read()
		if err != nil || len(reactions) != 0 {
			t.Fatalf("%s read %d reactions, err=%v", name, len(reactions), err)
		}
	}

	// The bounds are the store's own, not only the service's.
	tooMany := make([]string, chat.ReactionBatchLimit+1)
	for name, read := range map[string]func() ([]chat.Reaction, error){
		"too many posts": func() ([]chat.Reaction, error) {
			return s.ListReactionsForPosts(ctx, sam, "host", room.ID, tooMany, 200)
		},
		"no per-post bound": func() ([]chat.Reaction, error) {
			return s.ListReactionsForPosts(ctx, sam, "host", room.ID, asked[:1], 0)
		},
		"an unbounded page": func() ([]chat.Reaction, error) {
			return s.ListReactionsForPosts(ctx, sam, "host", room.ID, asked[:1], 201)
		},
	} {
		if _, err := read(); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
