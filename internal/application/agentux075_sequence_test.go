package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// The reaction to a question is chosen from its words, from one small table.
func TestTodo_AGENTUX_075(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{"What can you help me with here?", "👀"},
		{"", "👀"},
		{"List all the holidays still to come", "🔎"},
		{"Which documents cover parental leave?", "🔎"},
		{"Please search the handbook for travel rules", "🔎"},
		{"Thanks!", "🙏"},
		{"thank you, that was perfect", "🙌"},
		{"The expense form is not working for me", "🛠️"},
		{"This is unacceptable, I want to complain", "🫡"},
		{"Hello", "👋"},
		{"hi there", "👋"},
		// A long message that begins with a greeting is a question.
		{"Hi, could you explain how carry over works for people who joined mid year?", "👀"},
	} {
		if got := agentUX075QuestionEmoji(tc.body); got != tc.want {
			t.Errorf("%q reacted with %q, want %q", tc.body, got, tc.want)
		}
		if got := agentUX075QuestionEmoji(tc.body); !chatcore.ValidAgentQuestionReactionEmoji(got) {
			t.Errorf("%q chose %q, outside the fixed set", tc.body, got)
		}
	}
	if code, retry := personaPostFailureClassification(errPersonaModelNotConfigured); code != "SERVER_HAS_NO_MODEL" || retry {
		t.Fatalf("no-model failure classified as %q retry=%v", code, retry)
	}
	// The card says so in each language, in one plain sentence.
	for locale, want := range map[string]string{"en-US": "no model is configured on this server", "de-DE": "kein Modell eingerichtet", "ar": "نموذج"} {
		got := chatcore.AgentAnswerFailureFor(locale, "Assistant", "SERVER_HAS_NO_MODEL")
		if got.Class != "no_model" || !strings.Contains(got.Sentence, want) || strings.Contains(got.Sentence, "{agent}") {
			t.Errorf("%s: no-model card reads %+v", locale, got)
		}
	}
}

// agentUX075Room is the chat service of a served assembly: it answers the reads
// the direct-conversation question needs, and nothing else.
type agentUX075Room struct {
	chatcore.ConversationService
	mu      sync.Mutex
	kind    chatcore.ConversationKind
	members []string
	posted  int
}

func (r *agentUX075Room) SendPost(_ context.Context, request chatcore.SendPostRequest) (chatcore.Post, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.posted++
	return chatcore.Post{
		ID: "post-1", TenantID: request.TenantID, ConversationID: request.ConversationID,
		AuthorID: request.Principal.SubjectID, AuthorHomeTenantID: request.TenantID, Body: request.Body, Revision: 1,
		References: append([]chatcore.Reference(nil), request.References...),
	}, nil
}

func (r *agentUX075Room) GetConversation(_ context.Context, request chatcore.GetConversationRequest) (chatcore.Conversation, error) {
	return chatcore.Conversation{ID: request.ConversationID, TenantID: request.TenantID, Kind: r.kind}, nil
}

func (r *agentUX075Room) ListMemberships(_ context.Context, request chatcore.ListMembershipsRequest) (chatcore.ListMembershipsResponse, error) {
	joined := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	var out []chatcore.Membership
	for _, member := range r.members {
		out = append(out, chatcore.Membership{TenantID: request.TenantID, HomeTenantID: request.TenantID, ConversationID: request.ConversationID, SubjectID: member, JoinedAt: &joined})
	}
	return chatcore.ListMembershipsResponse{Memberships: out}, nil
}

// agentUX075Source says which identities are installed agents here.
type agentUX075Source struct{ agents map[string]bool }

func (agentUX075Source) ListPersonaReferenceCandidates(context.Context, chatcore.Principal, string, string, string) ([]chatcore.ReferenceCandidate, error) {
	return nil, nil
}

func (s agentUX075Source) LookupPersonaReference(_ context.Context, tenant, conversation, id string) (personaReferenceFacts, error) {
	if !s.agents[id] {
		return personaReferenceFacts{}, errPersonaReferenceNotPersona
	}
	return personaReferenceFacts{ReferenceID: id, TenantID: tenant, ConversationID: conversation, PersonaID: id, InstallationID: "install-channel-a", PersonaVersion: 1, CurrentVersion: 1, InstallationState: personaReferenceActive, PersonaLifecycle: personaReferencePublished}, nil
}

type agentUX075Log struct{}

func (agentUX075Log) Error(string, ...any) {}

