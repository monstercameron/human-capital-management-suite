package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type authorityInstallStoreFake struct {
	reader PersonaAuthorityInstallationReader
}

func (f authorityInstallStoreFake) ForTenant(context.Context, values.TenantId) (PersonaAuthorityInstallationReader, error) {
	return f.reader, nil
}

type authorityInstallReaderFake struct {
	version  agentpersonastore.PersonaVersion
	current  agentpersonastore.ActiveInstallation
	versions []agentpersonastore.PersonaVersion
	active   []agentpersonastore.ActiveInstallation
}

func (f authorityInstallReaderFake) ReadCurrentPersonaAuthority(context.Context, string, string) (agentpersonastore.PersonaVersion, agentpersonastore.ActiveInstallation, error) {
	return f.version, f.current, nil
}

type authorityAudienceFake struct {
	conversations []PersonaAudienceConversation
	installations []PersonaAudienceInstallation
}

func (f authorityAudienceFake) ListCurrentPersonaAudience(context.Context, string, string) ([]PersonaAudienceConversation, error) {
	return f.conversations, nil
}
func (f authorityAudienceFake) ListCurrentPersonaInstallations(context.Context, string, string) ([]PersonaAudienceInstallation, error) {
	return f.installations, nil
}

type authorityScopesFake struct{ err error }

func (f authorityScopesFake) ResolvePersonaScopes(context.Context, values.TenantId, agentpersona.PersonaProfile, agentpersonastore.ActiveInstallation, agentpersonastore.ChannelPolicy) (agentinvoke.SkillScopes, agentinvoke.SkillScopes, agentinvoke.SkillScopes, error) {
	if f.err != nil {
		return nil, nil, nil, f.err
	}
	return agentinvoke.SkillScopes{"read": {"worker"}}, agentinvoke.SkillScopes{"read": {"worker"}}, agentinvoke.SkillScopes{"read": {"worker"}}, nil
}

func authorityContext(t *testing.T) context.Context {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "user-a", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "credential"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), p)
}

func authorityProfile(t *testing.T) (agentpersonastore.PersonaVersion, agentpersona.PersonaProfile) {
	t.Helper()
	profile := agentpersona.PersonaProfile{
		Manifest:  agentpersona.AgentManifestRef{ID: "agent", Version: 1, Digest: "manifest", SchemaVersion: 1},
		PersonaID: "persona-a", Version: 2, Handle: "helper", DisplayName: "Helper", AvatarRef: "avatar", Purpose: "Read worker records", Instructions: "Answer from records.", InstructionsDigest: "", Owner: "owner", Steward: "steward", EvalSuiteRef: "eval",
		Audience:  agentpersona.Audience{Roles: []string{"manager"}, Populations: []string{"staff"}, OrganizationScopes: []string{"org-a"}},
		SkillPins: []agentskills.SkillPin{{ID: "skill.read", Version: 1, Digest: "skill-digest"}}, TierCeiling: agentskills.TierRead,
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate},
		EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1},
	}
	// The source's integrity check is intentionally exercised with the sealed digest.
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	return agentpersonastore.PersonaVersion{TenantID: values.TenantId("tenant-a"), PersonaID: "persona-a", Version: 2, AgentVersion: "agent-v1", Profile: b, ContentDigest: sealed.Digest}, profile
}

