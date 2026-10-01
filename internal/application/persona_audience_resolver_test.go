package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaAudienceSourceFake struct {
	conversations []PersonaAudienceConversation
	installations map[string][]PersonaAudienceInstallation
	err           error
}

func (f personaAudienceSourceFake) ListCurrentPersonaAudience(context.Context, string, string) ([]PersonaAudienceConversation, error) {
	return f.conversations, f.err
}

func (f personaAudienceSourceFake) ListCurrentPersonaInstallations(_ context.Context, _, conversation string) ([]PersonaAudienceInstallation, error) {
	return f.installations[conversation], f.err
}

func audiencePrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant", Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "credential"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTodo_AGENTP_019_CurrentPersonaAudienceResolvesCurrentMemberAudience(t *testing.T) {
	principal := audiencePrincipal(t)
	wanted := agentpersonastore.AvailableInstallation{PersonaID: "persona", PersonaVersion: 2, InstallationID: "install", ConversationID: "room"}
	source := personaAudienceSourceFake{
		conversations: []PersonaAudienceConversation{{TenantID: "tenant", ConversationID: "room", Members: []PersonaAudienceMember{{SubjectID: "alice", Roles: []string{"member"}, Populations: []string{"staff"}, OrganizationScope: "org"}}}},
		installations: map[string][]PersonaAudienceInstallation{"room": {{Tuple: wanted, Active: true, CurrentVersion: true, AudienceRoles: []string{"member"}, AudiencePopulations: []string{"staff"}, AudienceScopes: []string{"org"}}}},
	}
	got, err := (&CurrentPersonaAudience{Source: source}).ResolveAvailablePersonaInstallations(trust.WithPrincipal(context.Background(), principal), principal)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != wanted {
		t.Fatalf("got %+v, want %+v", got, wanted)
	}
}

func TestTodo_AGENTP_019_CurrentPersonaAudienceRejectsUnverifiedAndMalformedMembership(t *testing.T) {
	principal := audiencePrincipal(t)
	resolver := &CurrentPersonaAudience{Source: personaAudienceSourceFake{conversations: []PersonaAudienceConversation{{TenantID: "tenant", ConversationID: "room"}}}}
	if _, err := resolver.ResolveAvailablePersonaInstallations(context.Background(), principal); !errors.Is(err, errPersonaAudienceUnavailable) {
		t.Fatalf("unverified error = %v", err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	if _, err := resolver.ResolveAvailablePersonaInstallations(ctx, principal); !errors.Is(err, errPersonaAudienceUnavailable) {
		t.Fatalf("malformed membership error = %v", err)
	}
}

func TestTodo_AGENT2_016_CurrentPersonaAudienceOmitsIneligibleAndStaleInstallations(t *testing.T) {
	principal := audiencePrincipal(t)
	source := personaAudienceSourceFake{
		conversations: []PersonaAudienceConversation{{TenantID: "tenant", ConversationID: "room", Members: []PersonaAudienceMember{{SubjectID: "alice", Roles: []string{"member"}}}}},
		installations: map[string][]PersonaAudienceInstallation{"room": {
			{Tuple: agentpersonastore.AvailableInstallation{PersonaID: "stale", PersonaVersion: 1, InstallationID: "s", ConversationID: "room"}, Active: true, CurrentVersion: false, AudienceRoles: []string{"member"}},
			{Tuple: agentpersonastore.AvailableInstallation{PersonaID: "hidden", PersonaVersion: 1, InstallationID: "h", ConversationID: "room"}, Active: true, CurrentVersion: true, AudienceRoles: []string{"manager"}},
		}},
	}
	got, err := (&CurrentPersonaAudience{Source: source}).ResolveAvailablePersonaInstallations(trust.WithPrincipal(context.Background(), principal), principal)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got ineligible installations: %+v", got)
	}
}
