package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentskillgrantstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var errPersonaServeWiring = errors.New("application: persona serve wiring unavailable")

type personaServeWiringError struct {
	stage string
	err   error
}

func (e personaServeWiringError) Error() string {
	return "application: persona serve wiring " + e.stage + " failed"
}
func (e personaServeWiringError) Unwrap() error { return e.err }

func personaServeWiringStage(err error) string {
	var wiringErr personaServeWiringError
	if errors.As(err, &wiringErr) {
		return wiringErr.stage
	}
	return "chat_binding"
}

func personaServeClock(now func() time.Time) func() time.Time {
	if now != nil {
		return now
	}
	return time.Now
}

// personaInvocationServeMissingPorts lists production-only dependencies that
// currently have no served composition. The chat surface remains usable, but a
// persona mention must not be presented as running until these ports exist.
func personaInvocationServeMissingPorts() []string {
	return []string{
		"tenant_scoped_durable_invocation_admission_and_execution_stores",
		"current_admission_rechecker",
		"trusted_persona_model_work_source",
		"sealed_persona_output_validator_and_persister",
		"persona_run_model_worker_composition",
	}
}

// personaServeWiring holds the two user-facing persona projections. Chat is
// intentionally bound lazily because the audience source must read current
// membership from the very chat service whose composition consumes it.
type personaServeWiring struct {
	refs              *lazyPersonaReferenceSource
	store             *agentpersonastore.Store
	avail             AvailablePersonaReader
	skill             AgentSkillDiscoverer
	adminSkills       agentpersona.SkillResolver
	adminGrantStore   *agentskillgrantstore.Store
	capabilities      *capability.Registry
	capabilityGateway *capability.Gateway
	skills            *agentskills.Registry
	adminCatalog      productui.PersonaAdminClient
	aud               *CurrentPersonaAudience
	now               func() time.Time
	roles             roleaccess.Store
	adminTargets      *ChatDirectoryPersonaCatalogTargets
	adminGrants       *CurrentPersonaCatalogGrants
	adminEvidence     PersonaAdminPublicationEvidenceResolver
	adminInstall      PersonaAdminInstallationAuthorizer
	adminTransitions  PersonaAdminLifecycleTransitions
	adminFactory      interface {
		ClientForRequest(context.Context) productui.PersonaAdminClient
	}
}