func TestTodo_AGENTP_008_DatabasePersonaAuthoritySource(t *testing.T) {
	version, _ := authorityProfile(t)
	active := agentpersonastore.ActiveInstallation{InstallationID: "install-a", PersonaID: "persona-a", PersonaVersion: 2, ConversationID: "room-a", ChannelPolicy: agentpersonastore.ChannelPolicy{MaxTier: "T0"}}
	source := &DatabasePersonaAuthoritySource{
		Installations: authorityInstallStoreFake{reader: authorityInstallReaderFake{version: version, current: active}},
		Audience: authorityAudienceFake{
			conversations: []PersonaAudienceConversation{{TenantID: "tenant-a", ConversationID: "room-a", Members: []PersonaAudienceMember{{SubjectID: "user-a", Roles: []string{"manager"}, Populations: []string{"staff"}, OrganizationScope: "org-a"}}}},
			installations: []PersonaAudienceInstallation{{Tuple: agentpersonastore.AvailableInstallation{PersonaID: "persona-a", PersonaVersion: 2, InstallationID: "install-a", ConversationID: "room-a"}, Active: true, CurrentVersion: true, AudienceRoles: []string{"manager"}, AudiencePopulations: []string{"staff"}, AudienceScopes: []string{"org-a"}}},
		},
		Scopes: authorityScopesFake{},
	}
	got, err := source.ResolvePersonaAuthority(authorityContext(t), agentinvoke.AdmissionRequest{TenantID: "tenant-a", ConversationID: "room-a", InvokerID: "user-a", PersonaID: "persona-a"}, TrustedInvoker{ID: "user-a", TenantID: "tenant-a", Discoverable: agentinvoke.SkillScopes{"read": {"worker"}}})
	if err != nil {
		t.Fatalf("ResolvePersonaAuthority: %v", err)
	}
	if !got.Persona.Current || !got.Installation.Current || !got.PersonaInstalled || !got.AudienceMember || got.Persona.Version != "2" || got.Installation.ID != "install-a" {
		t.Fatalf("admission = %+v", got)
	}
	if !agentinvoke.SkillScopesSubset(got.Persona.PinnedSkills, got.Installation.SkillCeiling) || got.Discoverable == nil {
		t.Fatalf("authority scopes = %+v", got)
	}
}

func TestTodo_AGENTP_008_DatabasePersonaAuthoritySourceFailsClosed(t *testing.T) {
	version, _ := authorityProfile(t)
	base := &DatabasePersonaAuthoritySource{
		Installations: authorityInstallStoreFake{reader: authorityInstallReaderFake{version: version, current: agentpersonastore.ActiveInstallation{InstallationID: "install-a", PersonaID: "persona-a", PersonaVersion: 2, ConversationID: "room-a"}}},
		Audience:      authorityAudienceFake{conversations: []PersonaAudienceConversation{{TenantID: "tenant-a", ConversationID: "room-a", Members: []PersonaAudienceMember{{SubjectID: "user-a", Roles: []string{"manager"}, Populations: []string{"staff"}, OrganizationScope: "org-a"}}}}, installations: []PersonaAudienceInstallation{{Tuple: agentpersonastore.AvailableInstallation{PersonaID: "persona-a", PersonaVersion: 2, InstallationID: "install-a", ConversationID: "room-a"}, Active: true, CurrentVersion: true}}},
		Scopes:        authorityScopesFake{},
	}
	tests := []struct {
		name   string
		mutate func(*DatabasePersonaAuthoritySource, *agentinvoke.AdmissionRequest, *TrustedInvoker)
	}{
		{name: "missing scope source", mutate: func(s *DatabasePersonaAuthoritySource, _ *agentinvoke.AdmissionRequest, _ *TrustedInvoker) {
			s.Scopes = nil
		}},
		{name: "wrong tenant binding", mutate: func(_ *DatabasePersonaAuthoritySource, _ *agentinvoke.AdmissionRequest, i *TrustedInvoker) {
			i.TenantID = "tenant-b"
		}},
		{name: "not a member", mutate: func(_ *DatabasePersonaAuthoritySource, _ *agentinvoke.AdmissionRequest, i *TrustedInvoker) {
			i.ID = "user-b"
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := *base
			req := agentinvoke.AdmissionRequest{TenantID: "tenant-a", ConversationID: "room-a", InvokerID: "user-a", PersonaID: "persona-a"}
			invoker := TrustedInvoker{ID: "user-a", TenantID: "tenant-a"}
			tc.mutate(&s, &req, &invoker)
			if _, err := s.ResolvePersonaAuthority(authorityContext(t), req, invoker); err == nil || !errors.Is(err, errPersonaAuthoritySourceUnavailable) {
				t.Fatalf("err=%v, want fail-closed source error", err)
			}
		})
	}
}
