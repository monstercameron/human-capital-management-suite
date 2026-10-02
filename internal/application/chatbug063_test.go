package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
)

// chatbug063Writer is the classified human post writer as the coordinator sees
// it: the post is committed, and its classification may fail afterwards.
type chatbug063Writer struct {
	post    chat.Post
	fail    error
	room    chat.Conversation
	members []chat.Membership
}

func (w *chatbug063Writer) SendPost(context.Context, chat.SendPostRequest) (chat.Post, error) {
	if w.fail != nil {
		return w.post, &PersonaHumanPostClassificationError{PostID: w.post.ID, Cause: w.fail}
	}
	return w.post, nil
}

func (w *chatbug063Writer) GetConversation(context.Context, chat.GetConversationRequest) (chat.Conversation, error) {
	return w.room, nil
}

func (w *chatbug063Writer) ListMemberships(context.Context, chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error) {
	return chat.ListMembershipsResponse{Memberships: w.members}, nil
}

// chatbug063Resolver knows one agent, "agent:coach"; anybody else is a person.
type chatbug063Resolver struct{}

func (chatbug063Resolver) ResolvePersonaMentions(_ context.Context, _, _ string, references []chat.Reference) ([]agentinvoke.Mention, error) {
	var mentions []agentinvoke.Mention
	for _, reference := range references {
		if reference.Kind != chat.AgentMention {
			continue
		}
		if reference.ID != "agent:coach" {
			return nil, errPersonaReferenceNotPersona
		}
		mentions = append(mentions, agentinvoke.Mention{Kind: agentinvoke.PersonaMention, PersonaID: "coach", Canonical: true})
	}
	return mentions, nil
}

// A message that asked no agent never gets a stored agent failure, whatever
// went wrong after it was committed; a message that did ask one still does. And
// the activity the page reads never invents a name for an agent.
func TestTodo_CHATBUG_063(t *testing.T) {
	surface, ctx, _, invocations, execution := personaSurfaceFixture(t)
	joined := time.Date(2026, 10, 1, 22, 0, 0, 0, time.UTC)
	mention := chat.Reference{Kind: chat.AgentMention, TenantID: "tenant-a", ID: "agent:coach", ConversationID: "channel-a"}
	channel := chat.Conversation{ID: "channel-a", TenantID: "tenant-a", Kind: chat.PublicChannel}
	direct := chat.Conversation{ID: "channel-a", TenantID: "tenant-a", Kind: chat.Direct}
	member := func(subject string) chat.Membership {
		return chat.Membership{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "channel-a", SubjectID: subject, JoinedAt: &joined}
	}
	request := chat.SendPostRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "user-a"}, TenantID: "tenant-a", ConversationID: "channel-a", Body: "what the damn", IdempotencyKey: "chatbug063-key"}

	for _, tc := range []struct {
		name       string
		room       chat.Conversation
		members    []chat.Membership
		references []chat.Reference
		stored     int
	}{
		{name: "ordinary message in a channel", room: channel, stored: 0},
		{name: "message that mentions only a person", room: channel, references: []chat.Reference{{Kind: chat.PersonMention, TenantID: "tenant-a", ID: "coworker"}}, stored: 0},
		{name: "message to a person in a direct conversation", room: direct, members: []chat.Membership{member("user-a"), member("coworker")}, stored: 0},
		{name: "question that mentions an agent", room: channel, references: []chat.Reference{mention}, stored: 1},
		{name: "question in the agent's own conversation", room: direct, members: []chat.Membership{member("user-a"), member("agent:coach")}, stored: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &personaSurfaceFailuresFixture{}
			logged := &personaFailureFake{}
			writer := &chatbug063Writer{fail: errors.New("classification store is down"), room: tc.room, members: tc.members,
				post: chat.Post{ID: "post-b", TenantID: "tenant-a", ConversationID: "channel-a", AuthorID: "user-a", AuthorHomeTenantID: "tenant-a", Body: request.Body, Revision: 1, References: tc.references}}
			coordinator := &personaChatInvocation{chat: writer, refs: chatbug063Resolver{}, failures: &personaDurableInvocationFailureSink{store: store, logger: logged}}
			post, err := coordinator.SendPost(ctx, request)
			if err != nil || post.ID != "post-b" {
				t.Fatalf("the committed message was not returned: %+v %v", post, err)
			}
			if store.writes != tc.stored {
				t.Fatalf("stored agent failures=%d, want %d: %+v", store.writes, tc.stored, store.failures)
			}
			if len(logged.posts) != 1 || logged.posts[0] != "post-b" {
				t.Fatalf("the cause was not logged exactly once: %v", logged.posts)
			}
		})
	}

	// A failure stored for a message with no run behind it is sent without a
	// made-up agent name, so the page can only name the agent from the message.
	invocations.rows = nil
	execution.err = agentrunstate.ErrNotFound
	surface.Failures = &personaSurfaceFailuresFixture{failures: []agentinvocationstore.PostFailure{{TenantID: "tenant-a", InvokerID: "user-a", PostID: "post-b", ConversationID: "channel-a", ThreadID: "post-b", Code: "INVOCATION_FAILED"}}}
	progress, err := surface.Progress(ctx, "channel-a")
	if err != nil || len(progress.Invocations) != 1 {
		t.Fatalf("progress=%+v %v", progress, err)
	}
	if name := progress.Invocations[0].AgentName; name != "" {
		t.Fatalf("an unknown agent is sent with the name %q", name)
	}
}
