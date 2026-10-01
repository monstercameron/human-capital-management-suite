package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona/handle"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type identityPublicationFake struct {
	version agentpersona.PersonaVersion
	err     error
}

func (f identityPublicationFake) PublishedPersona(context.Context, values.TenantId, string, uint32) (agentpersona.PersonaVersion, error) {
	if f.err != nil {
		return agentpersona.PersonaVersion{}, f.err
	}
	return f.version, nil
}

type identityAgentFake struct {
	agent chatapps.Agent
	err   error
}

func (f identityAgentFake) ActiveChatAgent(context.Context, values.TenantId, string) (chatapps.Agent, error) {
	if f.err != nil {
		return chatapps.Agent{}, f.err
	}
	return f.agent, nil
}

type identityStoreFake struct {
	identity agentpersonastore.PersonaChatIdentity
	err      error
	calls    int
}

func (f *identityStoreFake) RegisterPersonaChatIdentity(_ context.Context, tenant values.TenantId, agentID, personaID string, at time.Time) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.identity = agentpersonastore.PersonaChatIdentity{TenantID: tenant, AgentID: agentID, PersonaID: personaID, Active: true, RegisteredAt: at}
	return nil
}

func (f *identityStoreFake) LookupPersonaChatIdentity(context.Context, values.TenantId, string) (agentpersonastore.PersonaChatIdentity, error) {
	if f.identity.AgentID == "" {
		return agentpersonastore.PersonaChatIdentity{}, agentpersonastore.ErrNotFound
	}
	return f.identity, nil
}

type identityClockFake struct{ now time.Time }

func (f identityClockFake) Now() time.Time { return f.now }

