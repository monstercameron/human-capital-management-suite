package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

type personaRunFactsSourceFake struct {
	facts PersonaRunRequestFacts
	err   error
	seen  agentinvoke.RunRequest
}

func (f *personaRunFactsSourceFake) ResolvePersonaRun(_ context.Context, invocation agentinvoke.RunRequest) (PersonaRunRequestFacts, error) {
	f.seen = invocation
	return f.facts, f.err
}

func TestTodo_AGENT_015_PersonaRunRequestBuilderMapsServerFacts(t *testing.T) {
	invocation := personaRunRequestBuilderInvocation()
	source := &personaRunFactsSourceFake{facts: personaRunRequestBuilderFacts()}
	builder, err := NewPersonaRunRequestBuilder(source)
	if err != nil {
		t.Fatal(err)
	}
	got, err := builder.BuildPersonaChatAdmission(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	if source.seen.InvocationID != invocation.InvocationID {
		t.Fatalf("source saw invocation=%q, want %q", source.seen.InvocationID, invocation.InvocationID)
	}
	if got.Source != (agentrun.SourceIdentity{TenantID: "tenant-1", Kind: agentrun.SourcePersonaMention, Key: "trigger-1", Ref: "post-1"}) {
		t.Fatalf("source=%+v", got.Source)
	}
	if got.LegalEntity != "legal-1" || got.Agent.AgentID != "agent-1" || got.Agent.Version != "v4" || got.Agent.Digest == "" {
		t.Fatalf("owner facts=%+v legal=%q", got.Agent, got.LegalEntity)
	}
	if got.Principal.AgentPrincipalID != "principal-1" || got.Principal.InvokerID != "user-1" || got.Principal.DelegatedCredentialRef != "grant-1" {
		t.Fatalf("principal=%+v", got.Principal)
	}
	if got.Audience.SnapshotID != "audience-rev-1" || got.Context.SnapshotID != "context-rev-1" || got.Budget.MaxCostMicros != 900 || got.CauseID != "trigger-1" {
		t.Fatalf("pinned evidence audience=%+v context=%+v budget=%+v cause=%q", got.Audience, got.Context, got.Budget, got.CauseID)
	}
}

func TestTodo_AGENTP_008_PersonaRunBuilderRequiresGrantTargetToMatchPinnedAgent(t *testing.T) {
	invocation := personaRunRequestBuilderInvocation()
	invocation.Grant.TargetAgentID = "agent:another-persona"
	builder, err := NewPersonaRunRequestBuilder(&personaRunFactsSourceFake{facts: personaRunRequestBuilderFacts()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.BuildPersonaChatAdmission(context.Background(), invocation); !errors.Is(err, errPersonaRunRequestBuilder) {
		t.Fatalf("mismatched pinned agent error=%v, want fail-closed builder refusal", err)
	}
	invocation.Grant.TargetAgentID = "agent-1"
	if _, err := builder.BuildPersonaChatAdmission(context.Background(), invocation); err != nil {
		t.Fatalf("matching exact agent ID was refused: %v", err)
	}
}

func TestTodo_AGENT_015_PersonaRunRequestBuilderRejectsMissingOrForgedFacts(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*PersonaRunRequestFacts)
	}{
		{name: "tenant mismatch", mutate: func(f *PersonaRunRequestFacts) { f.TenantID = "tenant-2" }},
		{name: "trigger mismatch", mutate: func(f *PersonaRunRequestFacts) { f.TriggerID = "other-trigger" }},
		{name: "legal entity missing", mutate: func(f *PersonaRunRequestFacts) { f.LegalEntityID = "" }},
		{name: "agent digest missing", mutate: func(f *PersonaRunRequestFacts) { f.Agent.Digest = "" }},
		{name: "context snapshot missing", mutate: func(f *PersonaRunRequestFacts) { f.Context.SnapshotID = "" }},
		{name: "budget missing", mutate: func(f *PersonaRunRequestFacts) { f.Budget.MaxOutputTokens = 0 }},
		{name: "deadline missing", mutate: func(f *PersonaRunRequestFacts) { f.Deadline = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			facts := personaRunRequestBuilderFacts()
			tc.mutate(&facts)
			builder, err := NewPersonaRunRequestBuilder(&personaRunFactsSourceFake{facts: facts})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := builder.BuildPersonaChatAdmission(context.Background(), personaRunRequestBuilderInvocation()); !errors.Is(err, errPersonaRunRequestBuilder) {
				t.Fatalf("error=%v, want builder refusal", err)
			}
		})
	}
}

func TestTodo_AGENT_015_PersonaRunRequestBuilderPropagatesSourceFailure(t *testing.T) {
	sourceErr := errors.New("authority unavailable")
	builder, err := NewPersonaRunRequestBuilder(&personaRunFactsSourceFake{err: sourceErr})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.BuildPersonaChatAdmission(context.Background(), personaRunRequestBuilderInvocation()); !errors.Is(err, sourceErr) {
		t.Fatalf("error=%v, want source error", err)
	}
}

func TestTodo_AGENT_015_PersonaRunRequestBuilderRequiresSource(t *testing.T) {
	if _, err := NewPersonaRunRequestBuilder(nil); !errors.Is(err, errPersonaRunRequestBuilder) {
		t.Fatalf("error=%v", err)
	}
}

func personaRunRequestBuilderInvocation() agentinvoke.RunRequest {
	skills := agentinvoke.SkillScopes{"persona.read": {"worker:self"}}
	return agentinvoke.RunRequest{
		InvocationID: "trigger-1", TenantID: "tenant-1", ConversationID: "conversation-1", ThreadID: "thread-1", InvokingPostID: "post-1", InvokerID: "user-1",
		PersonaID: "persona-1", PersonaVersion: "v2", InstallationID: "install-1", Mode: agentinvoke.OnBehalfOf,
		Grant: agentinvoke.DelegationGrant{ID: "grant-1", UserID: "user-1", TenantID: "tenant-1", Skills: skills.Clone()}, Skills: skills,
		Actor: agentinvoke.ActorChain{UserID: "user-1", PersonaID: "persona-1", PersonaVersion: "v2", InstallationID: "install-1", ConversationID: "conversation-1", InvokingPostID: "post-1", InvocationID: "trigger-1"},
	}
}

func personaRunRequestBuilderFacts() PersonaRunRequestFacts {
	return PersonaRunRequestFacts{
		TenantID: "tenant-1", LegalEntityID: "legal-1", Agent: agentrun.VersionRef{AgentID: "agent-1", Version: "v4", Digest: builderDigest("a")}, AgentPrincipal: "principal-1", PersonaDigest: builderDigest("b"),
		Audience: agentrun.AudienceScope{ID: "conversation-1", SnapshotID: "audience-rev-1", Digest: builderDigest("c")}, Context: agentrun.ContextScope{ID: "thread-1", SnapshotID: "context-rev-1", Digest: builderDigest("d")},
		Deadline: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), Budget: agentrun.Budget{MaxCostMicros: 900, MaxInputTokens: 3000, MaxOutputTokens: 700}, TriggerID: "trigger-1",
	}
}

func builderDigest(seed string) string {
	return "sha256:" + stringRepeat(seed[0], 64)
}

func stringRepeat(char byte, count int) string {
	bytes := make([]byte, count)
	for i := range bytes {
		bytes[i] = char
	}
	return string(bytes)
}
