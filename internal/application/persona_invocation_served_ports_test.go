package application

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type servedPortReferenceSource struct{ lookup personaReferenceLookup }

func (s servedPortReferenceSource) ListPersonaReferenceCandidates(context.Context, chatcore.Principal, string, string, string) ([]chatcore.ReferenceCandidate, error) {
	return nil, nil
}

func (s servedPortReferenceSource) LookupPersonaReference(ctx context.Context, tenant, conversation, id string) (personaReferenceFacts, error) {
	return s.lookup.LookupPersonaReference(ctx, tenant, conversation, id)
}

type servedPortChatWriter struct {
	chatcore.ConversationService
	writer *personaChatWriterFake
}

func (s servedPortChatWriter) SendPost(ctx context.Context, request chatcore.SendPostRequest) (chatcore.Post, error) {
	return s.writer.SendPost(ctx, request)
}

func TestTodo_AGENTP_008_ServedPortsCommitWithTrustedTenantAndResolveBoundPersona(t *testing.T) {
	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "alice", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow,
		SessionRef: "persona-served-ports-test", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "test-credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	writer := &personaChatWriterFake{}
	inner := servedPortChatWriter{writer: writer}
	lookup := &personaReferenceLookupFake{byReference: map[string]personaReferenceFacts{
		"agent-ref:comp": currentPersonaReferenceFacts("agent-ref:comp"),
	}}
	refs := &lazyPersonaReferenceSource{}
	refs.bind(servedPortReferenceSource{lookup: lookup})
	var logs bytes.Buffer
	ports, err := composePersonaInvocationServedPorts(
		&streamingChatService{ConversationService: inner},
		&personaServeWiring{refs: refs, now: func() time.Time { return now }},
		slog.New(slog.NewTextHandler(&logs, nil)),
	)
	if err != nil {
		t.Fatal(err)
	}
	request := chatcore.SendPostRequest{
		TenantID: "tenant-a", ConversationID: "channel-a", Body: "Summarize this policy.",
		Principal: chatcore.Principal{SubjectID: "alice", TenantID: "tenant-a"},
	}
	post, err := ports.chat.SendPost(ctx, request)
	if err != nil || post.ID != "post-1" || writer.calls != 1 {
		t.Fatalf("post=%+v calls=%d err=%v; want one committed post", post, writer.calls, err)
	}
	mentions, err := ports.references.ResolvePersonaMentions(ctx, post.TenantID, post.ConversationID, []chatcore.Reference{personaReference("agent-ref:comp", "untrusted display")})
	if err != nil || len(mentions) != 1 || mentions[0].Kind != agentinvoke.PersonaMention || mentions[0].PersonaID != "persona.comp-analyst" || !mentions[0].Canonical {
		t.Fatalf("mentions=%+v err=%v; want canonical current persona", mentions, err)
	}
	if got := lookup.calls; len(got) != 1 || got[0] != "tenant-a/channel-a/agent-ref:comp" {
		t.Fatalf("persona lookups=%v; want exact committed tenant and conversation", got)
	}
	ports.failures.RecordPersonaInvocationFailure(ctx, post.ID, errors.New("admission unavailable"))
	if !strings.Contains(logs.String(), "post_id=post-1") || !strings.Contains(logs.String(), "tenant_id=tenant-a") || !strings.Contains(logs.String(), "error_type=*errors.errorString") || strings.Contains(logs.String(), "admission unavailable") {
		t.Fatalf("failure sink did not record bound post context: %q", logs.String())
	}
}

func TestTodo_AGENTP_008_ServedPostWriterRejectsForgedOrMissingTrustedBinding(t *testing.T) {
	writer := &personaChatWriterFake{}
	ports := servedPersonaPostWriter{service: servedPortChatWriter{writer: writer}}
	request := chatcore.SendPostRequest{
		TenantID: "tenant-a", ConversationID: "channel-a",
		Principal: chatcore.Principal{SubjectID: "alice", TenantID: "tenant-a"},
	}
	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-b"), Subject: "alice", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow,
		SessionRef: "persona-served-ports-mismatch", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "test-credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{name: "missing principal", ctx: context.Background()},
		{name: "tenant mismatch", ctx: trust.WithPrincipal(context.Background(), principal)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ports.SendPost(tc.ctx, request); !errors.Is(err, chatcore.ErrPermissionDenied) {
				t.Fatalf("error=%v; want permission denied", err)
			}
			if writer.calls != 0 {
				t.Fatalf("untrusted request reached durable writer %d time(s)", writer.calls)
			}
		})
	}
}

func TestTodo_AGENTP_008_ServedPortsFailClosedWhenDependenciesAreAbsent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	if _, err := composePersonaInvocationServedPorts(nil, nil, logger); !errors.Is(err, errPersonaInvocationServedPorts) {
		t.Fatalf("error=%v; want missing served ports error", err)
	}
	if _, err := composePersonaInvocationServedPorts(&streamingChatService{ConversationService: servedPortChatWriter{}}, &personaServeWiring{refs: &lazyPersonaReferenceSource{}}, logger); !errors.Is(err, errPersonaInvocationServedPorts) {
		t.Fatalf("error=%v; want unbound canonical persona source to fail closed", err)
	}
}
