package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_AGENTP_018_StarterSourceRejectsMissingAndForgedTuple(t *testing.T) {
	ctx, _ := catalogContext(t)
	source := readyPersonaAdminStarterSource(t, false)
	for _, tc := range []struct {
		name string
		req  productui.PersonaAdminSnapshotRequest
		ctx  context.Context
	}{
		{name: "missing tuple", ctx: ctx},
		{name: "forged principal", ctx: ctx, req: productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "attacker"}},
		{name: "forged tenant", ctx: ctx, req: productui.PersonaAdminSnapshotRequest{TenantID: "tenant-b", Principal: "user-a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := source.PersonaAdminStarterCatalog(tc.ctx, tc.req)
			if !errors.Is(err, ErrPersonaCatalogDenied) {
				t.Fatalf("catalog error = %v, want denial", err)
			}
		})
	}
}

func TestTodo_AGENTP_018_StarterSourceRequiresCurrentManifestAndEveryGrant(t *testing.T) {
	ctx, _ := catalogContext(t)
	for _, tc := range []struct {
		name         string
		missingRef   bool
		missingGrant bool
	}{
		{name: "missing manifest", missingRef: true},
		{name: "missing one pin grant", missingGrant: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := readyPersonaAdminStarterSource(t, tc.missingGrant)
			if tc.missingRef {
				source.Manifests = starterManifestSourceFake{missing: true}
			}
			catalog, err := source.PersonaAdminStarterCatalog(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
			if err != nil {
				t.Fatal(err)
			}
			if len(catalog.Starters) != 0 {
				t.Fatalf("unready templates exposed: %+v", catalog)
			}
		})
	}
}

func TestTodo_AGENTP_018_StarterSourceProjectsReadyTenantStarters(t *testing.T) {
	ctx, _ := catalogContext(t)
	source := readyPersonaAdminStarterSource(t, false)
	catalog, err := source.PersonaAdminStarterCatalog(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
	if err != nil {
		t.Fatal(err)
	}
	if !catalog.Available || len(catalog.Starters) != len(agenttemplate.PersonaStarters()) {
		t.Fatalf("ready catalog = %+v", catalog)
	}
	for _, projected := range catalog.Starters {
		starter, ok := agenttemplate.PersonaStarterFor(projected.ID, projected.Version)
		if !ok || projected.ManifestID != personaAdminStarterManifestID(starter.ID) || len(projected.SkillGrantIDs) != len(starter.SkillPins) {
			t.Fatalf("unverified projection: %+v", projected)
		}
		if len(projected.ChannelClasses) == 0 {
			t.Fatalf("starter has no bounded channel classes: %+v", projected)
		}
	}
}

func TestTodo_AGENTP_018_StarterSourcePreservesGrantAudienceTuples(t *testing.T) {
	ctx, _ := catalogContext(t)
	source := readyPersonaAdminStarterSource(t, false)
	source.Grants = starterGrantSourceFake{fragmentStarterAudience: true}
	catalog, err := source.PersonaAdminStarterCatalog(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
	if err != nil {
		t.Fatal(err)
	}
	for _, starter := range catalog.Starters {
		if starter.ID == "hcmnext.persona_template.onboarding_coordinator" {
			t.Fatalf("starter became ready by combining separate grant tuples: %+v", starter)
		}
	}
	if len(catalog.Starters) != len(agenttemplate.PersonaStarters())-1 {
		t.Fatalf("unexpected starters after tuple-preserving filter: %+v", catalog.Starters)
	}
}

func TestTodo_AGENTP_018_StarterSourceDeniesCrossTenantPrincipal(t *testing.T) {
	ctx, principal := catalogContext(t)
	other, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-b", Subject: "user-b", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-b", IssuedAt: time.Date(2026, 9, 29, 11, 59, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 9, 29, 13, 0, 0, 0, time.UTC), CredentialDigest: "cred:sha256:b"})
	if err != nil {
		t.Fatal(err)
	}
	_ = principal
	ctx = trust.WithPrincipal(ctx, other)
	source := readyPersonaAdminStarterSource(t, false)
	catalog, err := source.PersonaAdminStarterCatalog(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
	if catalog.Available || len(catalog.Starters) != 0 || !errors.Is(err, ErrPersonaCatalogDenied) {
		t.Fatalf("cross-tenant result = %+v, %v", catalog, err)
	}
}

func readyPersonaAdminStarterSource(t *testing.T, missingGrant bool) *PersonaAdminStarterSource {
	t.Helper()
	manifests := starterManifestSourceFake{manifests: map[string]agentmanifest.Manifest{}, instructions: map[string]string{}}
	for _, starter := range agenttemplate.PersonaStarters() {
		id := personaAdminStarterManifestID(starter.ID)
		text := personaStarterInstructions(starter)
		manifests.manifests[id] = agentmanifest.Manifest{
			SchemaVersion: agentmanifest.CurrentSchemaVersion, ID: id, Version: 1, OwnerID: "tenant-owner", Purpose: starter.Purpose,
			InstructionsDigest: personaInstructionDigest(text), SourceCeiling: []agentmanifest.Reference{}, ToolCeiling: []agentmanifest.Reference{},
			ModelPolicy: starterTestRef("model"), AutonomyCeiling: "ASSISTED",
			Budget:       agentmanifest.Budget{MaxCostMicros: 100, MaxInputTokens: 4000, MaxOutputTokens: 1000, MaxConcurrentRuns: 1},
			OutputSchema: starterTestRef("output"), ContextGrants: []agentmanifest.Reference{},
			EvaluationRefs: []agentmanifest.Reference{starterTestRef(starter.EvaluationSuite)},
		}
		manifests.instructions[id] = text
	}
	grantSource := starterGrantSourceFake{missing: missingGrant}
	source, err := NewPersonaAdminStarterSource(starterSkillsFake{}, manifests, starterInstructionSourceFake{source: manifests}, grantSource, &catalogAuth{})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func starterTestRef(id string) agentmanifest.Reference {
	return agentmanifest.Reference{ID: id, Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("a", 64)}
}

type starterSkillsFake struct{}

func (starterSkillsFake) ResolvePin(pin agentskills.SkillPin) (agentskills.SkillRecord, error) {
	return agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, SideEffectTier: agentskills.TierRead}, Digest: pin.Digest, Status: agentskills.StatusActive}, nil
}

