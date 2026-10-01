package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type localDraftBootstrapDB struct{ begins int }

func (d *localDraftBootstrapDB) Begin(context.Context) (dbport.Tx, error) {
	d.begins++
	return nil, errors.New("unexpected bootstrap write")
}

type localDraftBootstrapDirectory struct {
	reads  int
	member *PersonaAudienceMember
}

func (d *localDraftBootstrapDirectory) ResolvePersonaAudienceMember(context.Context, string, string) (PersonaAudienceMember, error) {
	d.reads++
	if d.member != nil {
		return *d.member, nil
	}
	return PersonaAudienceMember{}, ErrPersonaAudienceDirectoryUnavailable
}

type localDraftBootstrapVersions struct {
	row   agentpersonastore.PersonaVersion
	state agentpersonastore.LifecycleState
}

func (s localDraftBootstrapVersions) GetVersion(context.Context, values.TenantId, string, int64) (agentpersonastore.PersonaVersion, error) {
	return s.row, nil
}
func (s localDraftBootstrapVersions) ListVersions(context.Context, values.TenantId, string) ([]agentpersonastore.PersonaVersion, error) {
	return []agentpersonastore.PersonaVersion{s.row}, nil
}
func (s localDraftBootstrapVersions) Lifecycle(context.Context, values.TenantId, string, int64) (agentpersonastore.LifecycleState, error) {
	return s.state, nil
}

func TestTodo_AGENTP_023_LocalPolicyDraftPreservesCurrentVersion(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	tenant := values.TenantId("ironridge-demo")
	owner, steward := "ir-001-walt-brennan", "ir-003-loretta-haynes"
	starter, _ := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	refs, err := resolveLocalDevPersonaStarterReferences(context.Background(), personaStarterProvisionReferencesFake{}, starter)
	if err != nil {
		t.Fatal(err)
	}
	instructions := personaStarterInstructions(starter)
	manifest := localDevPersonaStarterManifest(starter, instructions, refs)
	manifest.Version = 2
	manifest.Budget.MaxCostMicros = 10000
	profile := personaStarterProfile(starter, PersonaStarterDraftRequest{PersonaID: "hcmnext.local.persona.policy_helper", AvatarRef: "avatar:policy-helper", BusinessOwnerID: owner, TechnicalStewardID: steward}, manifest, instructions)
	profile.Version = 4
	profile.Purpose = "Answer reviewed policy questions for this tenant."
	profile.Audience = agentpersona.Audience{Roles: []string{"worker_self"}, Populations: []string{localDemoPopulationID}, OrganizationScopes: []string{"org:ironridge-demo:field"}}
	version, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	row := agentpersonastore.PersonaVersion{TenantID: tenant, PersonaID: profile.PersonaID, Version: 4, Profile: encoded, ContentDigest: version.Digest}
	db := &localDraftBootstrapDB{}
	directory := &localDraftBootstrapDirectory{member: &PersonaAudienceMember{SubjectID: owner, Roles: profile.Audience.Roles, Populations: profile.Audience.Populations, OrganizationScope: profile.Audience.OrganizationScopes[0]}}
	authorizer := &personaCreateAuthorizerFake{allowedTenant: tenant}
	manifests := &personaStarterProvisionStoreFake{manifests: map[string]agentmanifest.Manifest{manifest.ID: manifest}, contents: map[string]string{}}
	bootstrap := &LocalDevPolicyHelperDraftBootstrap{Core: db, TenantUUID: func(t values.TenantId) uuid.UUID { return pgstore.TenantID(t.String()) }, Directory: directory, Manifests: manifests, References: personaStarterProvisionReferencesFake{}, Skills: personaStarterProvisionSkillsFake{}, Drafts: &PersonaAdminDraftService{Authorizer: authorizer, Clock: personaDraftClockFake{now}, Profiles: &personaStarterProfileBuilderSpy{}}, Existing: localDraftBootstrapVersions{row: row, state: agentpersonastore.StateInReview}}
	ctx := trust.WithPrincipal(context.Background(), personaDraftPrincipal(t, tenant.String(), owner, now))
	receipt, err := bootstrap.CreateDraft(ctx, ServeProfileLocalDev)
	if err != nil || receipt.Version != 4 || receipt.Digest != version.Digest || receipt.Lifecycle != string(agentpersonastore.StateInReview) {
		t.Fatalf("current version was not preserved: %+v %v", receipt, err)
	}
	if db.begins != 0 || manifests.saves != 0 || len(manifests.contents) != 0 {
		t.Fatal("repeated setup wrote new grants, manifests or instructions")
	}
	invalid := row
	invalid.ContentDigest = personaInstructionDigest("different profile")
	bootstrap.Existing = localDraftBootstrapVersions{row: invalid, state: agentpersonastore.StateInReview}
	if _, err := bootstrap.CreateDraft(ctx, ServeProfileLocalDev); !errors.Is(err, ErrLocalDevPersonaChatBootstrap) {
		t.Fatalf("mismatched current profile accepted: %v", err)
	}
}

