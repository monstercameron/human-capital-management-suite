package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type ownerLegalFake struct{ value string }

func (f ownerLegalFake) Resolve(context.Context, agentinvoke.RunRequest) (string, error) {
	return f.value, nil
}

type ownerPrincipalFake struct{ value string }

func (f ownerPrincipalFake) Resolve(_ context.Context, tenant values.TenantId, persona string, version int64) (string, error) {
	if tenant != "tenant-a" || persona != "persona-a" || version != 7 {
		return "", errors.New("wrong principal lookup")
	}
	return f.value, nil
}

type ownerAudienceFake struct{ value agentrun.AudienceScope }

func (f ownerAudienceFake) ResolvePersonaRunAudience(context.Context, agentinvoke.RunRequest) (agentrun.AudienceScope, error) {
	return f.value, nil
}

type ownerThreadFake struct {
	snapshot chat.ThreadSnapshot
	err      error
}

func (f ownerThreadFake) CaptureThreadSnapshot(context.Context, chat.ThreadSnapshotRequest) (chat.ThreadSnapshot, error) {
	return f.snapshot, f.err
}

type ownerPolicyFake struct{ value PersonaRunEffectivePolicy }

func (f ownerPolicyFake) Resolve(_ context.Context, tenant, legal string) (PersonaRunEffectivePolicy, error) {
	if tenant != "tenant-a" || legal != "entity-a" {
		return PersonaRunEffectivePolicy{}, errors.New("wrong policy scope")
	}
	return f.value, nil
}

func TestTodo_AGENTP_015_PersonaRunOwnerFactsComposesAuthoritativeCurrentFacts(t *testing.T) {
	invocation := personaOwnerFactsInvocation()
	snapshot := personaOwnerFactsThread(invocation)
	digest, err := chat.ThreadSnapshotDigest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Digest = digest
	snapshot.SnapshotID = "chat-thread-" + digest
	deadline := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	composer, err := NewPersonaRunOwnerFactsComposer(ownerLegalFake{"entity-a"}, ownerPrincipalFake{"service-a"}, ownerAudienceFake{agentrun.AudienceScope{ID: "room-a", SnapshotID: "aud-a", Digest: runSourceDigest('a')}}, ownerThreadFake{snapshot: snapshot}, ownerPolicyFake{PersonaRunEffectivePolicy{Deadline: deadline, Budget: agentrun.Budget{MaxCostMicros: 5, MaxInputTokens: 40, MaxOutputTokens: 20}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), personaOwnerFactsPrincipal(t))
	facts, err := composer.ResolvePersonaRunOwnerFacts(ctx, invocation)
	if err != nil {
		t.Fatal(err)
	}
	if facts.LegalEntityID != "entity-a" || facts.AgentPrincipal != "service-a" || facts.Audience.ID != "room-a" || facts.Context.ID != "thread-a" || facts.Context.SnapshotID != snapshot.SnapshotID || facts.Context.Digest != digest {
		t.Fatalf("composed identities=%+v", facts)
	}
	if facts.Deadline != deadline || facts.Budget.MaxCostMicros != 5 || facts.Budget.MaxInputTokens != 40 || facts.Budget.MaxOutputTokens != 20 {
		t.Fatalf("policy facts=%+v", facts)
	}
}

func TestTodo_AGENTP_015_PersonaRunOwnerFactsFailsClosedOnMissingAuthority(t *testing.T) {
	if _, err := NewPersonaRunOwnerFactsComposer(nil, ownerPrincipalFake{"service"}, ownerAudienceFake{}, ownerThreadFake{}, ownerPolicyFake{}); !errors.Is(err, errPersonaRunOwnerFacts) {
		t.Fatalf("missing source error=%v", err)
	}
	invocation := personaOwnerFactsInvocation()
	snapshot := personaOwnerFactsThread(invocation)
	digest, _ := chat.ThreadSnapshotDigest(snapshot)
	snapshot.Digest, snapshot.SnapshotID = digest, "chat-thread-"+digest
	composer, err := NewPersonaRunOwnerFactsComposer(ownerLegalFake{"entity-a"}, ownerPrincipalFake{"service"}, ownerAudienceFake{}, ownerThreadFake{snapshot: snapshot}, ownerPolicyFake{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := composer.ResolvePersonaRunOwnerFacts(context.Background(), invocation); !errors.Is(err, errPersonaRunOwnerFacts) {
		t.Fatalf("unverified caller error=%v", err)
	}
}

func TestTodo_AGENTP_008_DatabasePersonaRunRequestBuilderComposesCurrentSource(t *testing.T) {
	source := personaRunValidSource(t)
	builder, err := NewDatabasePersonaRunRequestBuilder(source.Personas, source.Manifests, source.OwnerFacts)
	if err != nil {
		t.Fatal(err)
	}
	invocation := personaRunTestInvocation()
	invocation.Skills = agentinvoke.SkillScopes{"persona.read": {"worker:self"}}
	invocation.Grant = agentinvoke.DelegationGrant{ID: "grant-a", UserID: invocation.InvokerID, TenantID: invocation.TenantID, Skills: invocation.Skills.Clone()}
	request, err := builder.BuildPersonaChatAdmission(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	if request.Source.TenantID != "tenant-a" || request.Source.Kind != agentrun.SourcePersonaMention || request.Persona == nil || request.Persona.ID != "persona-a" {
		t.Fatalf("composed admission identity=%+v persona=%+v", request.Source, request.Persona)
	}
}

func personaOwnerFactsInvocation() agentinvoke.RunRequest {
	return agentinvoke.RunRequest{InvocationID: "invoke-a", TenantID: "tenant-a", ConversationID: "room-a", ThreadID: "thread-a", InvokingPostID: "post-a", InvokerID: "user-a", PersonaID: "persona-a", PersonaVersion: "v7", Mode: agentinvoke.OnBehalfOf}
}

func personaOwnerFactsThread(invocation agentinvoke.RunRequest) chat.ThreadSnapshot {
	return chat.ThreadSnapshot{
		TenantID: invocation.TenantID, ConversationID: invocation.ConversationID, ThreadID: invocation.ThreadID, InvokingPostID: invocation.InvokingPostID,
		PrincipalTenantID: invocation.TenantID, PrincipalID: invocation.InvokerID, Revision: 2, AuthorityRevision: 4,
		Posts: []chat.Post{{TenantID: invocation.TenantID, ConversationID: invocation.ConversationID, ID: invocation.InvokingPostID, AuthorID: invocation.InvokerID, ParentID: invocation.ThreadID, Revision: 1}},
	}
}

func personaOwnerFactsPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "user-a", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-a", IssuedAt: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), CredentialDigest: "credential-a"})
	if err != nil {
		t.Fatal(err)
	}
	return principal
}
