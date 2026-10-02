package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// agent027Direct makes the fixture's conversation the person's own direct
// conversation with the agent.
func agent027Direct(chat *personaChatWriterFake) {
	joined := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	chat.room = chatcore.Conversation{ID: "channel-a", TenantID: "tenant-a", Kind: chatcore.Direct}
	chat.members = []chatcore.Membership{
		{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "channel-a", SubjectID: "alice", JoinedAt: &joined},
		{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "channel-a", SubjectID: "persona-comp", JoinedAt: &joined},
	}
}

func agent027Ask(body string, references ...chatcore.Reference) chatcore.SendPostRequest {
	request := personaSendRequest()
	request.Body, request.References = body, references
	return request
}

var agent027Agent = chatcore.Reference{Kind: chatcore.AgentMention, TenantID: "tenant-a", ID: "persona-comp", Display: "Comp Analyst"}

// TestTodo_AGENT_027 sends a question to an owned agent the three ways Chat
// allows: a typed mention, a plain message in the person's direct conversation
// with the agent, and the "/ask" command. Each commits one message written by
// the person and starts exactly one run for them; sending it again starts no
// second run. The run starter here records the request and runs nothing.
func TestTodo_AGENT_027(t *testing.T) {
	for name, tc := range map[string]struct {
		direct   bool
		request  chatcore.SendPostRequest
		wantBody string
	}{
		"typed mention":           {request: agent027Ask("@Comp Analyst summarize the public policy", agent027Agent), wantBody: "@Comp Analyst summarize the public policy"},
		"direct message":          {direct: true, request: agent027Ask("summarize the public policy"), wantBody: "summarize the public policy"},
		"ask command":             {request: agent027Ask("/ask @Comp Analyst summarize the public policy", agent027Agent), wantBody: "@Comp Analyst summarize the public policy"},
		"ask command, any case":   {request: agent027Ask("  /Ask   @Comp Analyst what's \"carry over\" = how many days?", agent027Agent), wantBody: "@Comp Analyst what's \"carry over\" = how many days?"},
		"ask command in a direct": {direct: true, request: agent027Ask("/ask summarize the public policy"), wantBody: "summarize the public policy"},
	} {
		t.Run(name, func(t *testing.T) {
			service, chat, _, runs, grants, failures, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), nil)
			if tc.direct {
				agent027Direct(chat)
			}
			post, err := service.SendPost(ctx, tc.request)
			if err != nil || post.ID != "post-1" || chat.calls != 1 {
				t.Fatalf("post=%+v err=%v durable sends=%d", post, err, chat.calls)
			}
			// The message is the person's own, in the words a mention sends:
			// the command word is not posted.
			if post.AuthorID != "alice" || post.Body != tc.wantBody || strings.Contains(post.Body, "/ask") {
				t.Fatalf("committed message = %q by %q, want %q by alice", post.Body, post.AuthorID, tc.wantBody)
			}
			if grants.calls != 1 || len(runs.requests) != 1 || len(failures.errs) != 0 {
				t.Fatalf("grants=%d runs=%d failures=%v, want one grant and one run", grants.calls, len(runs.requests), failures.errs)
			}
			run := runs.requests[0]
			if run.Mode != agentinvoke.OnBehalfOf || run.InvokerID != "alice" || run.InvokingPostID != post.ID || run.PersonaID != "persona-comp" || run.ConversationID != "channel-a" {
				t.Fatalf("the run is not the person's own run on this message: %+v", run)
			}
			// Sent again: Chat answers with the same message, and no second run.
			if again, err := service.SendPost(ctx, tc.request); err != nil || again.ID != post.ID {
				t.Fatalf("replay post=%+v err=%v", again, err)
			}
			if grants.calls != 1 || len(runs.requests) != 1 {
				t.Fatalf("a replay started a second run: grants=%d runs=%d", grants.calls, len(runs.requests))
			}
		})
	}
	// The command is one the agent platform registers with Chat's registry,
	// through the versioned interface; Chat's own list is unchanged.
	registry := personaAskCommands()
	command, ok := registry.Lookup("ASK")
	if !ok || command.Owner != chatcore.Chatcmd001AskOwner || command.Preview || !command.Agents || !strings.HasPrefix(command.Usage, "/ask") || !strings.HasPrefix(command.Example, "/ask @") {
		t.Fatalf("the ask command is not registered as the agent platform's: %+v ok=%v", command, ok)
	}
	if _, own := chatcore.Chatcmd001Defaults().Lookup("ask"); own {
		t.Fatal("the ask command was added to Chat's own commands")
	}
	for _, place := range []chatcore.Chatcmd001Place{{Kind: chatcore.PublicChannel}, {Kind: chatcore.PrivateChannel}, {Kind: chatcore.Group}, {Kind: chatcore.Direct, Agent: true}} {
		if !command.AllowedIn(place) {
			t.Errorf("the ask command is not allowed in %+v", place)
		}
	}
	if again := chatcore.Chatcmd001WithAsk(registry); len(again.Commands()) != len(registry.Commands()) {
		t.Fatal("registering the ask command twice added it twice")
	}
}

