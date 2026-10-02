package chatstore

import (
	"context"
	"errors"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// An agent's reaction to the question it was asked is a write to a placed
// conversation: refused without the route lease (the live server's failure, "chat
// write requires a route lease"), stored with it, by an agent that is not a member
// of the conversation, and taken away again by that agent alone (AGENTUX-075).
func TestTodo_AGENTUX_075_AgentReactionNeedsLeaseNotMembership(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "placed-agent", TenantID: "host", Kind: chat.PublicChannel, OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	good := leased(ctx, c.TenantID, c.ID, "chat-local", 1)
	if _, err := s.CreateConversation(good, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	send := chat.SendPostRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "agent-question"}
	post, err := s.SendPost(good, send, chat.Post{AuthorID: "alice", Body: "list all the holidays"})
	if err != nil {
		t.Fatal(err)
	}
	reaction := chat.Reaction{TenantID: c.TenantID, ConversationID: c.ID, PostID: post.ID, SubjectID: "assistant", HomeTenantID: c.TenantID, Emoji: "🔎"}

	if _, err := s.PutAgentQuestionReaction(ctx, reaction); !errors.Is(err, ErrNoRouteLease) {
		t.Fatalf("an unleased agent reaction = %v, want ErrNoRouteLease", err)
	}
	// The agent is not a member of the conversation: the lease is what it needs.
	if _, err := s.PutReaction(good, reaction); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("the member path for a non-member = %v, want a refusal", err)
	}
	stored, err := s.PutAgentQuestionReaction(good, reaction)
	if err != nil || stored.Emoji != "🔎" {
		t.Fatalf("the leased agent reaction: %+v %v", stored, err)
	}
	listed, err := s.ListReactions(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, c.TenantID, c.ID, post.ID, chat.Page{PageSize: 10})
	if err != nil || len(listed.Reactions) != 1 || listed.Reactions[0].SubjectID != "assistant" {
		t.Fatalf("the asker sees %+v %v, want the agent's reaction", listed.Reactions, err)
	}
	// A post that is not in this conversation, or does not exist, takes nothing.
	missing := reaction
	missing.PostID = "no-such-post"
	if _, err := s.PutAgentQuestionReaction(good, missing); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("a reaction to a message that does not exist = %v", err)
	}
	// Taking it away needs the lease too, and removes only the agent's own row.
	if err := s.RemoveAgentQuestionReaction(ctx, c.TenantID, c.ID, post.ID, c.TenantID, "assistant", "🔎"); !errors.Is(err, ErrNoRouteLease) {
		t.Fatalf("an unleased removal = %v, want ErrNoRouteLease", err)
	}
	if err := s.RemoveAgentQuestionReaction(good, c.TenantID, c.ID, post.ID, c.TenantID, "someone-else", "🔎"); err != nil {
		t.Fatal(err)
	}
	if listed, _ := s.ListReactions(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, c.TenantID, c.ID, post.ID, chat.Page{PageSize: 10}); len(listed.Reactions) != 1 {
		t.Fatalf("another subject's removal took the agent's reaction: %+v", listed.Reactions)
	}
	if err := s.RemoveAgentQuestionReaction(good, c.TenantID, c.ID, post.ID, c.TenantID, "assistant", "🔎"); err != nil {
		t.Fatal(err)
	}
	if listed, _ := s.ListReactions(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, c.TenantID, c.ID, post.ID, chat.Page{PageSize: 10}); len(listed.Reactions) != 0 {
		t.Fatalf("the agent's reaction was not taken away: %+v", listed.Reactions)
	}
}
