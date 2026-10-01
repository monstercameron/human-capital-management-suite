package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// LocalDevPolicyHelperDraftBootstrap composes the explicitly selected demo
// setup with the same current directory and authorized draft service as the
// administrator UI. It accepts a verified human context, never an actor ID.
type LocalDevPolicyHelperDraftBootstrap struct {
	Core       dbport.Beginner
	TenantUUID func(values.TenantId) uuid.UUID
	Directory  PersonaAudienceDirectory
	Manifests  LocalDevPersonaStarterManifestStore
	References LocalDevPersonaStarterReferenceResolver
	Skills     agentpersona.SkillResolver
	Drafts     *PersonaAdminDraftService
	Existing   LocalDevPolicyHelperVersionLookup
}

// LocalDevPolicyHelperVersionLookup preserves an already persisted exact
// draft and its actual lifecycle on repeated operator bootstrap calls.
type LocalDevPolicyHelperVersionLookup interface {
	GetVersion(context.Context, values.TenantId, string, int64) (agentpersonastore.PersonaVersion, error)
	ListVersions(context.Context, values.TenantId, string) ([]agentpersonastore.PersonaVersion, error)
	Lifecycle(context.Context, values.TenantId, string, int64) (agentpersonastore.LifecycleState, error)
}

// CreateDraft creates a tenant-owned local copy of Policy Helper with the
// administrator's exact current employee audience. It records explicit local
// skill grants and immutable instructions, then creates only a DRAFT. Review,
// measured evaluation, publication and installation remain independent steps.
func (b *LocalDevPolicyHelperDraftBootstrap) CreateDraft(ctx context.Context, profile string) (PersonaDraft, error) {
	if b == nil || ctx == nil || profile != ServeProfileLocalDev || b.Core == nil || b.TenantUUID == nil || b.Directory == nil || b.Manifests == nil || b.References == nil || b.Skills == nil || b.Drafts == nil || b.Drafts.Authorizer == nil || b.Drafts.Clock == nil {
		return PersonaDraft{}, ErrLocalDevPersonaChatBootstrap
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman {
		return PersonaDraft{}, ErrLocalDevPersonaChatBootstrap
	}
	now := b.Drafts.Clock.Now().UTC()
	if now.IsZero() || now.Before(principal.IssuedAt()) || !now.Before(principal.ExpiresAt()) {
		return PersonaDraft{}, ErrLocalDevPersonaChatBootstrap
	}
	tenant := principal.Tenant()
	pack, ok := demoworkforce.PackFor(tenant.String())
	if !ok || b.TenantUUID(tenant) == uuid.Nil {
		return PersonaDraft{}, ErrLocalDevPersonaChatBootstrap
	}
	owner, steward, err := localDevPolicyHelperOwners(pack, b.TenantUUID(tenant))
	if err != nil || principal.Subject() != owner {
		return PersonaDraft{}, ErrLocalDevPersonaChatBootstrap
	}
	personaID := "hcmnext.local.persona.policy_helper"
	if err := b.Drafts.Authorizer.AuthorizePersonaCreate(ctx, PersonaCreateAuthorization{Principal: principal, Tenant: tenant, PersonaID: personaID}); err != nil {
		return PersonaDraft{}, fmt.Errorf("%w: current persona administration permission required", ErrLocalDevPersonaChatBootstrap)
	}
	member, err := b.Directory.ResolvePersonaAudienceMember(ctx, tenant.String(), principal.Subject())
	if err != nil || member.SubjectID != principal.Subject() || len(member.Roles) == 0 || len(member.Populations) != 1 || member.Populations[0] != localDemoPopulationID || strings.TrimSpace(member.OrganizationScope) == "" {
		return PersonaDraft{}, fmt.Errorf("%w: exact current employee audience required", ErrLocalDevPersonaChatBootstrap)
	}
	starter, _ := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	for _, pin := range starter.SkillPins {
		record, err := b.Skills.ResolvePin(pin)
		if err != nil || record.Status != agentskills.StatusActive || record.Digest != pin.Digest || record.Definition.SideEffectTier != agentskills.TierT0 {
			return PersonaDraft{}, ErrPersonaStarterSkillUnavailable
		}
	}
	refs, err := resolveLocalDevPersonaStarterReferences(ctx, b.References, starter)
	if err != nil {
		return PersonaDraft{}, err
	}
	instructions := personaStarterInstructions(starter)
	manifest := localDevPersonaStarterManifest(starter, instructions, refs)
	// This explicit local development allowance is one cent per bounded reply.
	// The platform's manifest-only starter fixture allowance is too small for
	// a real model call; it is left unchanged for that fixture workflow.
	manifest.Budget.MaxCostMicros = 10000
	current, _, err := b.Manifests.CurrentManifest(ctx, b.TenantUUID(tenant), manifest.ID)
	missing := errors.Is(err, agentstore.ErrNotFound)
	if err != nil && !missing {
		return PersonaDraft{}, err
	}
	if !missing {
		manifest.Version = current.Version
		if !samePersonaStarterManifest(current, manifest) {
			return PersonaDraft{}, fmt.Errorf("%w: tenant manifest differs", ErrLocalDevPersonaChatBootstrap)
		}
		if draft, found, err := b.currentDraft(ctx, tenant, personaID, owner, steward, manifest); found || err != nil {
			return draft, err
		}
	}
	if err := b.provisionGrants(ctx, tenant, owner, member, starter.SkillPins, now); err != nil {
		return PersonaDraft{}, err
	}
	digest, err := b.Manifests.SaveInstructionContent(ctx, b.TenantUUID(tenant), instructions)
	if err != nil || digest != manifest.InstructionsDigest {
		return PersonaDraft{}, fmt.Errorf("%w: immutable instruction content unavailable", ErrLocalDevPersonaChatBootstrap)
	}
	if missing {
		if _, err := b.Manifests.SaveManifest(ctx, b.TenantUUID(tenant), manifest, 0); err != nil {
			return PersonaDraft{}, err
		}
	}
	draftProfile := personaStarterProfile(starter, PersonaStarterDraftRequest{PersonaID: personaID, AvatarRef: "avatar:policy-helper", OrganizationScopes: []string{member.OrganizationScope}, BusinessOwnerID: owner, TechnicalStewardID: steward}, manifest, instructions)
	draftProfile.Audience = agentpersona.Audience{Roles: slices.Clone(member.Roles), Populations: slices.Clone(member.Populations), OrganizationScopes: []string{member.OrganizationScope}}
	version, err := buildPersonaProfile(ctx, b.Drafts.Profiles, tenant, draftProfile)
	if err != nil {
		return PersonaDraft{}, err
	}
	if b.Existing != nil {
		stored, readErr := b.Existing.GetVersion(ctx, tenant, personaID, int64(version.Profile.Version))
		if readErr == nil {
			if stored.ContentDigest != version.Digest {
				return PersonaDraft{}, fmt.Errorf("%w: existing local persona profile differs", ErrLocalDevPersonaChatBootstrap)
			}
			state, stateErr := b.Existing.Lifecycle(ctx, tenant, personaID, stored.Version)
			if stateErr != nil {
				return PersonaDraft{}, stateErr
			}
			return PersonaDraft{PersonaID: personaID, Version: version.Profile.Version, Digest: version.Digest, Lifecycle: string(state)}, nil
		}
		if !errors.Is(readErr, agentpersonastore.ErrNotFound) {
			return PersonaDraft{}, readErr
		}
	}
	return b.Drafts.CreateDraft(ctx, PersonaDraftRequest{Version: version, BusinessOwnerID: owner, TechnicalStewardID: steward})
}

func (b *LocalDevPolicyHelperDraftBootstrap) currentDraft(ctx context.Context, tenant values.TenantId, personaID, owner, steward string, manifest agentmanifest.Manifest) (PersonaDraft, bool, error) {
	if b.Existing == nil {
		return PersonaDraft{}, false, nil
	}
	versions, err := b.Existing.ListVersions(ctx, tenant, personaID)
	if err != nil {
		return PersonaDraft{}, false, err
	}
	if len(versions) == 0 {
		return PersonaDraft{}, false, nil
	}
	latest := versions[0]
	for _, version := range versions {
		if version.TenantID != tenant || version.PersonaID != personaID || version.Version <= 0 || version.Version > math.MaxUint32 {
			return PersonaDraft{}, false, ErrLocalDevPersonaChatBootstrap
		}
		if version.Version > latest.Version {
			latest = version
		}
	}
	var profile agentpersona.PersonaProfile
	if json.Unmarshal(latest.Profile, &profile) != nil || profile.PersonaID != personaID || int64(profile.Version) != latest.Version || profile.Owner != owner || profile.Steward != steward ||
		profile.Manifest.ID != manifest.ID || uint64(profile.Manifest.Version) != manifest.Version || profile.Manifest.SchemaVersion != manifest.SchemaVersion || profile.Manifest.Digest != manifestDigest(manifest) {
		return PersonaDraft{}, false, ErrLocalDevPersonaChatBootstrap
	}
	validated, err := buildPersonaProfile(ctx, b.Drafts.Profiles, tenant, profile)
	if err != nil || validated.Digest != latest.ContentDigest {
		return PersonaDraft{}, false, fmt.Errorf("%w: current local persona profile differs", ErrLocalDevPersonaChatBootstrap)
	}
	state, err := b.Existing.Lifecycle(ctx, tenant, personaID, latest.Version)
	if err != nil {
		return PersonaDraft{}, false, err
	}
	return PersonaDraft{PersonaID: personaID, Version: profile.Version, Digest: latest.ContentDigest, Lifecycle: string(state)}, true, nil
}

func localDevPolicyHelperOwners(pack *demoworkforce.Pack, tenant uuid.UUID) (string, string, error) {
	workers, err := pack.Plan(tenant)
	if err != nil {
		return "", "", err
	}
	number := ""
	for _, persona := range pack.Personas {
		if persona.ID == "admin" {
			number = persona.WorkerNumber
		}
	}
	owner := ""
	stewardPresent := false
	for _, worker := range workers {
		if worker.Row.WorkerType != "employee" || worker.Row.LifecycleStatus != "active" {
			continue
		}
		if worker.Row.WorkerNumber == number {
			owner = worker.Row.WorkerKey
		}
		if worker.Row.WorkerKey == pack.FinancePartnerKey {
			stewardPresent = true
		}
	}
	if owner == "" || !stewardPresent || owner == pack.FinancePartnerKey {
		return "", "", ErrLocalDevPersonaChatBootstrap
	}
	return owner, pack.FinancePartnerKey, nil
}

func (b *LocalDevPolicyHelperDraftBootstrap) provisionGrants(ctx context.Context, tenant values.TenantId, owner string, member PersonaAudienceMember, pins []agentskills.SkillPin, at time.Time) error {
	tx, err := b.Core.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tenantID := b.TenantUUID(tenant)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	roles := slices.Clone(member.Roles)
	slices.Sort(roles)
	scopes := []string{member.OrganizationScope}
	for _, pin := range pins {
		record, err := b.Skills.ResolvePin(pin)
		if err != nil {
			return err
		}
		purposes := slices.Clone(record.Definition.RequiredPurposes)
		purposes = append(purposes, "persona_admin_preview")
		slices.Sort(purposes)
		purposes = slices.Compact(purposes)
		grantID := "local-demo/policy-helper/" + pin.ID + "/v1"
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenantID.String()+"/"+grantID); err != nil {
			return err
		}
		var currentSkill, currentPopulation, currentOwner, currentEvidence string
		var currentVersion int64
		var currentRoles, currentScopes, currentPurposes []string
		var current bool
		err = tx.QueryRow(ctx, `SELECT skill_id,skill_version,roles,population,organization_scopes,purposes,granted_by,admin_evidence_ref,revoked_at IS NULL AND not_before<=CURRENT_TIMESTAMP AND (expires_at IS NULL OR expires_at>CURRENT_TIMESTAMP) FROM agent_skill_grant WHERE tenant_id=$1 AND grant_id=$2`, tenantID, grantID).Scan(&currentSkill, &currentVersion, &currentRoles, &currentPopulation, &currentScopes, &currentPurposes, &currentOwner, &currentEvidence, &current)
		evidence := "local-demo/persona-policy-helper/v1/" + pin.Digest
		if err == nil {
			if !current || currentSkill != pin.ID || currentVersion != int64(pin.Version) || !slices.Equal(currentRoles, roles) || currentPopulation != localDemoPopulationID || !slices.Equal(currentScopes, scopes) || !slices.Equal(currentPurposes, purposes) || currentOwner != owner || currentEvidence != evidence {
				return fmt.Errorf("%w: existing local grant differs or is revoked", ErrLocalDevPersonaChatBootstrap)
			}
			continue
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agent_skill_grant(tenant_id,grant_id,skill_id,skill_version,roles,population,organization_scopes,purposes,not_before,granted_by,granted_at,admin_evidence_ref) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$9,$11)`, tenantID, grantID, pin.ID, int64(pin.Version), roles, localDemoPopulationID, scopes, purposes, at, owner, evidence); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
