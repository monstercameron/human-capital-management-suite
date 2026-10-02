package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// agentUX075Chat is a real chat service over PostgreSQL with one channel (the
// asker and a colleague, no agent) and the asker's own conversation with an
// agent, created with both its members as the product creates it.
type agentUX075Chat struct {
	ctx     context.Context
	store   *chatstore.Adapter
	service *chat.Service
	asker   chat.Principal
	direct  string
	channel string
}

func newAgentUX075Chat(t *testing.T) *agentUX075Chat {
	t.Helper()
	db := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, db)
	raw, err := chatstore.New(context.Background(), chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	store := chatstore.NewAdapter(raw)
	t.Cleanup(store.Close)
	now := time.Now().UTC()
	service := chat.NewService(store, func() time.Time { return now })
	service.SetAuthority(servedPersonaChatAuthority{})
	room := &agentUX075Chat{store: store, service: service, asker: chat.Principal{TenantID: "tenant-a", SubjectID: "owner"}, direct: "owner-assistant", channel: "general"}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "owner", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "agentux075", CredentialDigest: "agentux075", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	room.ctx = trust.WithPrincipal(context.Background(), principal)
	if _, err := service.CreateConversation(room.ctx, chat.CreateConversationRequest{Principal: room.asker, TenantID: "tenant-a", ConversationID: room.channel, Kind: chat.PublicChannel, Name: "general"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateConversation(room.ctx, chat.CreateConversationRequest{Principal: room.asker, TenantID: "tenant-a", ConversationID: room.direct, Kind: chat.Direct, Name: "Assistant", Members: []chat.MemberRef{{TenantID: "tenant-a", SubjectID: "assistant"}}}); err != nil {
		t.Fatal(err)
	}
	return room
}

func (r *agentUX075Chat) ask(t *testing.T, conversation, key, body string) chat.Post {
	t.Helper()
	post, err := r.service.SendPost(r.ctx, chat.SendPostRequest{Principal: r.asker, TenantID: "tenant-a", ConversationID: conversation, Body: body, IdempotencyKey: key})
	if err != nil {
		t.Fatalf("posting the question in %s: %v", conversation, err)
	}
	return post
}

func (r *agentUX075Chat) react(conversation, post, agent, emoji, replaces string) error {
	_, err := r.service.CommitAgentQuestionReaction(r.ctx, chat.AgentQuestionReaction{TenantID: "tenant-a", ConversationID: conversation, PostID: post, AgentSubjectID: agent, Asker: r.asker, Emoji: emoji, Replaces: replaces})
	return err
}

func (r *agentUX075Chat) reactions(t *testing.T, conversation, post string) map[string]bool {
	t.Helper()
	listed, err := r.store.ListReactions(r.ctx, r.asker, "tenant-a", conversation, post, chat.Page{PageSize: 50})
	if err != nil {
		t.Fatalf("listing reactions: %v", err)
	}
	out := map[string]bool{}
	for _, reaction := range listed.Reactions {
		out[reaction.SubjectID+" "+reaction.Emoji] = true
	}
	return out
}

// The agent's reaction to a question lands through the real chat service and
// store, in the person's own conversation with the agent (where the agent is a
// member) and in a channel where it is not one, and its outcome reaction takes
// the first one's place. The live server refused the one in the direct
// conversation; the cause was the route lease (see the chatroutingadapter test),
// and this proves the store side with the agent as member and as non-member.
func TestTodo_AGENTUX_075_ReactionIntegration(t *testing.T) {
	room := newAgentUX075Chat(t)

	t.Run("direct conversation", func(t *testing.T) {
		question := room.ask(t, room.direct, "agentux075-direct", "What can you help me with here?")
		if err := room.react(room.direct, question.ID, "assistant", "👀", ""); err != nil {
			t.Fatalf("the agent's reaction in its own conversation: %v", err)
		}
		if got := room.reactions(t, room.direct, question.ID); !got["assistant 👀"] || len(got) != 1 {
			t.Fatalf("reactions after the first: %v", got)
		}
		if err := room.react(room.direct, question.ID, "assistant", chat.AgentOutcomeAnswered(), "👀"); err != nil {
			t.Fatalf("the outcome reaction: %v", err)
		}
		if got := room.reactions(t, room.direct, question.ID); !got["assistant ✅"] || got["assistant 👀"] || len(got) != 1 {
			t.Fatalf("reactions after the outcome: %v, want only the agent's ✅", got)
		}
	})

	t.Run("channel where the agent is not a member", func(t *testing.T) {
		question := room.ask(t, room.channel, "agentux075-channel", "@Assistant list all the holidays")
		if err := room.react(room.channel, question.ID, "assistant", "🔎", ""); err != nil {
			t.Fatalf("the agent's reaction in a channel it is not a member of: %v", err)
		}
		if got := room.reactions(t, room.channel, question.ID); !got["assistant 🔎"] {
			t.Fatalf("reactions in the channel: %v", got)
		}
		if err := room.react(room.channel, question.ID, "assistant", chat.AgentOutcomeFailed(), "🔎"); err != nil {
			t.Fatalf("the failure reaction: %v", err)
		}
		if got := room.reactions(t, room.channel, question.ID); !got["assistant "+chat.AgentOutcomeFailed()] || got["assistant 🔎"] {
			t.Fatalf("reactions after the failure: %v", got)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		question := room.ask(t, room.direct, "agentux075-other", "another question")
		if _, err := room.service.CommitAgentQuestionReaction(room.ctx, chat.AgentQuestionReaction{TenantID: "tenant-a", ConversationID: room.direct, PostID: question.ID, AgentSubjectID: "assistant", Asker: chat.Principal{TenantID: "tenant-a", SubjectID: "someone-else"}, Emoji: "👀"}); err == nil {
			t.Error("a reaction to a message the asker did not write was stored")
		}
		if err := room.react(room.direct, question.ID, "assistant", "💣", ""); err == nil {
			t.Error("an emoji outside the set was stored")
		}
		if err := room.react(room.direct, "no-such-post", "assistant", "👀", ""); err == nil {
			t.Error("a reaction to a message that does not exist was stored")
		}
		if got := room.reactions(t, room.direct, question.ID); len(got) != 0 {
			t.Errorf("a refused reaction left %v", got)
		}
	})
}
