package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type currentDMIdentities struct {
	rows  []agentpersonastore.PersonaChatIdentity
	err   error
	reads int
}

func (r *currentDMIdentities) ListPersonaChatIdentities(context.Context, string) ([]agentpersonastore.PersonaChatIdentity, error) {
	r.reads++
	return r.rows, r.err
}

func TestTodo_AGENTP_011_CurrentDMRechecksPublishedIdentity(t *testing.T) {
	store := streamIntegrationStore(t)
	at := time.Now().UTC()
	invocation := personaDMRouterInvocation()
	invocation.PersonaVersion = "1"
	invocation.Actor.PersonaVersion = "1"
	invocation.Grant.ExpiresAt = at.Add(time.Hour)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "human-1", SubjectKind: trust.SubjectKindHuman, Purposes: []string{"persona-mention"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "current-dm-test", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "verified-test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithPersonaDMInvocation(trust.WithPrincipal(context.Background(), principal), invocation)
	invoker := chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}
	persona := chatcore.MemberRef{TenantID: "tenant-a", SubjectID: "canonical-agent"}
	dmID, err := chatcore.DirectPairConversationID("tenant-a", []chatcore.MemberRef{{TenantID: invoker.TenantID, SubjectID: invoker.SubjectID}, persona})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateConversation(ctx, chatcore.Conversation{ID: dmID, TenantID: "tenant-a", Kind: chatcore.Direct, OwnerID: invoker.SubjectID, Revision: 1}, []chatcore.Membership{
		{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: dmID, SubjectID: invoker.SubjectID, Role: chatcore.Manager, HistoryVisibility: chatcore.FullHistory},
		{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: dmID, SubjectID: persona.SubjectID, Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory},
	}, "current-dm-input")
	if err != nil {
		t.Fatal(err)
	}
	identity := agentpersonastore.PersonaChatIdentity{TenantID: "tenant-a", PersonaID: "persona-a", AgentID: persona.SubjectID, Active: true, RegisteredAt: at.Add(-time.Minute)}
	identities := &currentDMIdentities{rows: []agentpersonastore.PersonaChatIdentity{identity}}
	creator := &personaDMProvisionCreator{}
	publication := identityPublicationFake{version: agentpersona.PersonaVersion{Profile: agentpersona.PersonaProfile{PersonaID: "persona-a", Version: 1}}}
	resolver := &publishedPersonaDM{publications: publication, identities: identities, store: store, creator: creator, now: func() time.Time { return at }}
	if got, err := resolver.ResolvePersonaDM(ctx, invoker, "tenant-a"); err != nil || got != dmID {
		t.Fatalf("current DM=%q err=%v", got, err)
	}
	if creator.request.ConversationID != "" || identities.reads != 1 {
		t.Fatal("existing DM did not use current identity or was recreated")
	}
	identities.rows[0].Active = false
	if got, err := resolver.ResolvePersonaDM(ctx, invoker, "tenant-a"); got != "" || !errors.Is(err, chatcore.ErrPermissionDenied) || identities.reads != 2 {
		t.Fatalf("revoked identity accepted: %q %v", got, err)
	}
	identities.rows = []agentpersonastore.PersonaChatIdentity{identity}
	for _, testCase := range []struct {
		name   string
		change func()
	}{
		{"publication removed", func() { resolver.publications = identityPublicationFake{err: ErrPersonaIdentityNotPublished} }},
		{"wrong published version", func() { p := publication; p.version.Profile.Version = 2; resolver.publications = p }},
		{"foreign identity", func() { identities.rows[0].TenantID = "tenant-b" }},
		{"revoked identity", func() { identities.rows[0].RevokedAt = &at }},
		{"future identity", func() { identities.rows[0].RegisteredAt = at.Add(time.Hour) }},
		{"ambiguous identity", func() { identities.rows = append(identities.rows, identity) }},
		{"unknown identity", func() { identities.rows = nil }},
		{"identity store unavailable", func() { identities.err = errors.New("offline") }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resolver.publications = publication
			identities.rows = []agentpersonastore.PersonaChatIdentity{identity}
			identities.err = nil
			testCase.change()
			if got, err := resolver.ResolvePersonaDM(ctx, invoker, "tenant-a"); got != "" || err == nil {
				t.Fatalf("invalid owner accepted: %q %v", got, err)
			}
			if creator.request.ConversationID != "" {
				t.Fatal("denied owner created a DM")
			}
		})
	}
	resolver.publications = publication
	identities.rows = []agentpersonastore.PersonaChatIdentity{identity}
	identities.err = nil
	for _, badCtx := range []context.Context{context.Background(), trust.WithPrincipal(context.Background(), principal), WithPersonaDMInvocation(context.Background(), invocation), WithPersonaDMInvocation(trust.WithPrincipal(context.Background(), principal), func() agentinvoke.RunRequest { v := invocation; v.Grant.ExpiresAt = at; return v }())} {
		if got, err := resolver.ResolvePersonaDM(badCtx, invoker, "tenant-a"); got != "" || !errors.Is(err, errPersonaDMInvocation) {
			t.Fatalf("invalid invocation accepted: %q %v", got, err)
		}
	}
}
