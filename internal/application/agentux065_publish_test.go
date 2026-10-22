package application

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/semver"
)

// The served publish path refuses the author's bump choice when it is
// smaller than the change requires, computes the label from a sufficient
// choice, and defaults an absent choice to the required bump (AGENTUX-065).
func TestAgentUX065_PublishChoiceBelowRequiredIsRefused(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	v1 := validPersonaProfileForLifecycleTest(t, principal.Subject())
	v2profile := v1.Profile
	v2profile.Version = 2
	v2profile.SkillPins = append(append([]agentskills.SkillPin(nil), v1.Profile.SkillPins...), agentskills.SkillPin{ID: "skill-two", Version: 1, Digest: "digest-two"})
	v2, err := agentpersona.Seal(v2profile)
	if err != nil {
		t.Fatal(err)
	}
	raw1, _ := json.Marshal(v1.Profile)
	raw2, _ := json.Marshal(v2.Profile)
	newTenant := func() *personaAdminLifecycleTenantFake {
		return &personaAdminLifecycleTenantFake{
			versions: []agentpersonastore.PersonaVersion{
				{TenantID: principal.Tenant(), PersonaID: v1.Profile.PersonaID, Version: 1, Profile: raw1, ContentDigest: v1.Digest},
				{TenantID: principal.Tenant(), PersonaID: v1.Profile.PersonaID, Version: 2, Profile: raw2, ContentDigest: v2.Digest},
			},
			states: map[int64]agentpersonastore.LifecycleState{1: agentpersonastore.StatePublished, 2: agentpersonastore.StateInReview},
			labels: map[int64]string{1: "1.0.0"},
		}
	}
	newExecutor := func(tenant *personaAdminLifecycleTenantFake) *PersonaAdminLifecycleExecutor {
		return NewPersonaAdminLifecycleExecutor(personaAdminLifecycleStoreFake{tenant: tenant}, personaAdminLifecycleAuthorizerFake{}, &PersonaAdminDraftService{Profiles: &personaStarterProfileBuilderSpy{}}, nil, nil, personaAdminPublicationEvidenceFake{evidence: agentpersonastore.PublicationEvidence{ReviewID: "review", EvaluationRunID: "evaluation"}}, nil, nil, time.Now, func() string { return "publish" }, &personaAdminRuntimeProvisionerFake{})
	}
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}

	patch := semver.BumpPatch
	tenant := newTenant()
	if err := newExecutor(tenant).ExecutePersonaAdminCommand(ctx, actor, PersonaAdminCommand{Action: PersonaAdminPublish, PersonaID: v1.Profile.PersonaID, VersionBump: &patch}); !errors.Is(err, ErrPersonaAdminVersionBumpTooSmall) {
		t.Fatalf("patch choice for new skills err=%v; want ErrPersonaAdminVersionBumpTooSmall", err)
	}
	if tenant.published.EventID != "" {
		t.Fatalf("refused publish still published: %+v", tenant.published)
	}

	minor := semver.BumpMinor
	tenant = newTenant()
	if err := newExecutor(tenant).ExecutePersonaAdminCommand(ctx, actor, PersonaAdminCommand{Action: PersonaAdminPublish, PersonaID: v1.Profile.PersonaID, VersionBump: &minor}); err != nil {
		t.Fatal(err)
	}
	if tenant.published.SemanticVersion != "1.1.0" {
		t.Fatalf("minor choice sealed %q; want 1.1.0", tenant.published.SemanticVersion)
	}

	tenant = newTenant()
	if err := newExecutor(tenant).ExecutePersonaAdminCommand(ctx, actor, PersonaAdminCommand{Action: PersonaAdminPublish, PersonaID: v1.Profile.PersonaID}); err != nil {
		t.Fatal(err)
	}
	if tenant.published.SemanticVersion != "1.1.0" {
		t.Fatalf("absent choice sealed %q; want required 1.1.0", tenant.published.SemanticVersion)
	}
}

// The command boundary carries the author's plain bump choice to a publish
// and refuses anything but the three choices, or a choice on any other
// action (AGENTUX-065).
func TestAgentUX065_PublishChoiceCommandBoundary(t *testing.T) {
	ctx, _ := personaAdminCommandContext(t)
	authorizer := &personaAdminCommandAuthFake{}
	executor := &personaAdminCommandExecutorFake{}
	factory, err := NewPersonaAdminCommandFactory(personaAdminCommandCatalogFake{}, authorizer, executor)
	if err != nil {
		t.Fatal(err)
	}
	minor := "minor"
	if err := factory.ExecutePersonaAdminCommand(ctx, productui.PersonaAdminCommandRequest{Action: string(PersonaAdminPublish), PersonaID: "assistant", ReviewID: "review", EvaluationRunID: "evaluation", VersionBump: &minor}); err != nil {
		t.Fatal(err)
	}
	if executor.command.VersionBump == nil || *executor.command.VersionBump != semver.BumpMinor {
		t.Fatalf("publish choice = %+v; want minor", executor.command.VersionBump)
	}
	bad := "rewrite"
	if err := factory.ExecutePersonaAdminCommand(ctx, productui.PersonaAdminCommandRequest{Action: string(PersonaAdminPublish), PersonaID: "assistant", ReviewID: "review", EvaluationRunID: "evaluation", VersionBump: &bad}); err == nil {
		t.Fatal("an unknown bump choice was accepted")
	}
	if err := factory.ExecutePersonaAdminCommand(ctx, productui.PersonaAdminCommandRequest{Action: string(PersonaAdminSuspend), PersonaID: "assistant", VersionBump: &minor}); err == nil {
		t.Fatal("a bump choice on suspend was accepted")
	}
}