type starterManifestSourceFake struct {
	manifests    map[string]agentmanifest.Manifest
	instructions map[string]string
	missing      bool
}

func (f starterManifestSourceFake) ForTenant(ctx context.Context, tenant values.TenantId) (PersonaStarterManifestResolver, error) {
	if f.missing || tenant != "tenant-a" || !trustedStarterContext(ctx, tenant) {
		return nil, errPersonaAdminStarterSource
	}
	return f, nil
}

func (f starterManifestSourceFake) ResolveCurrentPersonaManifest(_ context.Context, id string) (agentmanifest.Manifest, error) {
	manifest, ok := f.manifests[id]
	if !ok {
		return agentmanifest.Manifest{}, errPersonaAdminStarterSource
	}
	return manifest, nil
}

type starterInstructionSourceFake struct{ source starterManifestSourceFake }

func (f starterInstructionSourceFake) ForTenant(ctx context.Context, tenant values.TenantId) (PersonaStarterInstructionsResolver, error) {
	if tenant != "tenant-a" || !trustedStarterContext(ctx, tenant) {
		return nil, errPersonaAdminStarterSource
	}
	return f, nil
}

func (f starterInstructionSourceFake) ResolvePersonaInstructions(_ context.Context, id string, version uint64, digest string) (string, error) {
	manifest, ok := f.source.manifests[id]
	if !ok || version != manifest.Version || digest != manifest.InstructionsDigest {
		return "", errPersonaAdminStarterSource
	}
	return f.source.instructions[id], nil
}

type starterGrantSourceFake struct {
	missing                 bool
	fragmentStarterAudience bool
}

func (f starterGrantSourceFake) ForTenant(ctx context.Context, tenant values.TenantId) (PersonaProfileGrantReader, error) {
	if tenant != "tenant-a" || !trustedStarterContext(ctx, tenant) {
		return nil, errPersonaAdminStarterSource
	}
	return f, nil
}

func (f starterGrantSourceFake) Grants(_ context.Context, tenant values.TenantId, key agentskills.SkillKey) ([]agentgate.SkillGrant, error) {
	if f.missing {
		return nil, nil
	}
	if f.fragmentStarterAudience && key.ID == agenttemplate.PersonaStarters()[0].SkillPins[0].ID {
		return []agentgate.SkillGrant{
			{ID: "grant-fragment-hr", Tenant: tenant, Skill: key, Roles: []string{"HR"}, Population: "NEW_HIRES", OrganizationScopes: []string{"org-a"}, Purposes: []string{"persona"}},
			{ID: "grant-fragment-manager", Tenant: tenant, Skill: key, Roles: []string{"MANAGER"}, Population: "NEW_HIRES", OrganizationScopes: []string{"org-b"}, Purposes: []string{"persona"}},
		}, nil
	}
	return []agentgate.SkillGrant{{ID: "grant-" + key.ID, Tenant: tenant, Skill: key, Roles: []string{agentgate.AnyScope}, Population: agentgate.AnyScope, OrganizationScopes: []string{agentgate.AnyScope}, Purposes: []string{"persona"}}}, nil
}

func trustedStarterContext(ctx context.Context, tenant values.TenantId) bool {
	p, ok := trust.FromContext(ctx)
	return ok && p != nil && p.SubjectKind() == trust.SubjectKindHuman && p.Tenant() == tenant
}
