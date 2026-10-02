package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// agentDirectResolver is the reference resolver as the registry answers it: one
// agent identity is known, anybody else is a person.
type agentDirectResolver struct {
	agent string
	calls int
	err   error
}

func (r *agentDirectResolver) ResolvePersonaMentions(_ context.Context, _, _ string, references []chatcore.Reference) ([]agentinvoke.Mention, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	var mentions []agentinvoke.Mention
	for _, reference := range references {
		if reference.Kind != chatcore.AgentMention {
			continue
		}
		if reference.ID != r.agent {
			return nil, errPersonaReferenceNotPersona
		}
		mentions = append(mentions, agentinvoke.Mention{Kind: agentinvoke.PersonaMention, PersonaID: r.agent, Canonical: true})
	}
	return mentions, nil
}

func agentDirectFixture(t *testing.T, other string) (*personaChatInvocation, *personaChatWriterFake, *agentDirectResolver, *personaRunFake, *personaFailureFake, context.Context) {
	t.Helper()
	service, chat, _, runs, _, failures, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), nil)
	resolver := &agentDirectResolver{agent: "persona-comp"}
	service.refs = resolver
	joined := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	chat.room = chatcore.Conversation{ID: "channel-a", TenantID: "tenant-a", Kind: chatcore.Direct}
	chat.members = []chatcore.Membership{
		{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "channel-a", SubjectID: "alice", JoinedAt: &joined},
		{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "channel-a", SubjectID: other, JoinedAt: &joined},
	}
	return service, chat, resolver, runs, failures, ctx
}

func agentDirectSend(t *testing.T, service *personaChatInvocation, chat *personaChatWriterFake, ctx context.Context, id, body string) {
	t.Helper()
	request := personaSendRequest()
	request.Body, request.References, request.IdempotencyKey = body, nil, "key-"+id
	chat.post = chatcore.Post{ID: id, TenantID: "tenant-a", ConversationID: "channel-a", AuthorID: "alice", Body: body, Revision: 1}
	if post, err := service.SendPost(ctx, request); err != nil || post.ID != id {
		t.Fatalf("send %s: %+v %v", id, post, err)
	}
}

// Two people talking in a direct conversation ask no agent: no failure is
// recorded for any of their messages, and after the first one the conversation
// is not looked up again.
func TestAgentUX_DirectConversationWithoutAgent(t *testing.T) {
	for _, other := range []string{"bob", "bob@example.com"} {
		service, chat, resolver, runs, failures, ctx := agentDirectFixture(t, other)
		for index, id := range []string{"post-1", "post-2", "post-3"} {
			agentDirectSend(t, service, chat, ctx, id, "See you at ten.")
			if len(failures.posts) != 0 || len(runs.requests) != 0 {
				t.Fatalf("%s, message %d: %d failures recorded (%v), %d runs started", other, index+1, len(failures.posts), failures.errs, len(runs.requests))
			}
		}
		// A person's identifier that no agent could have is never looked up; any
		// other is looked up once, for the first message.
		want := 1
		if other == "bob@example.com" {
			want = 0
		}
		if resolver.calls != want {
			t.Fatalf("%s: the other member was looked up %d times for three messages, want %d", other, resolver.calls, want)
		}
		// The memory lapses: the conversation is looked at again later.
		if !service.quiet.known("tenant-a", "channel-a", time.Now()) || service.quiet.known("tenant-a", "channel-a", time.Now().Add(personaDirectQuietTTL+time.Second)) || service.quiet.known("tenant-b", "channel-a", time.Now()) {
			t.Fatalf("%s: the conversation is not remembered for its tenant and for a while only", other)
		}
	}
}

// The other side: a person's own conversation with an agent still asks it with
// no mention typed, a message that names an agent is still resolved in any
// conversation, and a real failure of the lookup is still recorded.
func TestAgentUX_DirectConversationWithoutAgent_Security(t *testing.T) {
	service, chat, resolver, runs, failures, ctx := agentDirectFixture(t, "persona-comp")
	agentDirectSend(t, service, chat, ctx, "post-1", "Summarize the policy")
	agentDirectSend(t, service, chat, ctx, "post-2", "And the holidays?")
	if resolver.calls != 2 || len(runs.requests) != 2 || len(failures.posts) != 0 || service.quiet.known("tenant-a", "channel-a", time.Now()) {
		t.Fatalf("the agent's own conversation: %d lookups, %d runs, %d failures", resolver.calls, len(runs.requests), len(failures.posts))
	}

	// Two people, and one of them names an agent that is not there: that is a
	// question to an agent, and its refusal is recorded as before.
	service, chat, resolver, runs, failures, ctx = agentDirectFixture(t, "bob")
	agentDirectSend(t, service, chat, ctx, "post-1", "See you at ten.")
	request := personaSendRequest()
	request.References = []chatcore.Reference{{Kind: chatcore.AgentMention, TenantID: "tenant-a", ID: "not-an-agent", Display: "Somebody"}}
	chat.post = chatcore.Post{ID: "post-2", TenantID: "tenant-a", ConversationID: "channel-a", AuthorID: "alice", Body: request.Body, Revision: 1, References: request.References}
	if _, err := service.SendPost(ctx, request); err != nil {
		t.Fatal(err)
	}
	if resolver.calls < 2 || len(runs.requests) != 0 || len(failures.posts) != 1 || failures.posts[0] != "post-2" || !errors.Is(failures.errs[0], errPersonaReferenceNotPersona) {
		t.Fatalf("a typed mention in a quiet conversation: %d lookups, %d runs, failures %v %v", resolver.calls, len(runs.requests), failures.posts, failures.errs)
	}

	// The registry is down: that is not "no agent here". It is recorded, and the
	// conversation is not remembered as quiet.
	service, chat, resolver, _, failures, ctx = agentDirectFixture(t, "bob")
	resolver.err = errors.New("registry is down")
	agentDirectSend(t, service, chat, ctx, "post-1", "See you at ten.")
	if len(failures.posts) != 1 || service.quiet.known("tenant-a", "channel-a", time.Now()) {
		t.Fatalf("a failed lookup: %d failures recorded, quiet=%v", len(failures.posts), service.quiet.known("tenant-a", "channel-a", time.Now()))
	}

	// A channel message that names nobody is not resolved at all.
	service, chat, resolver, runs, failures, ctx = agentDirectFixture(t, "bob")
	chat.room.Kind = chatcore.PublicChannel
	agentDirectSend(t, service, chat, ctx, "post-1", "Good morning.")
	if resolver.calls != 0 || len(runs.requests) != 0 || len(failures.posts) != 0 {
		t.Fatalf("a channel message with no mention: %d lookups, %d runs, %d failures", resolver.calls, len(runs.requests), len(failures.posts))
	}

	// The memory itself: nil remembers nothing, and it starts over past its limit.
	var none *personaDirectQuiet
	none.remember("tenant-a", "room", time.Now())
	if none.known("tenant-a", "room", time.Now()) {
		t.Fatal("a nil memory remembered")
	}
	memory := &personaDirectQuiet{until: make(map[string]time.Time, personaDirectQuietLimit)}
	now := time.Now()
	for index := 0; index < personaDirectQuietLimit; index++ {
		memory.until["tenant-a\x00room-"+time.Duration(index).String()] = now.Add(time.Minute)
	}
	memory.remember("tenant-a", "one-more", now)
	if len(memory.until) != 1 || !memory.known("tenant-a", "one-more", now) {
		t.Fatalf("the memory holds %d conversations past its limit", len(memory.until))
	}
}
