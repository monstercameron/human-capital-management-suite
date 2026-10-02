package application

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PersonaAdminCommandSurfaceConfig contains the trusted server dependencies
// used to compose the authenticated persona administration command surface.
// Publication, installation, and lifecycle fencing dependencies may be absent
// only while those operations are intentionally unavailable; they fail closed.
type PersonaAdminCommandSurfaceConfig struct {
	Catalog         productui.PersonaAdminClient
	Roles           roleaccess.Store
	Store           *agentpersonastore.Store
	Profiles        PersonaProfileBuilderSource
	Manifests       PersonaStarterManifestResolverSource
	Instructions    PersonaStarterInstructionsResolverSource
	ReviewAuthority PersonaReviewAuthority
	ReviewWriter    PersonaReviewEvidenceWriter
	Evidence        PersonaAdminPublicationEvidenceResolver
	InstallAuth     PersonaAdminInstallationAuthorizer
	Transitions     PersonaAdminLifecycleTransitions
	Evaluations     PersonaAdminEvaluationRunner
	Now             func() time.Time
	NewEventID      func() string
	// Reactions stores the owner's choice about an agent's reactions (AGENTUX-075).
	Reactions personaReactionSettingWriter
}

// newAdminCommandFactory composes the admin surface from the already-bound
// persona serve wiring and explicit tenant-scoped validation/review sources.
// It is a method so the served persona store, role source, and clock cannot be
// accidentally replaced by request data or a second tenant's wiring.
func (w *personaServeWiring) newAdminCommandFactory(catalog productui.PersonaAdminClient, profiles PersonaProfileBuilderSource, manifests PersonaStarterManifestResolverSource, instructions PersonaStarterInstructionsResolverSource, reviewAuthority PersonaReviewAuthority, reviewWriter PersonaReviewEvidenceWriter, newEventID func() string) (*PersonaAdminCommandFactory, error) {
	if w == nil {
		return nil, ErrPersonaAdminCommandUnavailable
	}
	return NewPersonaAdminCommandSurface(PersonaAdminCommandSurfaceConfig{
		Catalog: catalog, Roles: w.roles, Store: w.store,
		Profiles: profiles, Manifests: manifests, Instructions: instructions,
		ReviewAuthority: reviewAuthority, ReviewWriter: reviewWriter,
		Evidence: w.adminEvidence, InstallAuth: w.adminInstall, Transitions: w.adminTransitions, Reactions: w.reactions,
		Now: w.now, NewEventID: newEventID,
	})
}

// bindAdminEvaluation attaches a trusted evaluator after the ordinary command
// surface has been composed. Serve uses this only for an explicitly configured
// runtime; an absent binding remains the typed evaluation_unavailable state.
func (w *personaServeWiring) bindAdminEvaluation(runner PersonaAdminEvaluationRunner) error {
	if w == nil || runner == nil {
		return ErrPersonaAdminEvaluationUnavailable
	}
	factory, ok := w.adminFactory.(*PersonaAdminCommandFactory)
	if !ok || factory == nil {
		return ErrPersonaAdminEvaluationUnavailable
	}
	executor, ok := factory.executor.(*PersonaAdminLifecycleExecutor)
	if !ok || executor == nil {
		return ErrPersonaAdminEvaluationUnavailable
	}
	executor.Evaluations = runner
	return nil
}

// NewPersonaAdminCommandSurface composes a role-authorized command factory
// and durable lifecycle executor for server wiring. Required draft and review
// authorities are validated at construction; unavailable optional lifecycle
// authorities remain closed at execution time.
func NewPersonaAdminCommandSurface(config PersonaAdminCommandSurfaceConfig) (*PersonaAdminCommandFactory, error) {
	if config.Catalog == nil || config.Roles == nil || config.Store == nil || config.Profiles == nil || config.Manifests == nil || config.Instructions == nil || config.Now == nil || config.NewEventID == nil || (config.ReviewAuthority == nil) != (config.ReviewWriter == nil) {
		return nil, ErrPersonaAdminCommandUnavailable
	}
	clock := personaAdminDraftClock{now: config.Now}
	draftAuthorizer := personaAdminCreateRoleAuthorizer{roles: config.Roles}
	profiles := TenantPersonaProfileBuilder{Source: config.Profiles}
	drafts := &PersonaAdminDraftService{Store: personaAdminDraftStoreAdapter{store: config.Store}, Authorizer: draftAuthorizer, Profiles: profiles, Clock: clock}
	starterDrafts := &PersonaStarterDraftBuilder{Drafts: drafts, Manifests: TenantPersonaStarterManifestResolver{Source: config.Manifests}, Instructions: TenantPersonaStarterInstructionsResolver{Source: config.Instructions}}
	var reviews *PersonaReviewIssuanceService
	if config.ReviewAuthority != nil {
		var err error
		reviews, err = NewPersonaReviewIssuanceService(config.ReviewAuthority, config.ReviewWriter)
		if err != nil {
			return nil, ErrPersonaAdminCommandUnavailable
		}
	}
	authorizer := PersonaAdminCommandRoleAuthorizer{Roles: config.Roles}
	executor := NewPersonaAdminLifecycleExecutor(
		personaAdminLifecycleStoreAdapter{store: config.Store},
		authorizer,
		drafts,
		starterDrafts,
		reviews,
		config.Evidence,
		config.InstallAuth,
		config.Transitions,
		config.Now,
		config.NewEventID,
	)
	executor.Evaluations = config.Evaluations
	executor.Reactions = config.Reactions
	return NewPersonaAdminCommandFactory(config.Catalog, authorizer, executor)
}

type personaAdminDraftClock struct{ now func() time.Time }

func (c personaAdminDraftClock) Now() time.Time {
	if c.now == nil {
		return time.Time{}
	}
	return c.now()
}

type personaAdminDraftStoreAdapter struct{ store *agentpersonastore.Store }

func (a personaAdminDraftStoreAdapter) CreateDraft(ctx context.Context, version agentpersonastore.PersonaVersion, owner, steward agentpersonastore.PersonaOwner, actor string, at time.Time) error {
	principal, ok := trust.FromContext(ctx)
	if a.store == nil || !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant() != version.TenantID || owner.TenantID != version.TenantID || steward.TenantID != version.TenantID || principal.Subject() != actor {
		return ErrPersonaDraftDenied
	}
	tenant, err := a.store.ForTenant(ctx, principal.Tenant())
	if err != nil || tenant == nil {
		return ErrPersonaDraftDenied
	}
	return tenant.CreateDraftWithIcon(ctx, version, owner, steward, actor, at)
}

type personaAdminCreateRoleAuthorizer struct{ roles roleaccess.Store }

func (a personaAdminCreateRoleAuthorizer) AuthorizePersonaCreate(ctx context.Context, request PersonaCreateAuthorization) error {
	if a.roles == nil || request.Principal == nil {
		return ErrPersonaDraftDenied
	}
	actor := PersonaAdminCommandActor{Principal: request.Principal, Tenant: request.Tenant, Subject: request.Principal.Subject()}
	return (PersonaAdminCommandRoleAuthorizer{Roles: a.roles}).AuthorizePersonaAdminCommand(ctx, actor, PersonaAdminCreateDraft, request.PersonaID)
}
