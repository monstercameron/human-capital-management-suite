package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

type personaChatAdmissionBuilderFake struct {
	request agentrun.Request
	err     error
	seen    agentinvoke.RunRequest
	calls   int
}

func (b *personaChatAdmissionBuilderFake) BuildPersonaChatAdmission(_ context.Context, invocation agentinvoke.RunRequest) (agentrun.Request, error) {
	b.calls++
	b.seen = invocation
	return b.request, b.err
}

type personaChatAdmissionAuthorityFake struct{ err error }

func (a personaChatAdmissionAuthorityFake) VerifyAdmission(_ context.Context, request agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	if a.err != nil {
		return agentrun.AuthoritySnapshot{}, a.err
	}
	return agentrun.AuthoritySnapshot{
		Agent: request.Agent, InstallationID: request.InstallationID,
		Principal: request.Principal, Audience: request.Audience, Context: request.Context,
		BudgetCeiling: request.Budget, GrantRef: "grant-ref", PolicyDigest: testPersonaDigest,
	}, nil
}

type personaChatAdmittedExecutorFake struct {
	record  agentrun.Record
	run     runstate.Run
	service *runstate.Service
	store   *runstate.MemoryStore
	calls   int
}

type personaChatAdmissionRecheckerFake struct{}

func (personaChatAdmissionRecheckerFake) Recheck(context.Context, string, string) error { return nil }

func (e *personaChatAdmittedExecutorFake) Start(ctx context.Context, record agentrun.Record) (runstate.Run, error) {
	e.calls++
	e.record = record
	run, err := e.service.Start(ctx, record)
	e.run = run
	return run, err
}

func personaChatAdmittedExecutorFixture(t *testing.T) *personaChatAdmittedExecutorFake {
	t.Helper()
	store := runstate.NewMemoryStore()
	service, err := runstate.New(store, personaChatAdmissionRecheckerFake{})
	if err != nil {
		t.Fatal(err)
	}
	return &personaChatAdmittedExecutorFake{service: service, store: store}
}

const testPersonaDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestTodo_AGENT_027_AdmissionAdapterBindsAndStartsAcceptedInvocation(t *testing.T) {
	invocation := personaChatRunRequestFixture()
	builder := &personaChatAdmissionBuilderFake{request: personaChatAdmissionRequestFixture()}
	executor := personaChatAdmittedExecutorFixture(t)
	adapter := personaChatAdmissionAdapterFixture(t, builder, personaChatAdmissionAuthorityFake{}, executor)

	if err := adapter.Start(context.Background(), invocation); err != nil {
		t.Fatal(err)
	}
	got := executor.record.Request
	if executor.calls != 1 || executor.record.Decision != agentrun.DecisionAccepted {
		t.Fatalf("executor calls=%d record=%+v, want one accepted durable record", executor.calls, executor.record)
	}
	if builder.calls != 1 || builder.seen.InvocationID != invocation.InvocationID {
		t.Fatalf("builder calls=%d invocation=%+v", builder.calls, builder.seen)
	}
	persistedRun, runErr := executor.store.Get(context.Background(), executor.record.ID)
	if runErr != nil || persistedRun.ID != executor.record.ID || persistedRun.State != runstate.StateReady || persistedRun.AdmissionID != executor.record.ID {
		t.Fatalf("runstate persistence=%+v err=%v", persistedRun, runErr)
	}
	if got.Source != (agentrun.SourceIdentity{TenantID: invocation.TenantID, Kind: agentrun.SourcePersonaMention, Key: invocation.InvocationID, Ref: invocation.InvokingPostID}) {
		t.Fatalf("source was not derived from the committed invocation: %+v", got.Source)
	}
	if got.Agent.AgentID != "agent-policy-assistant" || got.Agent.Version != "v9.2.1" || got.InstallationID != invocation.InstallationID {
		t.Fatalf("persona binding=%+v installation=%q", got.Agent, got.InstallationID)
	}
	if got.Principal.Mode != agentrun.ModeOnBehalfOf || got.Principal.InvokerID != invocation.InvokerID || got.Principal.DelegatedCredentialRef != invocation.Grant.ID || got.Principal.SponsorID != "" {
		t.Fatalf("principal binding=%+v", got.Principal)
	}
	if got.Audience.ID != invocation.ConversationID || got.Context.ID != invocation.ThreadID || got.CauseID != invocation.InvocationID || got.Purpose != "persona-mention" {
		t.Fatalf("scope binding audience=%+v context=%+v cause=%q purpose=%q", got.Audience, got.Context, got.CauseID, got.Purpose)
	}
}

func TestTodo_AGENTP_008_AdmissionAdapterRejectsInvalidActorBeforeAdmission(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*agentinvoke.RunRequest)
	}{
		{name: "sponsored mode", mutate: func(r *agentinvoke.RunRequest) { r.Mode = "SPONSORED" }},
		{name: "actor belongs to another invoker", mutate: func(r *agentinvoke.RunRequest) { r.Actor.UserID = "other-user" }},
		{name: "grant belongs to another tenant", mutate: func(r *agentinvoke.RunRequest) { r.Grant.TenantID = "other-tenant" }},
		{name: "grant widens skills", mutate: func(r *agentinvoke.RunRequest) { r.Grant.Skills = agentinvoke.SkillScopes{"admin.write": {"tenant:*"}} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			builder := &personaChatAdmissionBuilderFake{request: personaChatAdmissionRequestFixture()}
			executor := personaChatAdmittedExecutorFixture(t)
			adapter := personaChatAdmissionAdapterFixture(t, builder, personaChatAdmissionAuthorityFake{}, executor)
			invocation := personaChatRunRequestFixture()
			tc.mutate(&invocation)
			if err := adapter.Start(context.Background(), invocation); !errors.Is(err, errPersonaChatAdmissionAdapter) {
				t.Fatalf("Start error=%v, want fail-closed adapter error", err)
			}
			if builder.calls != 0 || executor.calls != 0 {
				t.Fatalf("invalid invocation reached builder/executor: builder=%d executor=%d", builder.calls, executor.calls)
			}
		})
	}
}

