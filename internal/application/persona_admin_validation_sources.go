package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentskillgrantstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaAdminValidationSources = errors.New("application: persona admin validation sources unavailable")

// PersonaAdminValidationSources groups the three authorities required by the
// command surface's draft and starter workflows.
type PersonaAdminValidationSources struct {
	Profiles     PersonaProfileBuilderSource
	Manifests    PersonaStarterManifestResolverSource
	Instructions PersonaStarterInstructionsResolverSource
}

// NewDatabasePersonaAdminValidationSources composes the persona sources over
// the isolated agent manifest/content stores, current tenant skill grants,
// global immutable skill registry, and canonical tenant mapper.
func NewDatabasePersonaAdminValidationSources(manifests AgentManifestVersionStore, instructions PersonaManifestInstructionStore, skills agentpersona.SkillResolver, grants *agentskillgrantstore.Store, tenantUUID func(values.TenantId) uuid.UUID) (PersonaAdminValidationSources, error) {
	if manifests == nil || instructions == nil || skills == nil || grants == nil || tenantUUID == nil {
		return PersonaAdminValidationSources{}, errPersonaAdminValidationSources
	}
	manifestSource, err := NewTenantAgentPersonaManifestSource(manifests, tenantUUID)
	if err != nil {
		return PersonaAdminValidationSources{}, errPersonaAdminValidationSources
	}
	return PersonaAdminValidationSources{
		Profiles: DatabasePersonaProfileBuilderSource{
			Skills: skills, Grants: AgentSkillGrantPersonaProfileSource{Store: grants}, Manifests: manifestSource,
		},
		Manifests: manifestSource,
		Instructions: DatabasePersonaStarterInstructionsSource{
			Store: instructions, TenantUUID: tenantUUID,
		},
	}, nil
}

// PersonaProfileGrantReader reads current skill grants for one fixed tenant.
type PersonaProfileGrantReader interface {
	Grants(context.Context, values.TenantId, agentskills.SkillKey) ([]agentgate.SkillGrant, error)
}

// PersonaProfileGrantSource creates one tenant-bound reader for current grants.
type PersonaProfileGrantSource interface {
	ForTenant(context.Context, values.TenantId) (PersonaProfileGrantReader, error)
}

// AgentSkillGrantPersonaProfileSource adapts the durable agent grant store to
// the tenant-scoped profile validation source.
type AgentSkillGrantPersonaProfileSource struct {
	Store *agentskillgrantstore.Store
}