type agentUX075Events struct {
	mu     sync.Mutex
	events []string
}

func (e *agentUX075Events) add(event string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
}

func (e *agentUX075Events) list() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.events...)
}

type agentUX075Reactor struct{ events *agentUX075Events }

func (r agentUX075Reactor) ReactToQuestion(_ context.Context, post chatcore.Post, asker chatcore.Principal, agent string) {
	r.events.add("react:" + agent + ":" + agentUX075QuestionEmoji(post.Body) + ":" + asker.SubjectID)
}

type agentUX075Runs struct{ events *agentUX075Events }

func (r agentUX075Runs) Start(_ context.Context, request agentinvoke.RunRequest) error {
	r.events.add("run:" + request.PersonaID)
	return nil
}

type agentUX075PassClassifier struct{}

func (agentUX075PassClassifier) ClassifyAcceptedHumanPost(context.Context, chatcore.Post) error {
	return nil
}

func agentUX075Principal(t *testing.T, kind trust.SubjectKind) context.Context {
	t.Helper()
	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "alice", SubjectKind: kind,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow,
		SessionRef: "agentux075", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "test-credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), principal)
}

// agentUX075Served composes the served ports over a fake room exactly as the
// server does, then joins them to a chat invocation with a fake model worker.
func agentUX075Served(t *testing.T, room *agentUX075Room, events *agentUX075Events) *personaChatInvocation {
	t.Helper()
	served := &streamingChatService{ConversationService: room}
	personas := &personaServeWiring{
		refs: &lazyPersonaReferenceSource{source: agentUX075Source{agents: map[string]bool{"persona-comp": true}}},
		now:  func() time.Time { return time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC) },
	}
	ports, err := composePersonaInvocationServedPorts(served, personas, agentUX075Log{})
	if err != nil {
		t.Fatal(err)
	}
	writer, err := ports.ClassifiedHumanWriter(agentUX075PassClassifier{})
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := newPersonaChatInvocation(personaChatInvocationConfig{
		Chat: writer, References: ports.references, Conversations: ports.directory,
		Authority: personaAuthorityFake{admission: personaAdmission(), requireTuple: true}, Grants: &personaGrantFake{requireTuple: true},
		Runs: agentUX075Runs{events: events}, T0Skills: personaT0PolicyFake{allowed: true},
		Repository: agentinvoke.NewMemoryRepository(), Failures: ports.failures,
		Reactions: agentUX075Reactor{events: events},
	})
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}

func agentUX075Question(body string, references ...chatcore.Reference) chatcore.SendPostRequest {
	return chatcore.SendPostRequest{
		Principal: chatcore.Principal{TenantID: "tenant-a", SubjectID: "alice"}, TenantID: "tenant-a",
		ConversationID: "channel-a", Body: body, IdempotencyKey: "question-1", References: references,
	}
}

