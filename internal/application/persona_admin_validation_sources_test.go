package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaAdminValidationSkills struct{ pin agentskills.SkillPin }

func (s personaAdminValidationSkills) ResolvePin(pin agentskills.SkillPin) (agentskills.SkillRecord, error) {
	if pin != s.pin {
		return agentskills.SkillRecord{}, agentskills.ErrDigestMismatch
	}
	return agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, SideEffectTier: agentskills.TierRead, DataClassesRead: []string{"WORKFORCE"}}, Digest: pin.Digest, Status: agentskills.StatusActive}, nil
}

type personaAdminValidationGrantSource struct{}

func (personaAdminValidationGrantSource) ForTenant(_ context.Context, tenant values.TenantId) (PersonaProfileGrantReader, error) {
	return personaAdminValidationGrantReader{tenant: tenant}, nil
}

type personaAdminValidationGrantReader struct{ tenant values.TenantId }

func (r personaAdminValidationGrantReader) Grants(_ context.Context, tenant values.TenantId, key agentskills.SkillKey) ([]agentgate.SkillGrant, error) {
	if tenant != r.tenant {
		return nil, errors.New("tenant mismatch")
	}
	if tenant == "ironridge" {
		return []agentgate.SkillGrant{
			{Tenant: tenant, Skill: key, ID: "ironridge-manager", Roles: []string{"manager"}, Population: "employees", OrganizationScopes: []string{"org-a"}, Purposes: []string{"persona_admin"}},
			{Tenant: tenant, Skill: key, ID: "ironridge-employee", Roles: []string{"employee"}, Population: "contractors", OrganizationScopes: []string{"org-b"}, Purposes: []string{"persona_admin"}},
		}, nil
	}
	role := "employee"
	return []agentgate.SkillGrant{{Tenant: tenant, Skill: key, ID: string(tenant), Roles: []string{role}, Population: "employees", OrganizationScopes: []string{"org-a"}, Purposes: []string{"persona_admin"}}}, nil
}

type personaAdminValidationManifestSource struct{}

func (personaAdminValidationManifestSource) ForTenant(_ context.Context, tenant values.TenantId) (PersonaStarterManifestResolver, error) {
	return personaAdminValidationManifestResolver{tenant: tenant, manifest: personaAdminValidationManifest()}, nil
}

type personaAdminValidationManifestResolver struct {
	tenant   values.TenantId
	manifest agentmanifest.Manifest
}

func (r personaAdminValidationManifestResolver) ResolveCurrentPersonaManifest(_ context.Context, id string) (agentmanifest.Manifest, error) {
	if id != r.manifest.ID {
		return agentmanifest.Manifest{}, ErrAgentManifestNotPublished
	}
	return r.manifest, nil
}

type personaAdminInstructionStoreFake struct {
	texts map[uuid.UUID]string
	seen  []uuid.UUID
}

func (s *personaAdminInstructionStoreFake) ManifestInstructions(_ context.Context, tenant uuid.UUID, id string, version uint64, digest string) (string, error) {
	s.seen = append(s.seen, tenant)
	text, ok := s.texts[tenant]
	if !ok || id != "agent.policy" || version != 1 || !personaInstructionsMatchDigest(text, digest) {
		return "", ErrAgentManifestNotPublished
	}
	return text, nil
}

func TestTodo_AGENTP_018_DatabasePersonaProfileBuilderSourceScopesBothTenants(t *testing.T) {
	pin := agentskills.SkillPin{ID: "skill.people.read", Version: 1, Digest: "digest-a"}
	source := DatabasePersonaProfileBuilderSource{Skills: personaAdminValidationSkills{pin: pin}, Grants: personaAdminValidationGrantSource{}, Manifests: personaAdminValidationManifestSource{}}
	for _, tc := range []struct {
		tenant values.TenantId
		role   string
	}{{tenant: "ironridge", role: "manager"}, {tenant: "harborcare", role: "employee"}} {
		t.Run(string(tc.tenant), func(t *testing.T) {
			ctx := personaTenantContext(t, tc.tenant)
			builder, err := source.ForTenant(ctx, tc.tenant)
			if err != nil {
				t.Fatalf("ForTenant: %v", err)
			}
			profile := personaAdminValidationProfile(pin, tc.role)
			version, err := builder.(ContextPersonaProfileBuilder).BuildForTenant(ctx, tc.tenant, profile)
			if err != nil || version.Profile.Audience.Roles[0] != tc.role || version.Digest == "" {
				t.Fatalf("tenant profile build = %+v, %v", version, err)
			}
			profile.Template = &agentpersona.TemplateProvenance{ID: "hcmnext.persona_template.policy_helper", Version: 1, Digest: "sha256:" + strings.Repeat("a", 64)}
			if _, err := builder.(ContextPersonaProfileBuilder).BuildForTenant(ctx, tc.tenant, profile); !errors.Is(err, ErrPersonaDraftInvalid) {
				t.Fatalf("counterfeit starter provenance accepted: %v", err)
			}
			profile.Template = nil
			if tc.tenant == "ironridge" {
				profile.Audience = agentpersona.Audience{Roles: []string{"manager", "employee"}, Populations: []string{"employees", "contractors"}, OrganizationScopes: []string{"org-a", "org-b"}}
				if _, err := builder.(ContextPersonaProfileBuilder).BuildForTenant(ctx, tc.tenant, profile); err == nil {
					t.Fatal("validator accepted a cross-product audience not covered by any grant tuple")
				}
			}
			if _, err := source.ForTenant(ctx, values.TenantId("other-tenant")); !errors.Is(err, errPersonaAdminValidationSources) {
				t.Fatalf("cross-tenant source error = %v", err)
			}
		})
	}
}