func identityProfile(t *testing.T, handleValue, display string) agentpersona.PersonaVersion {
	t.Helper()
	profile := agentpersona.PersonaProfile{
		Manifest:  agentpersona.AgentManifestRef{ID: "manifest", Version: 1, Digest: "manifest-digest", SchemaVersion: 1},
		PersonaID: "persona:benefits", Version: 2, Handle: handleValue, DisplayName: display,
		AvatarRef: "avatar:benefits", Purpose: "Answer benefit policy questions",
		Audience:  agentpersona.Audience{Roles: []string{"employee"}, Populations: []string{"staff"}, OrganizationScopes: []string{"org-a"}},
		SkillPins: []agentskills.SkillPin{{ID: "benefits.read", Version: 1, Digest: "skill-digest"}}, TierCeiling: agentskills.TierRead,
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate},
		Instructions: "Use approved policy material.", Owner: "user:owner", Steward: "user:steward", EvalSuiteRef: "eval:benefits",
		EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 2, MaxLatencyMS: 500},
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func identityCoordinator(t *testing.T, publication identityPublicationFake, agent identityAgentFake, store *identityStoreFake) *PersonaIdentityRegistrationCoordinator {
	t.Helper()
	coordinator, err := NewPersonaIdentityRegistrationCoordinator(handle.NewRegistry(), publication, agent, store, identityClockFake{now: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return coordinator
}

func TestTodo_AGENTP_005_RegistrationUsesExactPublishedAgentIdentity(t *testing.T) {
	version := identityProfile(t, "benefits-agent", "Benefits Agent")
	store := &identityStoreFake{}
	service := identityCoordinator(t, identityPublicationFake{version: version}, identityAgentFake{agent: chatapps.Agent{ID: "agent:benefits", InstallationID: "install:benefits", DisplayName: "Benefits Agent", Status: chatapps.Active}}, store)
	got, err := service.Register(context.Background(), PersonaIdentityRegistrationRequest{Tenant: values.TenantId("tenant-a"), PersonaID: version.Profile.PersonaID, PersonaVersion: version.Profile.Version, AgentID: "agent:benefits"})
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentID != "agent:benefits" || got.Identity.Agent.ID != "agent:benefits" || got.Identity.PersonaID != version.Profile.PersonaID || got.Handle != "benefits-agent" || !got.Badge.IsAgent || got.Badge.AgentID != "agent:benefits" || got.Badge.ActingFor != "" {
		t.Fatalf("registration = %+v", got)
	}
	if store.calls != 1 || store.identity.PersonaID != version.Profile.PersonaID {
		t.Fatalf("durable identity = %+v calls=%d", store.identity, store.calls)
	}
}

func TestTodo_AGENTP_005_RegistrationFailsClosedAtBoundaries(t *testing.T) {
	version := identityProfile(t, "benefits-agent", "Benefits Agent")
	validAgent := identityAgentFake{agent: chatapps.Agent{ID: "agent:benefits", InstallationID: "install:benefits", DisplayName: "Benefits Agent", Status: chatapps.Active}}
	validStore := &identityStoreFake{}
	validClock := identityClockFake{now: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
	tests := []struct {
		name  string
		pub   PersonaIdentityPublicationSource
		agent PersonaIdentityAgentSource
		store PersonaIdentityDurableStore
		clock PersonaIdentityClock
		want  error
	}{
		{name: "missing publication", agent: validAgent, store: validStore, clock: validClock, want: ErrPersonaIdentityRegistrationUnavailable},
		{name: "missing active installation source", pub: identityPublicationFake{version: version}, store: validStore, clock: validClock, want: ErrPersonaIdentityRegistrationUnavailable},
		{name: "missing durable store", pub: identityPublicationFake{version: version}, agent: validAgent, clock: validClock, want: ErrPersonaIdentityRegistrationUnavailable},
		{name: "missing clock", pub: identityPublicationFake{version: version}, agent: validAgent, store: validStore, want: ErrPersonaIdentityRegistrationUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewPersonaIdentityRegistrationCoordinator(handle.NewRegistry(), tc.pub, tc.agent, tc.store, tc.clock); !errors.Is(err, tc.want) {
				t.Fatalf("constructor error=%v want=%v", err, tc.want)
			}
		})
	}
	service := identityCoordinator(t, identityPublicationFake{version: version}, validAgent, validStore)
	if _, err := service.Register(context.Background(), PersonaIdentityRegistrationRequest{Tenant: values.TenantId("tenant-a"), PersonaID: version.Profile.PersonaID, PersonaVersion: version.Profile.Version, AgentID: "agent:benefits"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Register(context.Background(), PersonaIdentityRegistrationRequest{Tenant: values.TenantId("tenant-a"), PersonaID: version.Profile.PersonaID, PersonaVersion: version.Profile.Version, AgentID: "agent:other"}); !errors.Is(err, ErrPersonaIdentityRegistrationInvalid) {
		t.Fatalf("mismatched canonical agent error=%v", err)
	}
}

func TestTodo_AGENTP_005_RegistrationRejectsUnpublishedAndConfusableHandles(t *testing.T) {
	version := identityProfile(t, "benefits-agent", "Benefits Agent")
	store := &identityStoreFake{}
	unpublished := identityCoordinator(t, identityPublicationFake{version: agentpersona.PersonaVersion{Profile: version.Profile, Digest: "forged"}}, identityAgentFake{agent: chatapps.Agent{ID: "agent:benefits", InstallationID: "install:benefits", Status: chatapps.Active}}, store)
	if _, err := unpublished.Register(context.Background(), PersonaIdentityRegistrationRequest{Tenant: values.TenantId("tenant-a"), PersonaID: version.Profile.PersonaID, PersonaVersion: version.Profile.Version, AgentID: "agent:benefits"}); !errors.Is(err, ErrPersonaIdentityNotPublished) {
		t.Fatalf("unpublished error=%v", err)
	}
	registry := handle.NewRegistry()
	if err := registry.RegisterPerson(handle.PersonRegistration{Tenant: "tenant-a", PersonID: "person:ana", Handle: "benefits-agent", DisplayName: "Ana"}); err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewPersonaIdentityRegistrationCoordinator(registry, identityPublicationFake{version: version}, identityAgentFake{agent: chatapps.Agent{ID: "agent:benefits", InstallationID: "install:benefits", Status: chatapps.Active}}, store, identityClockFake{now: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Register(context.Background(), PersonaIdentityRegistrationRequest{Tenant: values.TenantId("tenant-a"), PersonaID: version.Profile.PersonaID, PersonaVersion: version.Profile.Version, AgentID: "agent:benefits"}); !errors.Is(err, ErrPersonaIdentityRegistrationInvalid) {
		t.Fatalf("confusable handle error=%v", err)
	}
	if store.calls != 0 {
		t.Fatalf("confusable registration reached durable store %d times", store.calls)
	}
}