func TestTodo_AGENT_027_AdmissionAdapterRefusalDoesNotStartExecution(t *testing.T) {
	refusal := &agentrun.AdmissionRefusal{Code: "PERSONA_SUSPENDED"}
	builder := &personaChatAdmissionBuilderFake{request: personaChatAdmissionRequestFixture()}
	executor := personaChatAdmittedExecutorFixture(t)
	adapter := personaChatAdmissionAdapterFixture(t, builder, personaChatAdmissionAuthorityFake{err: refusal}, executor)

	err := adapter.Start(context.Background(), personaChatRunRequestFixture())
	if !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("Start error=%v, want durable authority refusal", err)
	}
	if executor.calls != 0 {
		t.Fatalf("refused request reached executor %d times", executor.calls)
	}
}

func TestTodo_AGENTP_008_AdmissionAdapterRequiresTrustedOwnerEvidence(t *testing.T) {
	request := personaChatAdmissionRequestFixture()
	request.Agent.Digest = ""
	builder := &personaChatAdmissionBuilderFake{request: request}
	executor := personaChatAdmittedExecutorFixture(t)
	adapter := personaChatAdmissionAdapterFixture(t, builder, personaChatAdmissionAuthorityFake{}, executor)

	err := adapter.Start(context.Background(), personaChatRunRequestFixture())
	if err == nil || executor.calls != 0 {
		t.Fatalf("missing immutable agent digest error=%v executor calls=%d", err, executor.calls)
	}
}

func TestTodo_AGENT_027_AdmissionAdapterRequiresEveryProductionPort(t *testing.T) {
	if _, err := newPersonaChatAdmissionAdapter(nil, nil, nil); !errors.Is(err, errPersonaChatAdmissionAdapter) {
		t.Fatalf("empty adapter error=%v, want fail-closed error", err)
	}
	if _, err := newPersonaChatAdmissionAdapter(&personaChatAdmissionBuilderFake{}, nil, personaChatAdmittedExecutorFixture(t)); !errors.Is(err, errPersonaChatAdmissionAdapter) {
		t.Fatalf("adapter without admission error=%v, want fail-closed error", err)
	}
}

func personaChatAdmissionAdapterFixture(t *testing.T, builder personaChatAdmissionRequestBuilder, authority agentrun.Authority, executor personaChatAdmittedRunExecutor) *personaChatAdmissionAdapter {
	t.Helper()
	service, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{
		Authority: authority, Store: agentrun.NewMemoryAdmissionStore(),
		Now: func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := newPersonaChatAdmissionAdapter(builder, service, executor)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func personaChatRunRequestFixture() agentinvoke.RunRequest {
	skills := agentinvoke.SkillScopes{"policy.read": {"tenant:acme"}}
	return agentinvoke.RunRequest{
		InvocationID: "pinv-chat-post-persona", TenantID: "tenant-acme", ConversationID: "conversation-1",
		ThreadID: "thread-1", InvokingPostID: "post-1", InvokerID: "user-1", PersonaID: "persona-1",
		PersonaVersion: "v3", InstallationID: "installation-1", Mode: agentinvoke.OnBehalfOf,
		Grant:  agentinvoke.DelegationGrant{ID: "delegation-1", UserID: "user-1", TenantID: "tenant-acme", Skills: skills.Clone()},
		Skills: skills, Actor: agentinvoke.ActorChain{
			UserID: "user-1", PersonaID: "persona-1", PersonaVersion: "v3", InstallationID: "installation-1",
			ConversationID: "conversation-1", InvokingPostID: "post-1", InvocationID: "pinv-chat-post-persona",
		},
	}
}

func personaChatAdmissionRequestFixture() agentrun.Request {
	return agentrun.Request{
		LegalEntity: "entity-acme",
		Agent:       agentrun.VersionRef{AgentID: "agent-policy-assistant", Version: "v9.2.1", Digest: testPersonaDigest},
		Persona:     &agentrun.PersonaRef{ID: "persona-1", Version: "v3", Digest: testPersonaDigest},
		Principal:   agentrun.PrincipalChain{AgentPrincipalID: "agent-principal-1"},
		Audience:    agentrun.AudienceScope{SnapshotID: "audience-snapshot-1", Digest: testPersonaDigest},
		Context:     agentrun.ContextScope{SnapshotID: "context-snapshot-1", Digest: testPersonaDigest},
		Deadline:    time.Date(2026, 9, 29, 13, 0, 0, 0, time.UTC),
		Budget:      agentrun.Budget{MaxCostMicros: 1000, MaxInputTokens: 1000, MaxOutputTokens: 500},
	}
}
