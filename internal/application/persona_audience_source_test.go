package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type audienceChatFake struct {
	rooms   []chat.Conversation
	members map[string][]chat.Membership
}

func (f audienceChatFake) ListConversations(_ context.Context, req chat.ListConversationsRequest) (chat.ListConversationsResponse, error) {
	if req.Page.Cursor != "" {
		return chat.ListConversationsResponse{}, nil
	}
	return chat.ListConversationsResponse{Conversations: f.rooms}, nil
}

func (f audienceChatFake) ListMemberships(_ context.Context, req chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error) {
	if req.Page.Cursor != "" {
		return chat.ListMembershipsResponse{}, nil
	}
	return chat.ListMembershipsResponse{Memberships: f.members[req.ConversationID]}, nil
}

type audienceDirectoryFake struct {
	facts map[string]PersonaAudienceMember
	err   error
}

func (f audienceDirectoryFake) ResolvePersonaAudienceMember(_ context.Context, tenant, subject string) (PersonaAudienceMember, error) {
	if f.err != nil {
		return PersonaAudienceMember{}, f.err
	}
	return f.facts[tenant+"/"+subject], nil
}

type audienceInstallFake struct {
	active    []agentpersonastore.ActiveInstallation
	published []agentpersonastore.PersonaVersion
}

func (f audienceInstallFake) ListActiveByConversation(context.Context, string) ([]agentpersonastore.ActiveInstallation, error) {
	return f.active, nil
}
func (f audienceInstallFake) ListPublished(context.Context) ([]agentpersonastore.PersonaVersion, error) {
	return f.published, nil
}

type audienceInstallStoreFake struct {
	store PersonaAudienceInstallationReader
}

func (f audienceInstallStoreFake) ForTenant(context.Context, values.TenantId) (PersonaAudienceInstallationReader, error) {
	return f.store, nil
}

func audienceSourceContext(t *testing.T) context.Context {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant", Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "credential"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), p)
}

func TestPersonaAudienceSourceReadsChatAndTrustedDirectory(t *testing.T) {
	ctx := audienceSourceContext(t)
	s := &DatabasePersonaAudienceSource{
		Chat:      audienceChatFake{rooms: []chat.Conversation{{ID: "room", TenantID: "tenant"}}, members: map[string][]chat.Membership{"room": {{ConversationID: "room", TenantID: "tenant", HomeTenantID: "tenant", SubjectID: "alice"}}}},
		Directory: audienceDirectoryFake{facts: map[string]PersonaAudienceMember{"tenant/alice": {SubjectID: "alice", Roles: []string{"manager"}, Populations: []string{"staff"}, OrganizationScope: "org"}}},
	}
	got, err := s.ListCurrentPersonaAudience(ctx, "tenant", "alice")
	if err != nil || len(got) != 1 || len(got[0].Members) != 1 || got[0].Members[0].Roles[0] != "manager" {
		t.Fatalf("audience=%+v err=%v", got, err)
	}
}

func TestPersonaAudienceSourceMissingDirectoryFactsIsPrecise(t *testing.T) {
	ctx := audienceSourceContext(t)
	s := &DatabasePersonaAudienceSource{Chat: audienceChatFake{rooms: []chat.Conversation{{ID: "room", TenantID: "tenant"}}, members: map[string][]chat.Membership{"room": {{ConversationID: "room", TenantID: "tenant", HomeTenantID: "tenant", SubjectID: "alice"}}}}, Directory: audienceDirectoryFake{}}
	_, err := s.ListCurrentPersonaAudience(ctx, "tenant", "alice")
	if !errors.Is(err, ErrPersonaAudienceDirectoryFactsMissing) {
		t.Fatalf("err=%v, want directory blocker", err)
	}
}

func TestTodo_AGENT_044_IndependentPublishedInstallationAudience(t *testing.T) {
	ctx := audienceSourceContext(t)
	s := &DatabasePersonaAudienceSource{Installations: audienceInstallStoreFake{store: audienceInstallFake{published: []agentpersonastore.PersonaVersion{{TenantID: values.TenantId("tenant"), PersonaID: "p", Version: 2}, {TenantID: values.TenantId("tenant"), PersonaID: "p", Version: 1}}, active: []agentpersonastore.ActiveInstallation{{PersonaID: "p", PersonaVersion: 1, InstallationID: "old", ConversationID: "room"}, {PersonaID: "p", PersonaVersion: 2, InstallationID: "new", ConversationID: "room", Audience: agentpersonastore.InstallationAudience{Roles: []string{"manager"}}}}}}}
	got, err := s.ListCurrentPersonaInstallations(ctx, "tenant", "room")
	if err != nil || len(got) != 2 || got[0].Tuple.InstallationID != "old" || got[1].Tuple.InstallationID != "new" || !got[0].CurrentVersion || !got[1].CurrentVersion {
		t.Fatalf("installations=%+v err=%v", got, err)
	}
}

func TestPersonaAudienceSourceRejectsForgedTenantContext(t *testing.T) {
	s := &DatabasePersonaAudienceSource{}
	if _, err := s.ListCurrentPersonaAudience(context.Background(), "tenant", "alice"); !errors.Is(err, errPersonaAudienceSourceUnavailable) {
		t.Fatalf("err=%v", err)
	}
}