func TestTodo_AGENTP_018_DatabasePersonaStarterInstructionsSourceUsesTenantStoreAndDigest(t *testing.T) {
	ids := map[values.TenantId]uuid.UUID{"ironridge": uuid.MustParse("00000000-0000-0000-0000-000000000001"), "harborcare": uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	textA, textB := "Answer Ironridge policy questions.", "Answer Harborcare policy questions."
	store := &personaAdminInstructionStoreFake{texts: map[uuid.UUID]string{ids["ironridge"]: textA, ids["harborcare"]: textB}}
	source := DatabasePersonaStarterInstructionsSource{Store: store, TenantUUID: func(tenant values.TenantId) uuid.UUID { return ids[tenant] }}
	for _, tc := range []struct {
		tenant values.TenantId
		text   string
	}{{tenant: "ironridge", text: textA}, {tenant: "harborcare", text: textB}} {
		ctx := personaTenantContext(t, tc.tenant)
		resolver, err := source.ForTenant(ctx, tc.tenant)
		if err != nil {
			t.Fatalf("ForTenant(%s): %v", tc.tenant, err)
		}
		got, err := resolver.ResolvePersonaInstructions(ctx, "agent.policy", 1, personaTextDigest(tc.text))
		if err != nil || got != tc.text || store.seen[len(store.seen)-1] != ids[tc.tenant] {
			t.Fatalf("tenant %s instructions = %q, %v, store tenant %s", tc.tenant, got, err, store.seen[len(store.seen)-1])
		}
		if _, err := resolver.ResolvePersonaInstructions(personaTenantContext(t, "other-tenant"), "agent.policy", 1, personaTextDigest(tc.text)); !errors.Is(err, errPersonaAdminValidationSources) {
			t.Fatalf("foreign principal error = %v", err)
		}
	}
	ctx := personaTenantContext(t, "ironridge")
	resolver, _ := source.ForTenant(ctx, "ironridge")
	if _, err := resolver.ResolvePersonaInstructions(ctx, "agent.policy", 1, personaTextDigest(textB)); !errors.Is(err, errPersonaAdminValidationSources) {
		t.Fatalf("foreign tenant digest resolution error = %v", err)
	}
}

func personaAdminValidationManifest() agentmanifest.Manifest {
	ref := func(id string) agentmanifest.Reference {
		return agentmanifest.Reference{ID: id, Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("0", 64)}
	}
	return agentmanifest.Manifest{SchemaVersion: 1, ID: "agent.policy", Version: 1, OwnerID: "owner", Purpose: "Answer policy questions", InstructionsDigest: personaTextDigest("Answer policy questions."), SourceCeiling: []agentmanifest.Reference{}, ToolCeiling: []agentmanifest.Reference{}, ModelPolicy: ref("model.policy"), AutonomyCeiling: "ASSISTED", Budget: agentmanifest.Budget{MaxCostMicros: 1, MaxInputTokens: 1, MaxOutputTokens: 1, MaxConcurrentRuns: 1}, OutputSchema: ref("schema.answer"), ContextGrants: []agentmanifest.Reference{}, EvaluationRefs: []agentmanifest.Reference{ref("eval.policy")}}
}

func personaAdminValidationProfile(pin agentskills.SkillPin, role string) agentpersona.PersonaProfile {
	manifest := personaAdminValidationManifest()
	digest, _ := manifest.Digest()
	return agentpersona.PersonaProfile{Manifest: agentpersona.AgentManifestRef{ID: manifest.ID, Version: 1, Digest: digest, SchemaVersion: 1}, PersonaID: "persona.policy", Version: 1, Handle: "policy", DisplayName: "Policy Helper", AvatarRef: "avatar:policy", Purpose: "Answer policy questions", Audience: agentpersona.Audience{Roles: []string{role}, Populations: []string{"employees"}, OrganizationScopes: []string{"org-a"}}, SkillPins: []agentskills.SkillPin{pin}, TierCeiling: agentskills.TierRead, ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate}, Instructions: "Answer policy questions.", Owner: "owner", Steward: "steward", EvalSuiteRef: "eval.policy", EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1}}
}

func personaTextDigest(text string) string {
	digest := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(digest[:])
}