// composePersonaServeWiring builds production persona sources. Incomplete
// dependencies produce a nil wiring, which leaves both surfaces unavailable
// rather than deriving authority from request claims or labels.
func composePersonaServeWiring(pool *pgxadapter.Pool, cell *app.Cell, personas *agentpersonastore.Store, now func() time.Time) (*personaServeWiring, error) {
	if pool == nil {
		return nil, personaServeWiringError{stage: "core_pool_missing", err: errPersonaServeWiring}
	}
	if cell == nil || cell.Workers == nil || cell.Evidence == nil {
		return nil, personaServeWiringError{stage: "core_authorities_missing", err: errPersonaServeWiring}
	}
	if personas == nil {
		return nil, personaServeWiringError{stage: "persona_store_missing", err: errPersonaServeWiring}
	}
	// ServeOptions.Now is optional outside the local-dev clock profile. The
	// persona directory still needs an explicit wall-clock coordinate to
	// resolve current worker name facts, so bind the ordinary server clock here.
	now = personaServeClock(now)
	tenantUUID := tenantKeyMapper[values.TenantId](pgstore.TenantID)
	agentDirectory := NewAgentDirectoryDB(pool, tenantUUID)
	audienceSource := &DatabasePersonaAudienceSource{
		Installations: personaInstallationStore{store: personas},
		Directory:     NewPersonaAudienceDirectoryDB(agentDirectory),
	}
	audience := &CurrentPersonaAudience{Source: audienceSource}
	grants, err := agentskillgrantstore.New(pool, tenantUUID)
	if err != nil {
		return nil, personaServeWiringError{stage: "skill_grant_store", err: err}
	}
	reader := ownWorkerReader{db: pool, tenantUUID: tenantUUID, workers: cell.Workers, now: now}
	catalog, capabilityGateway, err := newAgentCapabilities(reader, cell.Evidence, now)
	if err != nil {
		return nil, personaServeWiringError{stage: "capability_catalog", err: err}
	}
	skills, err := newAgentSkills(catalog)
	if err != nil {
		return nil, personaServeWiringError{stage: "skill_catalog", err: err}
	}
	if _, err := bindPersonaChatReplySkill(catalog, skills); err != nil {
		return nil, personaServeWiringError{stage: "chat_reply_skill", err: err}
	}
	current := &ServerAgentDiscoveryContext{
		Roles:         &TenantAgentRoleSource{Directory: agentDirectory},
		Population:    &TenantAgentPopulationSource{Directory: agentDirectory},
		Organizations: &TenantAgentOrganizationSource{Directory: agentDirectory},
		Subjects:      &TenantAgentSubjectSource{Directory: personaHomeOrganizationSubjects{directory: agentDirectory}},
		Fields:        &TenantAgentFieldSource{Policy: agentDirectory},
	}
	skillSource, err := NewAgentSkillSource(skills, tenantGrantProvider{store: grants}, current)
	if err != nil {
		return nil, personaServeWiringError{stage: "skill_source", err: err}
	}
	// The served Cell may have been composed without ExecutionDB, leaving its
	// worker facts reader corpus-only. Chat memberships use durable WorkerKeys,
	// so resolve those through the same tenant-scoped workforce store before
	// building the catalog member directory.
	memberWorkers := workforce.NewLayeredWorkerFacts(cell.Workers, pool, tenantUUID)
	memberDirectory, err := NewCorePersonaCatalogMemberReaderWithIdentity(memberWorkers, personaCatalogWorkforceIdentity{facts: workforce.NewFacts(pool, tenantUUID)}, now)
	if err != nil {
		return nil, personaServeWiringError{stage: "member_directory", err: err}
	}
	catalogDirectory, err := NewCorePersonaCatalogDirectory(memberDirectory)
	if err != nil {
		return nil, personaServeWiringError{stage: "catalog_directory", err: err}
	}
	gate, err := agentgate.New(agentgate.Config{Skills: skills, Grants: tenantGrantProvider{store: grants}, Now: now})
	if err != nil {
		return nil, personaServeWiringError{stage: "effective_grant_gate", err: err}
	}
	adminTargets := &ChatDirectoryPersonaCatalogTargets{Directory: catalogDirectory}
	adminGrants := &CurrentPersonaCatalogGrants{Evaluator: gate, Context: current, Skills: skills, Purpose: "persona_admin_preview", Now: now}
	available := &TenantAvailablePersonaReader{
		Backend:  &AgentPersonaStoreBackend{Store: personas},
		Audience: audience,
		Skills:   skillSource,
	}
	return &personaServeWiring{
		refs:              &lazyPersonaReferenceSource{},
		store:             personas,
		avail:             available,
		skill:             skillSource,
		adminSkills:       skillSource,
		adminGrantStore:   grants,
		capabilities:      catalog,
		capabilityGateway: capabilityGateway,
		skills:            skills,
		aud:               audience,
		now:               now,
		roles:             cell.RoleAccess,
		adminTargets:      adminTargets,
		adminGrants:       adminGrants,
	}, nil
}

func (w *personaServeWiring) chatComposition() ChatComposition {
	if w == nil || w.refs == nil {
		return ChatComposition{}
	}
	// composeChat receives the lazy source before its chat dependency exists.
	return ChatComposition{PersonaReferences: w.refs, PersonaDMFactory: func(store chat.Store, creator chat.ConversationService) (chat.PersonaDMResolver, error) {
		if w.store == nil || w.now == nil {
			return nil, errPersonaDMResolverUnavailable
		}
		return &publishedPersonaDM{publications: AgentPersonaPublishedSource{Store: w.store}, identities: w.store, store: store, creator: creator, now: w.now}, nil
	}, AuthorityTTL: 0, PollInterval: 0, StreamRecheckInterval: 0}
}

