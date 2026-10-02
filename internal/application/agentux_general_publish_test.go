package application

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
)

func TestAgentUXGeneral_PublishRequiresRuntime(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	profile, err := agentpersona.Seal(agentpersona.PersonaProfile{
		Manifest: agentpersona.AgentManifestRef{ID: "agent.starter.assistant", Version: 1, SchemaVersion: 1, Digest: "manifest"}, PersonaID: "assistant", Version: 1,
		Handle: "assistant", DisplayName: "Assistant", AvatarRef: "avatar", Purpose: "Answer questions", Instructions: "Answer briefly.", Owner: principal.Subject(), Steward: "steward", EvalSuiteRef: localAgentDemoAssistantSuiteID,
		Audience: agentpersona.Audience{Roles: []string{"staff"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org"}}, SkillPins: []agentskills.SkillPin{{ID: personaPolicyHelperSkillID, Version: 1, Digest: "digest"}}, TierCeiling: agentskills.TierRead,
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate}, EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(profile.Profile)
	tenant := &personaAdminLifecycleTenantFake{versions: []agentpersonastore.PersonaVersion{{TenantID: principal.Tenant(), PersonaID: "assistant", Version: 1, Profile: raw, ContentDigest: profile.Digest}}, states: map[int64]agentpersonastore.LifecycleState{1: agentpersonastore.StateInReview}}
	executor := NewPersonaAdminLifecycleExecutor(personaAdminLifecycleStoreFake{tenant: tenant}, personaAdminLifecycleAuthorizerFake{}, &PersonaAdminDraftService{Profiles: &personaStarterProfileBuilderSpy{}}, nil, nil, personaAdminPublicationEvidenceFake{evidence: agentpersonastore.PublicationEvidence{ReviewID: "review", EvaluationRunID: "evaluation"}}, nil, nil, func() time.Time { return time.Unix(200, 0) }, func() string { return "publish" })
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	err = executor.ExecutePersonaAdminCommand(ctx, actor, PersonaAdminCommand{Action: PersonaAdminPublish, PersonaID: "assistant"})
	if !errors.Is(err, ErrPersonaAdminRuntimeUnavailable) || tenant.states[1] != agentpersonastore.StateInReview || tenant.published.EventID != "" {
		t.Fatalf("publish without runtime err=%v state=%s event=%+v", err, tenant.states[1], tenant.published)
	}
	if executor.PersonaAdminCommandAvailable(ctx, PersonaAdminPublish) {
		t.Fatal("publish was advertised without a runtime provisioner")
	}
}

func TestAgentUXGeneral_PublishProvisioningFailureIsFailClosed(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	profile := validPersonaProfileForLifecycleTest(t, principal.Subject())
	raw, _ := json.Marshal(profile.Profile)
	tenant := &personaAdminLifecycleTenantFake{versions: []agentpersonastore.PersonaVersion{{TenantID: principal.Tenant(), PersonaID: profile.Profile.PersonaID, Version: int64(profile.Profile.Version), Profile: raw, ContentDigest: profile.Digest}}, states: map[int64]agentpersonastore.LifecycleState{int64(profile.Profile.Version): agentpersonastore.StateInReview}}
	runtime := &personaAdminRuntimeProvisionerFake{err: errors.New("route authority unavailable")}
	executor := NewPersonaAdminLifecycleExecutor(personaAdminLifecycleStoreFake{tenant: tenant}, personaAdminLifecycleAuthorizerFake{}, &PersonaAdminDraftService{Profiles: &personaStarterProfileBuilderSpy{}}, nil, nil, personaAdminPublicationEvidenceFake{evidence: agentpersonastore.PublicationEvidence{ReviewID: "review", EvaluationRunID: "evaluation"}}, nil, nil, time.Now, func() string { return "publish" }, runtime)
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	err := executor.ExecutePersonaAdminCommand(ctx, actor, PersonaAdminCommand{Action: PersonaAdminPublish, PersonaID: profile.Profile.PersonaID})
	if !errors.Is(err, ErrPersonaAdminRuntimeUnavailable) || tenant.states[int64(profile.Profile.Version)] != agentpersonastore.StateInReview || !runtime.called {
		t.Fatalf("failed provisioning err=%v state=%s called=%t", err, tenant.states[int64(profile.Profile.Version)], runtime.called)
	}
}

func validPersonaProfileForLifecycleTest(t *testing.T, owner string) agentpersona.PersonaVersion {
	t.Helper()
	profile, err := agentpersona.Seal(agentpersona.PersonaProfile{Manifest: agentpersona.AgentManifestRef{ID: "agent", Version: 1, SchemaVersion: 1, Digest: "manifest"}, PersonaID: "assistant", Version: 1, Handle: "assistant", DisplayName: "Assistant", AvatarRef: "avatar", Purpose: "Answer questions", Audience: agentpersona.Audience{Roles: []string{"staff"}, Populations: []string{"staff"}, OrganizationScopes: []string{"org"}}, SkillPins: []agentskills.SkillPin{{ID: "skill", Version: 1, Digest: "digest"}}, TierCeiling: agentskills.TierRead, ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate}, Instructions: "Answer.", Owner: owner, Steward: "steward", EvalSuiteRef: "suite", EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1}})
	if err != nil {
		t.Fatal(err)
	}
	return profile
}