func TestTodo_AGENTP_023_LocalPolicyDraftBootstrapSecurity(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	db := &localDraftBootstrapDB{}
	directory := &localDraftBootstrapDirectory{}
	authorizer := &personaCreateAuthorizerFake{allowedTenant: "ironridge-demo", err: ErrPersonaDraftDenied}
	bootstrap := &LocalDevPolicyHelperDraftBootstrap{Core: db, TenantUUID: func(t values.TenantId) uuid.UUID { return pgstore.TenantID(t.String()) }, Directory: directory, Manifests: &personaStarterProvisionStoreFake{}, References: personaStarterProvisionReferencesFake{}, Skills: personaStarterProvisionSkillsFake{}, Drafts: &PersonaAdminDraftService{Authorizer: authorizer, Clock: personaDraftClockFake{now}}}
	for _, tc := range []struct{ profile, tenant, subject string }{{ServeProfileStandard, "ironridge-demo", "ir-001-walt-brennan"}, {ServeProfileLocalDev, "production-tenant", "ir-001-walt-brennan"}, {ServeProfileLocalDev, "ironridge-demo", "ir-003-loretta-haynes"}} {
		principal := personaDraftPrincipal(t, tc.tenant, tc.subject, now)
		if _, err := bootstrap.CreateDraft(trust.WithPrincipal(context.Background(), principal), tc.profile); !errors.Is(err, ErrLocalDevPersonaChatBootstrap) {
			t.Fatalf("unauthorized bootstrap %+v err=%v", tc, err)
		}
	}
	if authorizer.calls != 0 || db.begins != 0 || directory.reads != 0 {
		t.Fatalf("unauthorized setup reached dependencies: authorization=%d writes=%d directory=%d", authorizer.calls, db.begins, directory.reads)
	}
	principal := personaDraftPrincipal(t, "ironridge-demo", "ir-001-walt-brennan", now)
	if _, err := bootstrap.CreateDraft(trust.WithPrincipal(context.Background(), principal), ServeProfileLocalDev); !errors.Is(err, ErrLocalDevPersonaChatBootstrap) {
		t.Fatalf("denied current admin permission accepted err=%v", err)
	}
	if authorizer.calls != 1 || db.begins != 0 || directory.reads != 0 {
		t.Fatalf("side effect preceded admin permission: authorization=%d writes=%d directory=%d", authorizer.calls, db.begins, directory.reads)
	}
	authorizer.err = nil
	if _, err := bootstrap.CreateDraft(trust.WithPrincipal(context.Background(), principal), ServeProfileLocalDev); !errors.Is(err, ErrLocalDevPersonaChatBootstrap) {
		t.Fatalf("missing authoritative employee audience accepted err=%v", err)
	}
	if db.begins != 0 || directory.reads != 1 {
		t.Fatalf("missing audience reached writes=%d reads=%d", db.begins, directory.reads)
	}
}

func TestTodo_AGENTP_023_LocalPolicyDraftOwners(t *testing.T) {
	for _, tc := range []struct{ tenant, owner, steward string }{{"harborcare-demo", "hc-050-rafael-torres", "hc-054-thomas-baker"}, {"ironridge-demo", "ir-001-walt-brennan", "ir-003-loretta-haynes"}} {
		pack, _ := demoworkforce.PackFor(tc.tenant)
		owner, steward, err := localDevPolicyHelperOwners(pack, pgstore.TenantID(tc.tenant))
		if err != nil || owner != tc.owner || steward != tc.steward || owner == steward {
			t.Fatalf("tenant=%s owner=%s steward=%s err=%v", tc.tenant, owner, steward, err)
		}
		copy := *pack
		copy.FinancePartnerKey = owner
		if _, _, err := localDevPolicyHelperOwners(&copy, pgstore.TenantID(tc.tenant)); !errors.Is(err, ErrLocalDevPersonaChatBootstrap) {
			t.Fatalf("same owner/steward accepted tenant=%s err=%v", tc.tenant, err)
		}
	}
}