func (w *personaServeWiring) bindChat(service chat.ConversationService) error {
	if w == nil || w.refs == nil || w.store == nil || w.avail == nil || w.aud == nil || service == nil {
		return errPersonaServeWiring
	}
	identities, err := newProductionPersonaChatIdentityDirectory(w.store)
	if err != nil {
		return fmt.Errorf("compose persona identity directory: %w", err)
	}
	lookup, err := newProductionPersonaReferenceLookup(identities, w.store)
	if err != nil {
		return fmt.Errorf("compose persona reference lookup: %w", err)
	}
	refs, err := newPersonaChatReferenceSource(w.avail, identities, lookup, w.now)
	if err != nil {
		return fmt.Errorf("compose persona reference source: %w", err)
	}
	w.refs.bind(refs)
	w.aud.Source.(*DatabasePersonaAudienceSource).Chat = service
	w.adminTargets.Chat = service
	w.adminGrants.Chat = service
	client, err := NewPersonaAdminCatalogClient(PersonaAdminCatalogComposition{Store: personaAdminCatalogStoreAdapter{store: w.store}, Skills: w.adminSkills, Targets: w.adminTargets, Grants: w.adminGrants, Authorizer: TrustedPersonaCatalogAuthorizer{Roles: w.roles}})
	if err != nil {
		return personaServeWiringError{stage: "catalog_client", err: err}
	}
	w.adminFactory = newPersonaAdminServeFactory(client)
	w.adminCatalog = client
	if w.adminFactory == nil {
		return personaServeWiringError{stage: "catalog_route", err: errPersonaServeWiring}
	}
	return nil
}

// bindAdminCommands attaches durable draft commands once the isolated agent
// manifest and instruction sources are available. An absent review pool leaves
// review and publication closed while draft creation remains usable.
func (w *personaServeWiring) bindAdminCommands(agentDB *agentstore.Store, reviewDB *agentstore.PersonaReviewAuthorityStore, placements ...PersonaAdminPlacementSource) error {
	if w == nil || w.adminCatalog == nil || w.adminSkills == nil || w.adminGrantStore == nil || agentDB == nil {
		return errPersonaServeWiring
	}
	tenantUUID := tenantKeyMapper[values.TenantId](pgstore.TenantID)
	sources, err := NewDatabasePersonaAdminValidationSources(agentDB, agentDB, w.adminSkills, w.adminGrantStore, tenantUUID)
	if err != nil {
		return personaServeWiringError{stage: "admin_validation_sources", err: err}
	}
	starterSource, err := NewPersonaAdminStarterSource(w.adminSkills, sources.Manifests, sources.Instructions, AgentSkillGrantPersonaProfileSource{Store: w.adminGrantStore}, TrustedPersonaCatalogAuthorizer{Roles: w.roles})
	if err != nil {
		return personaServeWiringError{stage: "admin_starter_source", err: err}
	}
	client, err := NewPersonaAdminCatalogClient(PersonaAdminCatalogComposition{
		Store: personaAdminCatalogStoreAdapter{store: w.store}, Skills: w.adminSkills,
		Targets: w.adminTargets, Grants: w.adminGrants,
		Authorizer: TrustedPersonaCatalogAuthorizer{Roles: w.roles}, Starters: starterSource,
	})
	if err != nil {
		return personaServeWiringError{stage: "admin_catalog_starters", err: err}
	}
	var reviewSources PersonaAdminReviewSources
	if reviewDB != nil {
		reviewSources, err = NewPersonaAdminReviewSources(agentDB, reviewDB, tenantUUID)
		if err != nil {
			return personaServeWiringError{stage: "admin_review_sources", err: err}
		}
	}
	profiles := TenantPersonaProfileBuilder{Source: sources.Profiles}
	authorizer := PersonaAdminCommandRoleAuthorizer{Roles: w.roles}
	w.adminEvidence = personaAdminStoredEvidence{store: w.store}
	w.adminTransitions = &PersonaAdminReviewedTransitions{
		Store: personaAdminLifecycleStoreAdapter{store: w.store}, Authorizer: authorizer,
		Profiles: profiles, Evidence: w.adminEvidence, Security: agentDB,
		TenantUUID: tenantUUID, Now: w.now, NewEventID: func() string { return uuid.NewString() },
	}
	if len(placements) == 1 && placements[0] != nil {
		w.adminInstall = GovernedPersonaAdminInstallation{Placement: placements[0], Versions: personaAdminInstallationVersions{store: w.store}, Profiles: profiles}
	}
	factory, err := w.newAdminCommandFactory(client, sources.Profiles, sources.Manifests, sources.Instructions, reviewSources.Authority, reviewSources.Writer, func() string { return uuid.NewString() })
	if err != nil {
		return personaServeWiringError{stage: "admin_commands", err: err}
	}
	w.adminFactory = factory
	w.adminCatalog = client
	return nil
}