// TestTodo_AGENT_027_Security covers what must not start a run or must not be
// written at all: text that is not the person's own new words, an agent that
// is not placed here or that the person may not use, and command lines that
// name nobody, several agents, or ask nothing.
func TestTodo_AGENT_027_Security(t *testing.T) {
	other := chatcore.Reference{Kind: chatcore.AgentMention, TenantID: "tenant-a", ID: "persona-other", Display: "Other Agent"}
	t.Run("refused commands write nothing and run nothing", func(t *testing.T) {
		for name, tc := range map[string]struct {
			direct  bool
			request chatcore.SendPostRequest
			reason  string
		}{
			"no agent named in a channel": {request: agent027Ask("/ask summarize the public policy"), reason: PersonaAskNeedsAgent},
			"only a person named":         {request: agent027Ask("/ask @Dana summarize", chatcore.Reference{Kind: chatcore.PersonMention, TenantID: "tenant-a", ID: "dana", Display: "Dana"}), reason: PersonaAskNeedsAgent},
			"no question":                 {request: agent027Ask("/ask @Comp Analyst", agent027Agent), reason: PersonaAskNeedsQuestion},
			"no question, spaces":         {request: agent027Ask("/ask   @Comp Analyst   ", agent027Agent), reason: PersonaAskNeedsQuestion},
			"no question in a direct":     {direct: true, request: agent027Ask("/ask"), reason: PersonaAskNeedsQuestion},
			"two agents":                  {request: agent027Ask("/ask @Comp Analyst @Other Agent compare", agent027Agent, other), reason: PersonaAskOneAgent},
			"forwarded command": {request: func() chatcore.SendPostRequest {
				request := agent027Ask("/ask @Comp Analyst summarize", agent027Agent)
				request.SourceAttribution = &chatcore.SourceAttribution{}
				return request
			}(), reason: PersonaAskNotOwnWords},
		} {
			service, chat, refs, runs, grants, failures, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), nil)
			if tc.direct {
				agent027Direct(chat)
			}
			post, err := service.SendPost(ctx, tc.request)
			var refusal *PersonaAskCommandError
			if !errors.As(err, &refusal) || refusal.Reason != tc.reason || !errors.Is(err, chatcore.ErrInvalidArgument) {
				t.Errorf("%s: err=%v, want refusal %q as an invalid argument", name, err, tc.reason)
			}
			if post.ID != "" || chat.calls != 0 || refs.calls != 0 || grants.calls != 0 || len(runs.requests) != 0 || len(failures.errs) != 0 {
				t.Errorf("%s: a refused command wrote or started something: post=%+v sends=%d refs=%d grants=%d runs=%d", name, post, chat.calls, refs.calls, grants.calls, len(runs.requests))
			}
		}
	})
	t.Run("text that only looks like the command is an ordinary message", func(t *testing.T) {
		for _, body := range []string{"//ask @Comp Analyst summarize", "please /ask @Comp Analyst summarize", "> /ask @Comp Analyst summarize", "/asking for a friend", "/ask-me anything"} {
			service, chat, _, _, _, _, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), nil)
			post, err := service.SendPost(ctx, agent027Ask(body))
			if err != nil || post.Body != body || chat.calls != 1 {
				t.Errorf("%q: post=%+v err=%v", body, post, err)
			}
		}
	})
	t.Run("the command gives no more than a mention gives", func(t *testing.T) {
		for name, admission := range map[string]func() agentinvoke.Admission{
			"agent not placed here":  func() agentinvoke.Admission { a := personaAdmission(); a.PersonaInstalled = false; return a },
			"outside its audience":   func() agentinvoke.Admission { a := personaAdmission(); a.AudienceMember = false; return a },
			"not a member":           func() agentinvoke.Admission { a := personaAdmission(); a.HumanMember = false; return a },
			"agent version not live": func() agentinvoke.Admission { a := personaAdmission(); a.Persona.Current = false; return a },
		} {
			service, chat, _, runs, grants, _, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, admission(), nil)
			post, err := service.SendPost(ctx, agent027Ask("/ask @Comp Analyst summarize the public policy", agent027Agent))
			if err != nil || post.ID == "" || chat.calls != 1 {
				t.Errorf("%s: the person's message was not kept: post=%+v err=%v", name, post, err)
			}
			if grants.calls != 0 || len(runs.requests) != 0 {
				t.Errorf("%s: the command started a run a mention would not: grants=%d runs=%d", name, grants.calls, len(runs.requests))
			}
		}
		// An agent, or an edited message, never starts a run by command.
		for name, tc := range map[string]struct {
			kind     trust.SubjectKind
			revision uint64
		}{"agent author": {trust.SubjectKindAgent, 1}, "edited message": {trust.SubjectKindHuman, 2}} {
			service, chat, _, runs, grants, _, ctx := personaInvocationFixture(t, tc.kind, personaAdmission(), nil)
			chat.post = chatcore.Post{ID: "post-x", TenantID: "tenant-a", ConversationID: "channel-a", AuthorID: "alice", Body: "@Comp Analyst summarize", Revision: tc.revision, References: []chatcore.Reference{agent027Agent}}
			if _, err := service.SendPost(ctx, agent027Ask("/ask @Comp Analyst summarize", agent027Agent)); err != nil {
				t.Errorf("%s: %v", name, err)
			}
			if grants.calls != 0 || len(runs.requests) != 0 {
				t.Errorf("%s started a run: grants=%d runs=%d", name, grants.calls, len(runs.requests))
			}
		}
	})
	t.Run("a non-read-only run is stopped before the runner", func(t *testing.T) {
		service, chat, _, runs, _, failures, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), nil, false)
		post, err := service.SendPost(ctx, agent027Ask("/ask @Comp Analyst summarize", agent027Agent))
		if err != nil || post.ID == "" || chat.calls != 1 || len(runs.requests) != 0 || len(failures.errs) != 1 {
			t.Fatalf("post=%+v err=%v runs=%d failures=%v", post, err, len(runs.requests), failures.errs)
		}
	})
}