// A person's plain message in their own conversation with an agent is a question
// to it: reaction first, then one run. This failed in the served assembly
// because the post writer there cannot read the conversation or its members, so
// the direct conversation was never recognized and nothing was invoked.
func TestTodo_AGENTUX_075_Integration(t *testing.T) {
	t.Run("direct conversation", func(t *testing.T) {
		events := &agentUX075Events{}
		room := &agentUX075Room{kind: chatcore.Direct, members: []string{"alice", "persona-comp"}}
		invocation := agentUX075Served(t, room, events)
		ctx := agentUX075Principal(t, trust.SubjectKindHuman)
		if _, err := invocation.SendPost(ctx, agentUX075Question("What can you help me with here?")); err != nil {
			t.Fatal(err)
		}
		want := []string{"react:persona-comp:👀:alice", "run:persona-comp"}
		if got := events.list(); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("sequence %v, want %v", got, want)
		}
		// The same post event delivered a second time starts no second run.
		if _, err := invocation.SendPost(ctx, agentUX075Question("What can you help me with here?")); err != nil {
			t.Fatal(err)
		}
		runs := 0
		for _, event := range events.list() {
			if strings.HasPrefix(event, "run:") {
				runs++
			}
		}
		if runs != 1 {
			t.Fatalf("%d runs after a second delivery of the same post, want 1: %v", runs, events.list())
		}
	})
	t.Run("mention in a channel", func(t *testing.T) {
		events := &agentUX075Events{}
		room := &agentUX075Room{kind: chatcore.PublicChannel, members: []string{"alice", "bob", "persona-comp"}}
		invocation := agentUX075Served(t, room, events)
		ctx := agentUX075Principal(t, trust.SubjectKindHuman)
		reference := chatcore.Reference{Kind: chatcore.AgentMention, TenantID: "tenant-a", ID: "persona-comp", Display: "Comp Analyst", ConversationID: "channel-a"}
		if _, err := invocation.SendPost(ctx, agentUX075Question("@Comp Analyst list all the holidays", reference)); err != nil {
			t.Fatal(err)
		}
		want := []string{"react:persona-comp:🔎:alice", "run:persona-comp"}
		if got := events.list(); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("sequence %v, want %v", got, want)
		}
	})
	t.Run("nobody is asked", func(t *testing.T) {
		for name, room := range map[string]*agentUX075Room{
			"two people":             {kind: chatcore.Direct, members: []string{"alice", "bob"}},
			"a channel, no mention":  {kind: chatcore.PublicChannel, members: []string{"alice", "persona-comp"}},
			"agent with two members": {kind: chatcore.Direct, members: []string{"alice", "persona-comp", "bob"}},
		} {
			events := &agentUX075Events{}
			invocation := agentUX075Served(t, room, events)
			if _, err := invocation.SendPost(agentUX075Principal(t, trust.SubjectKindHuman), agentUX075Question("hello")); err != nil {
				t.Fatal(err)
			}
			if got := events.list(); len(got) != 0 {
				t.Fatalf("%s: invoked %v", name, got)
			}
		}
	})
	t.Run("the agent's own posts never invoke", func(t *testing.T) {
		events := &agentUX075Events{}
		room := &agentUX075Room{kind: chatcore.Direct, members: []string{"alice", "persona-comp"}}
		invocation := agentUX075Served(t, room, events)
		// An agent identity posting in its own conversation asks nobody.
		_, _ = invocation.SendPost(agentUX075Principal(t, trust.SubjectKindAgent), agentUX075Question("Here is your answer"))
		if got := events.list(); len(got) != 0 {
			t.Fatalf("an agent's post invoked %v", got)
		}
	})
	t.Run("no model configured", func(t *testing.T) {
		room := &agentUX075Room{kind: chatcore.Direct, members: []string{"alice", "persona-comp"}}
		served := &streamingChatService{ConversationService: room}
		personas := &personaServeWiring{
			refs: &lazyPersonaReferenceSource{source: agentUX075Source{agents: map[string]bool{"persona-comp": true}}},
			now:  time.Now,
		}
		ports, err := composePersonaInvocationServedPorts(served, personas, agentUX075Log{})
		if err != nil {
			t.Fatal(err)
		}
		failures := &personaFailureFake{}
		wiring, err := newPersonaModelUnavailableInvocation(ports.chat, ports.directory, ports.references, failures, nil)
		if err != nil {
			t.Fatal(err)
		}
		wiring.Chat.detached = false
		if err := served.bindPersonaInvocation(wiring); err != nil {
			t.Fatal(err)
		}
		if _, err := served.SendPost(agentUX075Principal(t, trust.SubjectKindHuman), agentUX075Question("What can you help me with here?")); err != nil {
			t.Fatal(err)
		}
		if len(failures.errs) != 1 || !errors.Is(failures.errs[0], errPersonaModelNotConfigured) {
			t.Fatalf("the question to an agent with no model recorded %v, want one stated failure", failures.errs)
		}
		// A message between two people is not an agent's failure.
		people := &agentUX075Room{kind: chatcore.Direct, members: []string{"alice", "bob"}}
		servedPeople := &streamingChatService{ConversationService: people}
		portsPeople, err := composePersonaInvocationServedPorts(servedPeople, personas, agentUX075Log{})
		if err != nil {
			t.Fatal(err)
		}
		failures = &personaFailureFake{}
		wiring, err = newPersonaModelUnavailableInvocation(portsPeople.chat, portsPeople.directory, portsPeople.references, failures, nil)
		if err != nil {
			t.Fatal(err)
		}
		wiring.Chat.detached = false
		if err := servedPeople.bindPersonaInvocation(wiring); err != nil {
			t.Fatal(err)
		}
		if _, err := servedPeople.SendPost(agentUX075Principal(t, trust.SubjectKindHuman), agentUX075Question("lunch?")); err != nil {
			t.Fatal(err)
		}
		if len(failures.errs) != 0 {
			t.Fatalf("two people talking recorded %v", failures.errs)
		}
	})
}