// ForTenant binds the grant reader to the verified human principal's tenant.
func (s AgentSkillGrantPersonaProfileSource) ForTenant(ctx context.Context, tenant values.TenantId) (PersonaProfileGrantReader, error) {
	if s.Store == nil || ctx == nil || tenant.Validate() != nil {
		return nil, errPersonaAdminValidationSources
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman || p.Tenant() != tenant {
		return nil, errPersonaAdminValidationSources
	}
	store, err := s.Store.Scoped(tenant)
	if err != nil || store == nil {
		return nil, errPersonaAdminValidationSources
	}
	return personaProfileGrantReader{tenant: tenant, store: store}, nil
}

type personaProfileGrantReader struct {
	tenant values.TenantId
	store  personaProfileGrantStore
}

type personaProfileGrantStore interface {
	Grants(context.Context, values.TenantId, agentskills.SkillKey) ([]agentgate.SkillGrant, error)
}

func (r personaProfileGrantReader) Grants(ctx context.Context, tenant values.TenantId, key agentskills.SkillKey) ([]agentgate.SkillGrant, error) {
	p, ok := trust.FromContext(ctx)
	if r.store == nil || !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman || p.Tenant() != r.tenant || tenant != r.tenant {
		return nil, errPersonaAdminValidationSources
	}
	return r.store.Grants(ctx, r.tenant, key)
}

// DatabasePersonaProfileBuilderSource builds validators from the existing
// global skill catalog, durable tenant grants, and tenant manifest store.
type DatabasePersonaProfileBuilderSource struct {
	Skills    agentpersona.SkillResolver
	Grants    PersonaProfileGrantSource
	Manifests PersonaStarterManifestResolverSource
}

// ForTenant returns a builder whose grants and manifest compatibility checks
// are bound to the verified tenant. The request's profile cannot select either
// tenant or backing store.
func (s DatabasePersonaProfileBuilderSource) ForTenant(ctx context.Context, tenant values.TenantId) (PersonaProfileBuilder, error) {
	if ctx == nil || tenant.Validate() != nil {
		return nil, errPersonaAdminValidationSources
	}
	p, ok := trust.FromContext(ctx)
	if s.Skills == nil || s.Grants == nil || s.Manifests == nil || !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman || p.Tenant() != tenant {
		return nil, errPersonaAdminValidationSources
	}
	grants, err := s.Grants.ForTenant(ctx, tenant)
	if err != nil || grants == nil {
		return nil, errPersonaAdminValidationSources
	}
	manifest, err := s.Manifests.ForTenant(ctx, tenant)
	if err != nil || manifest == nil {
		return nil, errPersonaAdminValidationSources
	}
	return tenantPersonaProfileBuilder{ctx: ctx, tenant: tenant, skills: s.Skills, grants: grants, manifests: manifest}, nil
}

type tenantPersonaProfileBuilder struct {
	ctx       context.Context
	tenant    values.TenantId
	skills    agentpersona.SkillResolver
	grants    PersonaProfileGrantReader
	manifests PersonaStarterManifestResolver
}

func (b tenantPersonaProfileBuilder) Build(profile agentpersona.PersonaProfile) (agentpersona.PersonaVersion, error) {
	return b.BuildForTenant(b.ctx, b.tenant, profile)
}

func (b tenantPersonaProfileBuilder) BuildForTenant(ctx context.Context, tenant values.TenantId, profile agentpersona.PersonaProfile) (agentpersona.PersonaVersion, error) {
	if ctx == nil || tenant.Validate() != nil {
		return agentpersona.PersonaVersion{}, errPersonaAdminValidationSources
	}
	p, ok := trust.FromContext(ctx)
	if b.skills == nil || b.grants == nil || b.manifests == nil || !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman || tenant != b.tenant || p.Tenant() != b.tenant {
		return agentpersona.PersonaVersion{}, errPersonaAdminValidationSources
	}
	if profile.Template != nil {
		starter, found := agenttemplate.PersonaStarterFor(profile.Template.ID, profile.Template.Version)
		if !found || profile.EvalSuiteRef != starter.EvaluationSuite || !personaVersionWithinStarter(profile, starter) {
			return agentpersona.PersonaVersion{}, fmt.Errorf("%w: trusted starter provenance or limits changed", ErrPersonaDraftInvalid)
		}
	}
	validator := agentpersona.Validator{
		Skills:    b.skills,
		Manifests: agentpersona.ManifestCompatibilityAdapter{Resolver: personaManifestCompatibilityResolver{ctx: ctx, resolver: b.manifests}},
	}
	grantProvider := tenantPersonaProfileGrantProvider{ctx: ctx, tenant: b.tenant, grants: b.grants}
	grantResolver, err := NewTenantPersonaSkillGrantResolver(grantProvider, b.tenant)
	if err != nil {
		return agentpersona.PersonaVersion{}, errPersonaAdminValidationSources
	}
	validator.Grants = grantResolver
	version, err := validator.Build(profile)
	if err != nil {
		return agentpersona.PersonaVersion{}, fmt.Errorf("validate tenant persona profile: %w", err)
	}
	return version, nil
}

type personaManifestCompatibilityResolver struct {
	ctx      context.Context
	resolver PersonaStarterManifestResolver
}

func (r personaManifestCompatibilityResolver) ResolveAgentManifest(ref agentpersona.AgentManifestRef) (manifest agentmanifest.Manifest, err error) {
	if r.resolver == nil {
		return agentmanifest.Manifest{}, errPersonaAdminValidationSources
	}
	manifest, err = r.resolver.ResolveCurrentPersonaManifest(r.ctx, ref.ID)
	if err != nil || manifest.Version != uint64(ref.Version) || manifest.SchemaVersion != ref.SchemaVersion {
		return agentmanifest.Manifest{}, ErrAgentManifestNotPublished
	}
	return manifest, nil
}

type tenantPersonaProfileGrantProvider struct {
	ctx    context.Context
	tenant values.TenantId
	grants PersonaProfileGrantReader
}

func (p tenantPersonaProfileGrantProvider) Grants(_ context.Context, tenant values.TenantId, key agentskills.SkillKey) ([]agentgate.SkillGrant, error) {
	verified, ok := trust.FromContext(p.ctx)
	if p.grants == nil || p.ctx == nil || !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman || verified.Tenant() != p.tenant || tenant != p.tenant {
		return nil, errPersonaAdminValidationSources
	}
	rows, err := p.grants.Grants(p.ctx, p.tenant, key)
	if err != nil {
		return nil, errPersonaAdminValidationSources
	}
	for _, row := range rows {
		if row.Tenant != p.tenant || row.Skill != key {
			return nil, errPersonaAdminValidationSources
		}
	}
	return rows, nil
}

// PersonaManifestInstructionStore is the durable contract for instruction
// content pinned by an immutable tenant manifest. agentstore.Store implements
// this contract.
type PersonaManifestInstructionStore interface {
	ManifestInstructions(context.Context, uuid.UUID, string, uint64, string) (string, error)
}

// DatabasePersonaStarterInstructionsSource resolves instruction bytes from a
// durable, tenant-keyed content store. It never treats request text as content.
type DatabasePersonaStarterInstructionsSource struct {
	Store      PersonaManifestInstructionStore
	TenantUUID func(values.TenantId) uuid.UUID
}

// ForTenant creates a resolver fixed to the authenticated tenant and mapped
// storage key.
func (s DatabasePersonaStarterInstructionsSource) ForTenant(ctx context.Context, tenant values.TenantId) (PersonaStarterInstructionsResolver, error) {
	if ctx == nil || tenant.Validate() != nil {
		return nil, errPersonaAdminValidationSources
	}
	p, ok := trust.FromContext(ctx)
	if s.Store == nil || s.TenantUUID == nil || !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman || p.Tenant() != tenant {
		return nil, errPersonaAdminValidationSources
	}
	id := s.TenantUUID(tenant)
	if id == uuid.Nil {
		return nil, errPersonaAdminValidationSources
	}
	return tenantPersonaInstructionResolver{store: s.Store, tenant: tenant, tenantID: id}, nil
}

type tenantPersonaInstructionResolver struct {
	store    PersonaManifestInstructionStore
	tenant   values.TenantId
	tenantID uuid.UUID
}

func (r tenantPersonaInstructionResolver) ResolvePersonaInstructions(ctx context.Context, id string, version uint64, digest string) (string, error) {
	p, ok := trust.FromContext(ctx)
	if r.store == nil || r.tenantID == uuid.Nil || id == "" || version == 0 || digest == "" || !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman || p.Tenant() != r.tenant {
		return "", errPersonaAdminValidationSources
	}
	text, err := r.store.ManifestInstructions(ctx, r.tenantID, id, version, digest)
	if err != nil || !personaInstructionsMatchDigest(text, digest) {
		return "", errPersonaAdminValidationSources
	}
	return text, nil
}

var _ PersonaProfileBuilderSource = DatabasePersonaProfileBuilderSource{}
var _ PersonaStarterInstructionsResolverSource = DatabasePersonaStarterInstructionsSource{}
var _ AgentManifestVersionStore = (*agentstore.Store)(nil)
var _ PersonaManifestInstructionStore = (*agentstore.Store)(nil)