// TestTodo_AGENT_027_Fault: the person's message is durable whatever happens
// to the agent. A failing provider is recorded against the message and does
// not fail the send; a failing chat store starts nothing.
func TestTodo_AGENT_027_Fault(t *testing.T) {
	providerErr := errors.New("synthetic provider failed")
	for name, request := range map[string]chatcore.SendPostRequest{
		"typed mention": agent027Ask("@Comp Analyst summarize the public policy", agent027Agent),
		"ask command":   agent027Ask("/ask @Comp Analyst summarize the public policy", agent027Agent),
	} {
		service, chat, _, runs, _, failures, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), providerErr)
		post, err := service.SendPost(ctx, request)
		if err != nil || post.ID != "post-1" || chat.calls != 1 {
			t.Fatalf("%s: the provider failure reached the sender: post=%+v err=%v", name, post, err)
		}
		if len(runs.requests) != 1 || len(failures.errs) != 1 || failures.posts[0] != post.ID || !errors.Is(failures.errs[0], providerErr) {
			t.Fatalf("%s: runs=%d failures=%v posts=%v", name, len(runs.requests), failures.errs, failures.posts)
		}

		service, chat, refs, runs, grants, _, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), nil)
		chat.err = errors.New("chat store failed")
		if _, err := service.SendPost(ctx, request); err == nil {
			t.Fatalf("%s: the chat failure was hidden", name)
		}
		if refs.calls != 0 || grants.calls != 0 || len(runs.requests) != 0 {
			t.Fatalf("%s: an agent run started without a committed message: refs=%d grants=%d runs=%d", name, refs.calls, grants.calls, len(runs.requests))
		}
	}
	// The direct conversation cannot be read: an ask that names nobody is
	// refused, not guessed.
	service, chat, _, runs, _, _, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), nil)
	agent027Direct(chat)
	chat.err = errors.New("chat store failed")
	var refusal *PersonaAskCommandError
	if _, err := service.SendPost(ctx, agent027Ask("/ask summarize")); !errors.As(err, &refusal) || refusal.Reason != PersonaAskNeedsAgent || len(runs.requests) != 0 {
		t.Fatalf("an unreadable conversation was treated as one with an agent: err=%v runs=%d", err, len(runs.requests))
	}
}

