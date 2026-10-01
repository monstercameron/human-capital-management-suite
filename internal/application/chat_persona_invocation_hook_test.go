package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaHookConversation struct {
	chatcore.ConversationService
	writer *personaChatWriterFake
}

func (c personaHookConversation) SendPost(ctx context.Context, request chatcore.SendPostRequest) (chatcore.Post, error) {
	return c.writer.SendPost(ctx, request)
}

func TestTodo_AGENTP_008_StreamingSendPostHandsOffOnlyWhenBound(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "alice", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow,
		SessionRef: "persona-stream-hook-test", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "test-credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	writer := &personaChatWriterFake{}
	inner := personaHookConversation{writer: writer}
	refs := &personaReferenceResolverFake{mentions: []agentinvoke.Mention{{Kind: agentinvoke.PersonaMention, PersonaID: "persona-comp", Canonical: true}}}
	invocations := agentinvoke.NewMemoryRepository()
	builder, err := NewPersonaRunRequestBuilder(servedPersonaFacts{})
	if err != nil {
		t.Fatal(err)
	}
	wiring, err := NewPersonaInvocationServeWiring(PersonaInvocationServeConfig{
		Chat: writer, References: refs, Authority: personaAuthorityFake{admission: personaAdmission()},
		Grants: &personaGrantFake{}, T0Skills: personaT0PolicyFake{allowed: true}, Invocations: invocations,
		Run: PersonaRunBindingConfig{
			Builder: builder, Authority: personaChatAdmissionAuthorityFake{}, AdmissionStore: agentrun.NewMemoryAdmissionStore(),
			ExecutionStore: runstate.NewMemoryStore(), AdmissionRecheck: personaChatAdmissionRecheckerFake{}, Now: func() time.Time { return now },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	served := &streamingChatService{ConversationService: inner}
	if err := served.bindPersonaInvocation(wiring); err != nil {
		t.Fatal(err)
	}
	request := chatcore.SendPostRequest{
		TenantID: "tenant-a", ConversationID: "channel-a", Body: "Please summarize the policy.",
		Principal:  chatcore.Principal{SubjectID: "alice", TenantID: "tenant-a"},
		References: []chatcore.Reference{{Kind: chatcore.AgentMention, ID: "persona-comp"}},
	}
	post, err := served.SendPost(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if post.ID != "post-1" || writer.calls != 1 || refs.calls != 1 {
		t.Fatalf("post=%+v commits=%d reference resolutions=%d; want one commit and one mention resolution", post, writer.calls, refs.calls)
	}
	if _, err := invocations.Get("tenant-a", post.ID, "persona-comp"); err != nil {
		t.Fatalf("served send did not hand off canonical mention: %v", err)
	}

	plainWriter := &personaChatWriterFake{}
	ordinary := &streamingChatService{ConversationService: personaHookConversation{writer: plainWriter}}
	post, err = ordinary.SendPost(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if post.ID != "post-1" || plainWriter.calls != 1 {
		t.Fatalf("unbound served send post=%+v commits=%d; want ordinary single commit", post, plainWriter.calls)
	}
}
