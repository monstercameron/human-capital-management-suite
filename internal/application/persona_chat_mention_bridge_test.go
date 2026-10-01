package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_AGENTP_008_MentionBridgeStartsOneIdempotentRun(t *testing.T) {
	resolver := &personaReferenceResolverFake{mentions: []agentinvoke.Mention{
		{Kind: agentinvoke.PersonaMention, PersonaID: "persona-comp", Display: "Comp Analyst", Canonical: true},
		{Kind: agentinvoke.PersonaMention, PersonaID: "persona-comp", Display: "forged display", Canonical: true},
	}}
	runs, grants := &personaRunFake{}, &personaGrantFake{requireTuple: true}
	service, err := agentinvoke.NewService(agentinvoke.Config{
		Authority: personaAuthorityFake{admission: personaAdmission(), requireTuple: true}, Grants: grants, Runs: runs,
		Repository: agentinvoke.NewMemoryRepository(),
	})
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := newPersonaChatMentionBridge(personaChatMentionBridgeConfig{References: resolver, Resolver: service})
	if err != nil {
		t.Fatal(err)
	}
	ctx := personaBridgeContext(t)
	post := personaBridgePost()
	for i := 0; i < 2; i++ {
		if err := bridge.HandlePostCommit(ctx, post); err != nil {
			t.Fatal(err)
		}
	}
	if resolver.calls != 2 || grants.calls != 1 || len(runs.requests) != 1 {
		t.Fatalf("calls=%d grants=%d runs=%d, want replay-safe one run", resolver.calls, grants.calls, len(runs.requests))
	}
	if got := runs.requests[0].Actor.PersonaID; got != "persona-comp" || runs.requests[0].Actor.InvokingPostID != post.ID {
		t.Fatalf("run actor=%+v, want canonical post binding", runs.requests[0].Actor)
	}
}

func TestTodo_AGENTP_008_MentionBridgeRejectsUntrustedOrMismatchedPrincipal(t *testing.T) {
	service, err := agentinvoke.NewService(agentinvoke.Config{
		Authority: personaAuthorityFake{admission: personaAdmission()}, Grants: &personaGrantFake{}, Runs: &personaRunFake{},
		Repository: agentinvoke.NewMemoryRepository(),
	})
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := newPersonaChatMentionBridge(personaChatMentionBridgeConfig{References: &personaReferenceResolverFake{mentions: []agentinvoke.Mention{{Kind: agentinvoke.PersonaMention, PersonaID: "persona-comp", Canonical: true}}}, Resolver: service})
	if err != nil {
		t.Fatal(err)
	}
	post := personaBridgePost()
	if err := bridge.HandlePostCommit(context.Background(), post); !errors.Is(err, errPersonaChatMentionBridge) {
		t.Fatalf("missing principal error=%v, want bridge sentinel", err)
	}
	wrongCtx := trust.WithPrincipal(context.Background(), mustBridgePrincipal(t, "other-user", "tenant-a"))
	if err := bridge.HandlePostCommit(wrongCtx, post); !errors.Is(err, errPersonaChatMentionBridge) {
		t.Fatalf("mismatched principal error=%v, want bridge sentinel", err)
	}
}

func TestTodo_AGENT_027_MentionBridgeRejectsEditedForwardedAndAttributedPosts(t *testing.T) {
	service, err := agentinvoke.NewService(agentinvoke.Config{
		Authority: personaAuthorityFake{admission: personaAdmission()}, Grants: &personaGrantFake{}, Runs: &personaRunFake{},
		Repository: agentinvoke.NewMemoryRepository(),
	})
	if err != nil {
		t.Fatal(err)
	}
	refs := &personaReferenceResolverFake{mentions: []agentinvoke.Mention{{Kind: agentinvoke.PersonaMention, PersonaID: "persona-comp", Canonical: true}}}
	bridge, err := newPersonaChatMentionBridge(personaChatMentionBridgeConfig{References: refs, Resolver: service})
	if err != nil {
		t.Fatal(err)
	}
	ctx := personaBridgeContext(t)
	for _, mutate := range []func(*chatcore.Post){
		func(p *chatcore.Post) { p.Revision = 2 },
		func(p *chatcore.Post) { p.Deleted = true },
		func(p *chatcore.Post) { p.SourceAttribution = &chatcore.SourceAttribution{PostID: "source"} },
	} {
		post := personaBridgePost()
		mutate(&post)
		if err := bridge.HandlePostCommit(ctx, post); !errors.Is(err, errPersonaChatMentionBridge) {
			t.Fatalf("invalid post error=%v, want bridge sentinel", err)
		}
	}
	if refs.calls != 0 {
		t.Fatalf("invalid posts reached reference resolver %d times", refs.calls)
	}
}

func TestTodo_AGENTP_008_MentionBridgeRequiresCompositionPorts(t *testing.T) {
	if _, err := newPersonaChatMentionBridge(personaChatMentionBridgeConfig{}); !errors.Is(err, errPersonaChatMentionBridge) {
		t.Fatalf("missing ports error=%v, want bridge sentinel", err)
	}
}

func personaBridgePost() chatcore.Post {
	return chatcore.Post{ID: "post-bridge-1", TenantID: "tenant-a", ConversationID: "channel-a", AuthorID: "alice", Body: "summarize policy", Revision: 1, References: []chatcore.Reference{{Kind: chatcore.AgentMention, TenantID: "tenant-a", ID: "persona-comp", Display: "Comp Analyst"}}}
}

func personaBridgeContext(t *testing.T) context.Context {
	t.Helper()
	return trust.WithPrincipal(context.Background(), mustBridgePrincipal(t, "alice", "tenant-a"))
}

func mustBridgePrincipal(t *testing.T, subject, tenant string) *trust.Principal {
	t.Helper()
	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow, SessionRef: "bridge-test", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "bridge-credential"})
	if err != nil {
		t.Fatal(err)
	}
	return principal
}