func (w *personaServeWiring) adminClientFactory() interface {
	ClientForRequest(context.Context) productui.PersonaAdminClient
} {
	if w == nil {
		return nil
	}
	return w.adminFactory
}

func (w *personaServeWiring) bindAgents(tasks productui.AgentClient) productui.AgentClient {
	if w == nil || tasks == nil || w.avail == nil || w.skill == nil {
		return tasks
	}
	bound, err := NewAgentPageCatalogBinding(tasks, w.avail, w.skill)
	if err != nil {
		return tasks
	}
	return bound
}

// bindNativeStarterSkills binds native owner adapters to the same registries
// consumed by discovery, profile validation, admission and execution. The
// dependencies are server composition, never caller-selected implementations.
func (w *personaServeWiring) bindNativeStarterSkills(onboarding PersonaOnboardingPort, schedule PersonaSchedulePort, band PersonaCompensationPort, compa PersonaCompaRatioPort, scenario PersonaCompensationScenarioPort) error {
	if w == nil || w.capabilities == nil || w.skills == nil || onboarding == nil || schedule == nil || band == nil || compa == nil || scenario == nil {
		return personaServeWiringError{stage: "native_starter_sources", err: errPersonaServeWiring}
	}
	if _, err := BindPersonaOnboardingSkills(w.capabilities, w.skills, onboarding); err != nil {
		return personaServeWiringError{stage: "onboarding_skills", err: err}
	}
	if _, err := BindPersonaScheduleSkills(w.capabilities, w.skills, schedule); err != nil {
		return personaServeWiringError{stage: "schedule_skills", err: err}
	}
	if _, err := BindPersonaCompensationSkills(w.capabilities, w.skills, band, compa, scenario); err != nil {
		return personaServeWiringError{stage: "compensation_skills", err: err}
	}
	return nil
}

type lazyPersonaReferenceSource struct {
	source personaChatReferenceSource
}

func (s *lazyPersonaReferenceSource) bind(source personaChatReferenceSource) { s.source = source }

func (s *lazyPersonaReferenceSource) ListPersonaReferenceCandidates(ctx context.Context, principal chat.Principal, tenant, conversation, query string) ([]chat.ReferenceCandidate, error) {
	if s == nil || s.source == nil {
		return nil, errPersonaServeWiring
	}
	return s.source.ListPersonaReferenceCandidates(ctx, principal, tenant, conversation, query)
}

func (s *lazyPersonaReferenceSource) LookupPersonaReference(ctx context.Context, tenant, conversation, referenceID string) (personaReferenceFacts, error) {
	if s == nil || s.source == nil {
		return personaReferenceFacts{}, errPersonaServeWiring
	}
	return s.source.LookupPersonaReference(ctx, tenant, conversation, referenceID)
}

type tenantGrantProvider struct{ store *agentskillgrantstore.Store }

func (p tenantGrantProvider) Grants(ctx context.Context, tenant values.TenantId, key agentskills.SkillKey) ([]agentgate.SkillGrant, error) {
	if p.store == nil {
		return nil, errPersonaServeWiring
	}
	scoped, err := p.store.Scoped(tenant)
	if err != nil {
		return nil, err
	}
	return scoped.Grants(ctx, tenant, key)
}

var _ agentgate.GrantProvider = tenantGrantProvider{}
var _ personaChatReferenceSource = (*lazyPersonaReferenceSource)(nil)

type personaInstallationStore struct{ store *agentpersonastore.Store }

type personaCatalogWorkforceIdentity struct{ facts workforce.Facts }

func (r personaCatalogWorkforceIdentity) ResolvePersonaCatalogMemberID(ctx context.Context, tenant values.TenantId, subject string) (string, bool, error) {
	row, found, err := r.facts.Lookup(ctx, tenant, subject)
	if err != nil || !found {
		return "", found, err
	}
	return row.WorkerID.String(), true, nil
}

var _ PersonaCatalogMemberIdentity = personaCatalogWorkforceIdentity{}

func (s personaInstallationStore) ForTenant(ctx context.Context, tenant values.TenantId) (PersonaAudienceInstallationReader, error) {
	if s.store == nil {
		return nil, errPersonaServeWiring
	}
	return s.store.ForTenant(ctx, tenant)
}

var _ PersonaAudienceInstallationStore = personaInstallationStore{}