// TestTodo_AGENT_027_Integration sends the three forms through the decorator
// the served Chat uses, with the invocations kept in the real invocation
// store. One message is one durable invocation and one run, across a replay
// and across a second coordinator over the same database (a restart).
func TestTodo_AGENT_027_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenantID := uuid.New()
	db.Exec(t, "INSERT INTO tenant(tenant_id) VALUES ($1)", tenantID)
	store := commonAgentOpenIntegrationStore(t, db)
	invocations, err := agentinvocationstore.NewWithTenantUUID(store, func(string) uuid.UUID { return tenantID })
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 2, 9, 20, 0, 0, time.UTC)
	worker, chat := &agentp015Worker{}, &agent027Chat{byKey: map[string]chatcore.Post{}}
	served := func(t *testing.T) *personaChatConversationService {
		t.Helper()
		coordinator, err := newPersonaChatInvocation(personaChatInvocationConfig{
			Chat: chat, References: agentp015References{},
			Authority: personaAuthorityFake{admission: personaAdmission(), requireTuple: true}, Grants: agentp015Grants{},
			Runs: worker, T0Skills: personaT0PolicyFake{allowed: true}, Repository: invocations, Failures: &agentp015Failures{},
		})
		if err != nil {
			t.Fatal(err)
		}
		service, err := newPersonaChatConversationService(chat, chat, coordinator, nil)
		if err != nil {
			t.Fatal(err)
		}
		return service
	}
	service := served(t)
	count := func(t *testing.T) int {
		t.Helper()
		var n int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM persona_invocations WHERE tenant_id=$1`, tenantID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	caller := agentp015Context(t, "alice", now)
	send := func(t *testing.T, service *personaChatConversationService, key, body string) chatcore.Post {
		t.Helper()
		request := agent027Ask(body, agent027Agent)
		request.IdempotencyKey = key
		post, err := service.SendPostWithReferences(caller, chatcore.SendPostWithReferencesRequest{SendPostRequest: request, References: request.References})
		if err != nil {
			t.Fatalf("send %q: %v", body, err)
		}
		return post
	}
	mention := send(t, service, "key-mention", "@Comp Analyst summarize the public policy")
	command := send(t, service, "key-command", "/ask @Comp Analyst summarize the public policy")
	if mention.Body != command.Body || command.Body != "@Comp Analyst summarize the public policy" || mention.ID == command.ID {
		t.Fatalf("the command did not commit the message a mention commits: %q and %q", mention.Body, command.Body)
	}
	if worker.count() != 2 || count(t) != 2 {
		t.Fatalf("two messages: runs=%d stored invocations=%d, want 2 and 2", worker.count(), count(t))
	}
	// The same command sent again, and again after a restart.
	if again := send(t, service, "key-command", "/ask @Comp Analyst summarize the public policy"); again.ID != command.ID {
		t.Fatalf("replay committed a second message: %q then %q", command.ID, again.ID)
	}
	if again := send(t, served(t), "key-command", "/ask @Comp Analyst summarize the public policy"); again.ID != command.ID {
		t.Fatalf("replay after a restart committed a second message: %q then %q", command.ID, again.ID)
	}
	if worker.count() != 2 || count(t) != 2 {
		t.Fatalf("replays: runs=%d stored invocations=%d, want still 2 and 2", worker.count(), count(t))
	}
	// A refused command reaches neither Chat nor the store.
	refused := agent027Ask("/ask @Comp Analyst", agent027Agent)
	refused.IdempotencyKey = "key-refused"
	if _, err := service.SendPostWithReferences(caller, chatcore.SendPostWithReferencesRequest{SendPostRequest: refused, References: refused.References}); !errors.Is(err, chatcore.ErrInvalidArgument) {
		t.Fatalf("a command that asks nothing = %v", err)
	}
	if worker.count() != 2 || count(t) != 2 {
		t.Fatalf("a refused command left something behind: runs=%d stored invocations=%d", worker.count(), count(t))
	}
}

// agent027Chat is the Chat the decorator wraps. It keeps one message per
// idempotency key, as Chat does, and outlives the coordinators built over it,
// so a second coordinator sees what the first committed.
type agent027Chat struct {
	chatcore.ConversationService
	chatcore.ReferenceService
	byKey map[string]chatcore.Post
}

func (c *agent027Chat) SendPost(_ context.Context, request chatcore.SendPostRequest) (chatcore.Post, error) {
	if post, ok := c.byKey[request.IdempotencyKey]; ok {
		return post, nil
	}
	post := chatcore.Post{
		ID: uuid.NewString(), TenantID: request.TenantID, ConversationID: request.ConversationID, AuthorID: request.Principal.SubjectID,
		Body: request.Body, Revision: 1, References: append([]chatcore.Reference(nil), request.References...),
	}
	c.byKey[request.IdempotencyKey] = post
	return post, nil
}

func (c *agent027Chat) SendPostWithReferences(ctx context.Context, request chatcore.SendPostWithReferencesRequest) (chatcore.Post, error) {
	request.SendPostRequest.References = request.References
	return c.SendPost(ctx, request.SendPostRequest)
}
