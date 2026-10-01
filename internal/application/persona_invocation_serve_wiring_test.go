package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type servedPersonaFacts struct{}

func (servedPersonaFacts) ResolvePersonaRun(_ context.Context, invocation agentinvoke.RunRequest) (PersonaRunRequestFacts, error) {
	facts := personaRunRequestBuilderFacts()
	facts.TenantID = invocation.TenantID
	facts.TriggerID = invocation.InvocationID
	facts.Audience.ID = invocation.ConversationID
	facts.Context.ID = invocation.ThreadID
	return facts, nil
}

func TestTodo_AGENTP_008_ServeWiringCommitsPostThenPersistsPersonaRun(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "alice", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow,
		SessionRef: "persona-serve-test", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "test-credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	chat := &personaChatWriterFake{}
	refs := &personaReferenceResolverFake{mentions: []agentinvoke.Mention{{Kind: agentinvoke.PersonaMention, PersonaID: "persona-comp", Canonical: true}}}
	grants := &personaGrantFake{}
	policy := personaT0PolicyFake{allowed: true}
	invocations := agentinvoke.NewMemoryRepository()
	admissionStore := agentrun.NewMemoryAdmissionStore()
	executionStore := runstate.NewMemoryStore()
	builder, err := NewPersonaRunRequestBuilder(servedPersonaFacts{})
	if err != nil {
		t.Fatal(err)
	}
	wiring, err := NewPersonaInvocationServeWiring(PersonaInvocationServeConfig{
		Chat: chat, References: refs, Authority: personaAuthorityFake{admission: personaAdmission()},
		Grants: grants, T0Skills: policy, Invocations: invocations,
		Run: PersonaRunBindingConfig{
			Builder: builder, Authority: personaChatAdmissionAuthorityFake{}, AdmissionStore: admissionStore,
			ExecutionStore: executionStore, AdmissionRecheck: personaChatAdmissionRecheckerFake{}, Now: func() time.Time { return now },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	post, err := wiring.SendPost(ctx, chatcore.SendPostRequest{
		TenantID: "tenant-a", ConversationID: "channel-a", Body: "Please summarize the policy.",
		Principal:  chatcore.Principal{SubjectID: "alice", TenantID: "tenant-a"},
		References: []chatcore.Reference{{Kind: chatcore.AgentMention, ID: "persona-comp"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if post.ID != "post-1" || chat.calls != 1 || refs.calls != 1 || grants.calls != 1 {
		t.Fatalf("post=%+v chat=%d refs=%d grants=%d; want one committed post and one admission", post, chat.calls, refs.calls, grants.calls)
	}
	invocation, err := invocations.Get("tenant-a", post.ID, "persona-comp")
	if err != nil || invocation.State != agentinvoke.InvocationStarted || invocation.Grant.ID == "" {
		t.Fatalf("invocation=%+v err=%v; want started invocation with grant", invocation, err)
	}
	request := agentinvoke.RunRequest{
		InvocationID: invocation.ID, TenantID: invocation.TenantID, ConversationID: invocation.ConversationID,
		ThreadID: invocation.ThreadID, InvokingPostID: invocation.PostID, InvokerID: invocation.InvokerID,
		PersonaID: invocation.PersonaID, PersonaVersion: invocation.PersonaVersion, InstallationID: invocation.InstallationID,
		Mode: invocation.Mode, Grant: invocation.Grant, Skills: invocation.Skills, Actor: invocation.Actor,
	}
	admissionRequest, err := builder.BuildPersonaChatAdmission(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	record, err := admissionStore.Get(admissionRequest.Source)
	if err != nil || record.Decision != agentrun.DecisionAccepted || record.Request.Source.Ref != post.ID {
		t.Fatalf("durable admission=%+v err=%v; want accepted request sourced from post %q", record, err, post.ID)
	}
	runID, err := agentrun.AdmissionRequestID(record.Request.Source)
	if err != nil {
		t.Fatal(err)
	}
	run, err := executionStore.Get(ctx, runID)
	if err != nil || run.State != runstate.StateReady || run.AdmissionID != record.ID {
		t.Fatalf("durable run=%+v err=%v; want READY run tied to accepted admission", run, err)
	}
}

func TestTodo_AGENTP_008_ServeWiringFailsClosedWithoutRunAuthority(t *testing.T) {
	_, err := NewPersonaInvocationServeWiring(PersonaInvocationServeConfig{})
	if !errors.Is(err, errPersonaInvocationServeWiring) {
		t.Fatalf("error=%v; want fail-closed serve wiring error", err)
	}
}
