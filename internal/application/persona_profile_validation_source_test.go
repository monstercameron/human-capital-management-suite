package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaProfileBuilderSourceFake struct {
	builder PersonaProfileBuilder
	tenant  values.TenantId
	calls   int
	err     error
}

func (f *personaProfileBuilderSourceFake) ForTenant(_ context.Context, tenant values.TenantId) (PersonaProfileBuilder, error) {
	f.calls++
	f.tenant = tenant
	return f.builder, f.err
}

type personaTenantProfileBuilderFake struct{ calls int }

func (f *personaTenantProfileBuilderFake) Build(agentpersona.PersonaProfile) (agentpersona.PersonaVersion, error) {
	f.calls++
	return agentpersona.PersonaVersion{Digest: "validated"}, nil
}

func TestPersonaProfileValidationSource_ResolvesOnlyAuthenticatedTenant(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	builder := &personaTenantProfileBuilderFake{}
	source := &personaProfileBuilderSourceFake{builder: builder}
	bound := TenantPersonaProfileBuilder{Source: source}
	result, err := bound.BuildForTenant(ctx, principal.Tenant(), agentpersona.PersonaProfile{})
	if err != nil || result.Digest != "validated" || source.calls != 1 || source.tenant != principal.Tenant() || builder.calls != 1 {
		t.Fatalf("tenant validator result=%+v err=%v source=%+v builder=%+v", result, err, source, builder)
	}
	if _, err := buildPersonaProfile(ctx, bound, principal.Tenant(), agentpersona.PersonaProfile{}); err != nil || source.calls != 2 {
		t.Fatalf("shared context-aware build error = %v calls=%d", err, source.calls)
	}
	if _, err := bound.BuildForTenant(ctx, values.TenantId("tenant-b"), agentpersona.PersonaProfile{}); !errors.Is(err, errPersonaProfileValidationUnavailable) {
		t.Fatalf("cross-tenant build error = %v", err)
	}
	if source.calls != 2 {
		t.Fatalf("cross-tenant build reached source: %d calls", source.calls)
	}
}

func TestPersonaProfileValidationSource_RejectsContextlessAndUnavailableValidators(t *testing.T) {
	bound := TenantPersonaProfileBuilder{}
	if _, err := bound.Build(agentpersona.PersonaProfile{}); !errors.Is(err, errPersonaProfileValidationUnavailable) {
		t.Fatalf("contextless validation error = %v", err)
	}
	ctx, principal := personaAdminCommandContext(t)
	if _, err := bound.BuildForTenant(ctx, principal.Tenant(), agentpersona.PersonaProfile{}); !errors.Is(err, errPersonaProfileValidationUnavailable) {
		t.Fatalf("missing source error = %v", err)
	}
	broken := TenantPersonaProfileBuilder{Source: &personaProfileBuilderSourceFake{err: errors.New("resolver failure")}}
	if _, err := broken.BuildForTenant(ctx, principal.Tenant(), agentpersona.PersonaProfile{}); !errors.Is(err, errPersonaProfileValidationUnavailable) {
		t.Fatalf("failed source error = %v", err)
	}
}

func TestPersonaProfileValidationSource_IsolatesIronridgeAndHarborcareValidators(t *testing.T) {
	resolved := make(map[values.TenantId]int)
	source := PersonaProfileBuilderSourceFunc(func(_ context.Context, tenant values.TenantId) (PersonaProfileBuilder, error) {
		resolved[tenant]++
		return tenantBoundProfileBuilder{tenant: tenant}, nil
	})
	bound := TenantPersonaProfileBuilder{Source: source}
	for _, tenant := range []values.TenantId{"ironridge", "harborcare"} {
		ctx := personaTenantContext(t, tenant)
		version, err := bound.BuildForTenant(ctx, tenant, agentpersona.PersonaProfile{})
		if err != nil || version.Digest != string(tenant) {
			t.Fatalf("tenant %q validation = %+v, %v", tenant, version, err)
		}
	}
	if resolved["ironridge"] != 1 || resolved["harborcare"] != 1 || len(resolved) != 2 {
		t.Fatalf("validator resolution counts = %#v", resolved)
	}
}

func TestPersonaProfileValidationSource_BindsStarterResolversToTenant(t *testing.T) {
	manifest := personaStarterManifest("Answer approved questions.")
	var manifestTenant, instructionsTenant values.TenantId
	manifestSource := PersonaStarterManifestResolverSourceFunc(func(_ context.Context, tenant values.TenantId) (PersonaStarterManifestResolver, error) {
		manifestTenant = tenant
		return &personaStarterManifestFake{manifest: manifest}, nil
	})
	instructionsSource := PersonaStarterInstructionsResolverSourceFunc(func(_ context.Context, tenant values.TenantId) (PersonaStarterInstructionsResolver, error) {
		instructionsTenant = tenant
		return &personaStarterInstructionsFake{text: "Answer approved questions."}, nil
	})
	ctx := personaTenantContext(t, "harborcare")
	gotManifest, err := (TenantPersonaStarterManifestResolver{Source: manifestSource}).ResolveCurrentPersonaManifest(ctx, manifest.ID)
	if err != nil || gotManifest.ID != manifest.ID || manifestTenant != "harborcare" {
		t.Fatalf("tenant manifest resolution = %+v, %v, tenant %q", gotManifest, err, manifestTenant)
	}
	gotInstructions, err := (TenantPersonaStarterInstructionsResolver{Source: instructionsSource}).ResolvePersonaInstructions(ctx, manifest.ID, manifest.Version, manifest.InstructionsDigest)
	if err != nil || gotInstructions != "Answer approved questions." || instructionsTenant != "harborcare" {
		t.Fatalf("tenant instructions = %q, %v, tenant %q", gotInstructions, err, instructionsTenant)
	}
	if _, err := (TenantPersonaStarterManifestResolver{}).ResolveCurrentPersonaManifest(ctx, manifest.ID); !errors.Is(err, errPersonaProfileValidationUnavailable) {
		t.Fatalf("missing manifest source error = %v", err)
	}
	if _, err := (TenantPersonaStarterInstructionsResolver{}).ResolvePersonaInstructions(ctx, manifest.ID, manifest.Version, manifest.InstructionsDigest); !errors.Is(err, errPersonaProfileValidationUnavailable) {
		t.Fatalf("missing instructions source error = %v", err)
	}
}

type tenantBoundProfileBuilder struct{ tenant values.TenantId }

func (b tenantBoundProfileBuilder) Build(agentpersona.PersonaProfile) (agentpersona.PersonaVersion, error) {
	return agentpersona.PersonaVersion{Digest: string(b.tenant)}, nil
}

func personaTenantContext(t *testing.T, tenant values.TenantId) context.Context {
	t.Helper()
	now := time.Unix(500, 0).UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: tenant, Subject: "user:tenant-admin", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-tenant", IssuedAt: now, ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:credential"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), principal)
}
