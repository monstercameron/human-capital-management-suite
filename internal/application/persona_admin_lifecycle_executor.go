package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrPersonaAdminLifecycleUnavailable = errors.New("application: persona admin lifecycle dependency unavailable")
	ErrPersonaAdminRuntimeUnavailable   = errors.New("This version cannot be published yet: it has no runtime identity. Ask the person who manages this installation.")
)

// PersonaAdminLifecycleTenant exposes the durable operations used by the
// command executor. Implementations are tenant-scoped agentpersonastore
// lifecycle stores; no request may select the tenant store directly.
type PersonaAdminLifecycleTenant interface {
	ListVersions(context.Context, string) ([]agentpersonastore.PersonaVersion, error)
	Lifecycle(context.Context, string, int64) (agentpersonastore.LifecycleState, error)
	ListLifecycle(context.Context, string, int64) ([]agentpersonastore.LifecycleEvent, error)
	AppendLifecycle(context.Context, agentpersonastore.LifecycleEvent) error
	Publish(context.Context, agentpersonastore.LifecycleEvent, agentpersonastore.PublicationEvidence) error
	Install(context.Context, agentpersonastore.PersonaInstallation) error
	RetireActiveInstallation(context.Context, string, string, string, string) (agentpersonastore.PersonaInstallation, bool, error)
	ReplaceActiveInstallation(context.Context, agentpersonastore.PersonaInstallation) (agentpersonastore.PersonaInstallation, bool, error)
}

// PersonaAdminLifecycleStore creates tenant-scoped lifecycle stores.
type PersonaAdminLifecycleStore interface {
	ForTenant(context.Context, values.TenantId) (PersonaAdminLifecycleTenant, error)
}

type personaAdminLifecycleStoreAdapter struct{ store *agentpersonastore.Store }

func (a personaAdminLifecycleStoreAdapter) ForTenant(ctx context.Context, tenant values.TenantId) (PersonaAdminLifecycleTenant, error) {
	if a.store == nil {
		return nil, ErrPersonaAdminLifecycleUnavailable
	}
	return a.store.ForTenant(ctx, tenant)
}

// PersonaAdminAtomicVersionTenant is implemented by the tenant store's atomic
// version-plus-DRAFT operation. It prevents an immutable version from being
// left behind without its lifecycle event.
type PersonaAdminAtomicVersionTenant interface {
	CreateVersionDraft(context.Context, agentpersonastore.PersonaVersion, string, time.Time) error
}

// PersonaAdminPublicationEvidenceResolver returns evidence references created
// by trusted review and evaluation authorities for this exact version. The
// store revalidates both references in its publication transaction.
type PersonaAdminPublicationEvidenceResolver interface {
	ResolvePersonaPublicationEvidence(context.Context, values.TenantId, string, int64) (agentpersonastore.PublicationEvidence, error)
}

// PersonaAdminInstallationAuthorizer derives a placement from current chat
// membership and channel policy. It must return server-verified installation
// fields and ignore caller-supplied actor, tenant, and installation ID.
type PersonaAdminInstallationAuthorizer interface {
	AuthorizePersonaInstallation(context.Context, PersonaAdminCommandActor, agentpersonastore.PersonaInstallation) (agentpersonastore.PersonaInstallation, error)
}

// PersonaAdminLifecycleTransitions fences active and waiting persona work
// before recording a persona-wide suspend or retirement.
type PersonaAdminLifecycleTransitions interface {
	SuspendPersona(context.Context, PersonaAdminCommandActor, string, string) error
	RollbackPersona(context.Context, PersonaAdminCommandActor, string, string) error
	RetirePersona(context.Context, PersonaAdminCommandActor, string, string) error
}

// PersonaAdminEvaluationRunner executes and records one immutable version
// through the same evaluator boundary used by the publication gate.
type PersonaAdminEvaluationRunner interface {
	RunPersonaEvaluation(context.Context, PersonaAdminCommandActor, string) (productui.PersonaAdminEvaluationResult, error)
}

// PersonaAdminRuntimeProvisioner creates the exact version-scoped service
// identity and model route required to execute a version. Publication calls
// this boundary after evidence validation and before any lifecycle mutation.
type PersonaAdminRuntimeProvisioner interface {
	ProvisionPersonaRuntime(context.Context, PersonaAdminCommandActor, agentpersonastore.PersonaVersion, agentpersona.PersonaProfile, agentpersonastore.PublicationEvidence) error
}

// PersonaAdminLifecycleExecutor runs authenticated commands through the
// tenant-scoped persona store and the dedicated draft/review services.
type PersonaAdminLifecycleExecutor struct {
	Store         PersonaAdminLifecycleStore
	Authorizer    PersonaAdminCommandAuthorizer
	Drafts        *PersonaAdminDraftService
	StarterDrafts *PersonaStarterDraftBuilder
	Reviews       *PersonaReviewIssuanceService
	Evidence      PersonaAdminPublicationEvidenceResolver
	InstallAuth   PersonaAdminInstallationAuthorizer
	Transitions   PersonaAdminLifecycleTransitions
	Evaluations   PersonaAdminEvaluationRunner
	Runtime       PersonaAdminRuntimeProvisioner
	Now           func() time.Time
	NewEventID    func() string
}

// NewPersonaAdminLifecycleExecutor creates an executor. Dependencies are
// checked per command so read-only deployments can keep the catalog available
// while every unavailable mutation remains fail-closed.
func NewPersonaAdminLifecycleExecutor(store PersonaAdminLifecycleStore, authorizer PersonaAdminCommandAuthorizer, drafts *PersonaAdminDraftService, starterDrafts *PersonaStarterDraftBuilder, reviews *PersonaReviewIssuanceService, evidence PersonaAdminPublicationEvidenceResolver, installAuth PersonaAdminInstallationAuthorizer, transitions PersonaAdminLifecycleTransitions, now func() time.Time, newEventID func() string, runtime ...PersonaAdminRuntimeProvisioner) *PersonaAdminLifecycleExecutor {
	executor := &PersonaAdminLifecycleExecutor{Store: store, Authorizer: authorizer, Drafts: drafts, StarterDrafts: starterDrafts, Reviews: reviews, Evidence: evidence, InstallAuth: installAuth, Transitions: transitions, Now: now, NewEventID: newEventID}
	if len(runtime) == 1 {
		executor.Runtime = runtime[0]
	}
	return executor
}

// PersonaAdminCommandAvailable projects current reviewer authority for the UI.
func (e *PersonaAdminLifecycleExecutor) PersonaAdminCommandAvailable(ctx context.Context, action PersonaAdminCommandAction) bool {
	if e == nil || ctx == nil {
		return false
	}
	if action == PersonaAdminReview {
		return e.Reviews != nil && e.Reviews.Authorize(ctx) == nil
	}
	if action == PersonaAdminRunEvaluation {
		if e.Evaluations == nil {
			return false
		}
		// A runner bound to one tenant reports itself unavailable to the
		// others, so their administrators are not offered a dead action.
		if scoped, ok := e.Evaluations.(interface {
			PersonaEvaluationAvailable(context.Context) bool
		}); ok {
			return scoped.PersonaEvaluationAvailable(ctx)
		}
		return true
	}
	if action == PersonaAdminPublish {
		if availability, ok := e.Runtime.(interface{ PersonaRuntimeAvailable(context.Context) bool }); ok {
			return availability.PersonaRuntimeAvailable(ctx)
		}
		return e.Runtime != nil
	}
	return true
}

// ExecutePersonaAdminCommand delegates each operation to its durable lifecycle
// service. Actor and tenant values are checked against the authenticated trust
// principal before any mutation.
func (e *PersonaAdminLifecycleExecutor) ExecutePersonaAdminCommand(ctx context.Context, actor PersonaAdminCommandActor, command PersonaAdminCommand) error {
	if e == nil || ctx == nil || !validPersonaAdminActor(actor.Principal) || !personaAdminActorBound(ctx, actor) || !validPersonaAdminCommand(command) {
		return ErrPersonaAdminCommandUnavailable
	}
	if e.Authorizer == nil || e.Authorizer.AuthorizePersonaAdminCommand(ctx, actor, command.Action, command.PersonaID) != nil {
		return ErrPersonaAdminCommandUnavailable
	}
	switch command.Action {
	case PersonaAdminCreateDraft:
		return e.createDraft(ctx, command)
	case PersonaAdminCreateVersion:
		return e.createVersion(ctx, actor, command)
	case PersonaAdminRequestReview:
		return e.requestReview(ctx, actor, command)
	case PersonaAdminReview:
		return e.recordReview(ctx, actor, command)
	case PersonaAdminRunEvaluation:
		_, err := e.runEvaluation(ctx, actor, command)
		return err
	case PersonaAdminPublish:
		return e.publish(ctx, actor, command)
	case PersonaAdminRollback:
		if e.Transitions == nil {
			return ErrPersonaAdminLifecycleUnavailable
		}
		return e.Transitions.RollbackPersona(ctx, actor, command.PersonaID, command.Reason)
	case PersonaAdminInstall:
		return e.install(ctx, actor, command)
	case PersonaAdminUninstall:
		return e.uninstall(ctx, actor, command)
	case PersonaAdminReinstall:
		return e.reinstall(ctx, actor, command)
	case PersonaAdminSuspend, PersonaAdminRetire:
		return e.transition(ctx, actor, command)
	default:
		return ErrPersonaAdminCommandUnavailable
	}
}

// ExecutePersonaAdminCommandWithResult preserves the ordinary authorization
// path while returning only the bounded evaluation receipt needed by the UI.
func (e *PersonaAdminLifecycleExecutor) ExecutePersonaAdminCommandWithResult(ctx context.Context, actor PersonaAdminCommandActor, command PersonaAdminCommand) (productui.PersonaAdminEvaluationResult, error) {
	if command.Action != PersonaAdminRunEvaluation {
		return productui.PersonaAdminEvaluationResult{}, e.ExecutePersonaAdminCommand(ctx, actor, command)
	}
	if e == nil || ctx == nil || !validPersonaAdminActor(actor.Principal) || !personaAdminActorBound(ctx, actor) || !validPersonaAdminCommand(command) {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminCommandUnavailable
	}
	if e.Authorizer == nil || e.Authorizer.AuthorizePersonaAdminCommand(ctx, actor, command.Action, command.PersonaID) != nil {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminCommandUnavailable
	}
	return e.runEvaluation(ctx, actor, command)
}

func (e *PersonaAdminLifecycleExecutor) runEvaluation(ctx context.Context, actor PersonaAdminCommandActor, command PersonaAdminCommand) (productui.PersonaAdminEvaluationResult, error) {
	if e.Evaluations == nil {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminEvaluationUnavailable
	}
	return e.Evaluations.RunPersonaEvaluation(ctx, actor, command.PersonaID)
}

func personaAdminActorBound(ctx context.Context, actor PersonaAdminCommandActor) bool {
	principal, ok := trust.FromContext(ctx)
	return ok && principal != nil && principal.Subject() == actor.Subject && principal.Tenant() == actor.Tenant && principal.SubjectKind() == actor.Principal.SubjectKind()
}

func (e *PersonaAdminLifecycleExecutor) createDraft(ctx context.Context, command PersonaAdminCommand) error {
	if command.Starter != nil {
		if e.StarterDrafts == nil {
			return ErrPersonaAdminLifecycleUnavailable
		}
		_, err := e.StarterDrafts.CreateDraft(ctx, *command.Starter)
		return err
	}
	if e.Drafts == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	_, err := e.Drafts.CreateDraft(ctx, PersonaDraftRequest{Version: command.Version, BusinessOwnerID: command.BusinessOwnerID, TechnicalStewardID: command.TechnicalStewardID})
	return err
}

func (e *PersonaAdminLifecycleExecutor) createVersion(ctx context.Context, actor PersonaAdminCommandActor, command PersonaAdminCommand) error {
	if e.Store == nil || e.Drafts == nil || e.Drafts.Profiles == nil || e.Now == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	versionCandidate := command.Version
	if command.StarterVersionEdit != nil {
		if e.StarterDrafts == nil {
			return ErrPersonaAdminLifecycleUnavailable
		}
		store, err := e.Store.ForTenant(ctx, actor.Tenant)
		if err != nil || store == nil {
			return ErrPersonaAdminLifecycleUnavailable
		}
		currentRow, _, err := latestPersonaVersion(ctx, store, command.PersonaID, "")
		if err != nil || currentRow.TenantID != actor.Tenant {
			return ErrPersonaAdminLifecycleUnavailable
		}
		var current agentpersona.PersonaProfile
		if err := json.Unmarshal(currentRow.Profile, &current); err != nil {
			return ErrPersonaAdminLifecycleUnavailable
		}
		currentVersion, err := buildPersonaProfile(ctx, e.Drafts.Profiles, actor.Tenant, current)
		if err != nil || currentVersion.Digest != currentRow.ContentDigest || currentVersion.Profile.Version == ^uint32(0) {
			return ErrPersonaDraftInvalid
		}
		edit := *command.StarterVersionEdit
		edit.Version = currentVersion.Profile.Version + 1
		versionCandidate, err = e.StarterDrafts.BuildVersion(ctx, currentVersion, edit)
		if err != nil {
			return err
		}
	}
	validated, err := buildPersonaProfile(ctx, e.Drafts.Profiles, actor.Tenant, versionCandidate.Profile)
	if err != nil || validated.Digest != versionCandidate.Digest || validated.Profile.PersonaID != command.PersonaID {
		return ErrPersonaDraftInvalid
	}
	tenant, err := e.Store.ForTenant(ctx, actor.Tenant)
	if err != nil || tenant == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	writer, ok := tenant.(PersonaAdminAtomicVersionTenant)
	if !ok {
		return ErrPersonaAdminLifecycleUnavailable
	}
	profileJSON, err := json.Marshal(validated.Profile)
	if err != nil {
		return fmt.Errorf("application: encode validated persona version: %w", err)
	}
	now := e.Now().UTC()
	version := agentpersonastore.PersonaVersion{TenantID: actor.Tenant, PersonaID: validated.Profile.PersonaID, Version: int64(validated.Profile.Version), AgentVersion: fmt.Sprintf("%s@%d", validated.Profile.Manifest.ID, validated.Profile.Manifest.Version), Handle: validated.Profile.Handle, DisplayName: validated.Profile.DisplayName, Profile: profileJSON, ContentDigest: validated.Digest, CreatedAt: now}
	return writer.CreateVersionDraft(ctx, version, actor.Subject, now)
}

func (e *PersonaAdminLifecycleExecutor) requestReview(ctx context.Context, actor PersonaAdminCommandActor, command PersonaAdminCommand) error {
	if e.Store == nil || !validPersonaAdminActor(actor.Principal) {
		return ErrPersonaAdminLifecycleUnavailable
	}
	tenant, err := e.Store.ForTenant(ctx, actor.Tenant)
	if err != nil || tenant == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	version, state, err := latestPersonaVersion(ctx, tenant, command.PersonaID, "")
	if err != nil || (state != agentpersonastore.StateDraft && state != agentpersonastore.StateInReview) {
		return ErrPersonaAdminLifecycleUnavailable
	}
	decision := command.Decision
	if decision != "" {
		if e.Reviews == nil {
			return ErrPersonaAdminLifecycleUnavailable
		}
		if err := e.Reviews.Authorize(ctx); err != nil {
			return err
		}
	}
	if state == agentpersonastore.StateDraft {
		now, eventID, err := e.eventIdentity()
		if err != nil {
			return err
		}
		if err := tenant.AppendLifecycle(ctx, agentpersonastore.LifecycleEvent{TenantID: actor.Tenant, EventID: eventID, PersonaID: command.PersonaID, PersonaVersion: version.Version, From: agentpersonastore.StateDraft, To: agentpersonastore.StateInReview, Reason: "Independent persona review requested", ActorID: actor.Subject, OccurredAt: now}); err != nil {
			return err
		}
	}
	if decision == "" {
		return nil
	}
	_, err = e.Reviews.Issue(ctx, command.PersonaID, version.Version, decision)
	return err
}

// recordReview never changes lifecycle state. A reviewer can decide only the
// latest author version that has already entered the review queue.
func (e *PersonaAdminLifecycleExecutor) recordReview(ctx context.Context, actor PersonaAdminCommandActor, command PersonaAdminCommand) error {
	if e.Store == nil || e.Reviews == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	tenant, err := e.Store.ForTenant(ctx, actor.Tenant)
	if err != nil || tenant == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	version, state, err := latestPersonaVersion(ctx, tenant, command.PersonaID, "")
	if err != nil || state != agentpersonastore.StateInReview {
		return ErrPersonaAdminLifecycleUnavailable
	}
	_, err = e.Reviews.Issue(ctx, command.PersonaID, version.Version, command.Decision)
	return err
}

func (e *PersonaAdminLifecycleExecutor) publish(ctx context.Context, actor PersonaAdminCommandActor, command PersonaAdminCommand) error {
	if e.Store == nil || e.Evidence == nil || e.Drafts == nil || e.Drafts.Profiles == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	tenant, err := e.Store.ForTenant(ctx, actor.Tenant)
	if err != nil || tenant == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	version, state, err := latestPersonaVersion(ctx, tenant, command.PersonaID, "")
	if err != nil || (state != agentpersonastore.StateInReview && state != agentpersonastore.StateSuspended && state != agentpersonastore.StatePublished) {
		return ErrPersonaAdminLifecycleUnavailable
	}
	resume, canResume := e.Transitions.(interface {
		ResumePersonaVersion(context.Context, PersonaAdminCommandActor, string, int64) error
	})
	if (state == agentpersonastore.StateSuspended || state == agentpersonastore.StatePublished) && !canResume {
		return ErrPersonaAdminLifecycleUnavailable
	}
	var profile agentpersona.PersonaProfile
	if json.Unmarshal(version.Profile, &profile) != nil {
		return ErrPersonaDraftInvalid
	}
	validated, err := buildPersonaProfile(ctx, e.Drafts.Profiles, actor.Tenant, profile)
	if err != nil || validated.Digest != version.ContentDigest || validated.Profile.PersonaID != command.PersonaID || int64(validated.Profile.Version) != version.Version {
		return ErrPersonaDraftInvalid
	}
	evidence := agentpersonastore.PublicationEvidence{ReviewID: command.ReviewID, EvaluationRunID: command.EvaluationRunID}
	if evidence.ReviewID == "" || evidence.EvaluationRunID == "" {
		evidence, err = e.Evidence.ResolvePersonaPublicationEvidence(ctx, actor.Tenant, command.PersonaID, version.Version)
		if err != nil || strings.TrimSpace(evidence.ReviewID) == "" || strings.TrimSpace(evidence.EvaluationRunID) == "" {
			return ErrPersonaAdminLifecycleUnavailable
		}
	}
	if e.Runtime == nil {
		return ErrPersonaAdminRuntimeUnavailable
	}
	if err := e.Runtime.ProvisionPersonaRuntime(ctx, actor, version, validated.Profile, evidence); err != nil {
		return ErrPersonaAdminRuntimeUnavailable
	}
	now, eventID, err := e.eventIdentity()
	if err != nil {
		return err
	}
	event := agentpersonastore.LifecycleEvent{TenantID: actor.Tenant, EventID: eventID, PersonaID: command.PersonaID, PersonaVersion: version.Version, From: state, To: agentpersonastore.StatePublished, Reason: "Reviewed persona published", ActorID: actor.Subject, OccurredAt: now}
	if state != agentpersonastore.StatePublished {
		if err := tenant.Publish(ctx, event, evidence); err != nil {
			return err
		}
	}
	if canResume {
		return resume.ResumePersonaVersion(ctx, actor, command.PersonaID, version.Version)
	}
	return nil
}

func (e *PersonaAdminLifecycleExecutor) install(ctx context.Context, actor PersonaAdminCommandActor, command PersonaAdminCommand) error {
	if e.Store == nil || e.InstallAuth == nil || e.Now == nil || e.NewEventID == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	tenant, err := e.Store.ForTenant(ctx, actor.Tenant)
	if err != nil || tenant == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	version, state, err := latestPersonaVersion(ctx, tenant, command.PersonaID, agentpersonastore.StatePublished)
	if err != nil || state != agentpersonastore.StatePublished {
		return ErrPersonaAdminLifecycleUnavailable
	}
	now, installationID := e.Now().UTC(), strings.TrimSpace(e.NewEventID())
	if now.IsZero() || installationID == "" {
		return ErrPersonaAdminLifecycleUnavailable
	}
	request := command.Installation
	request.TenantID, request.InstallerID, request.InstallationID = actor.Tenant, actor.Subject, installationID
	request.PersonaVersion, request.State, request.Revision, request.RevocationEpoch = version.Version, agentpersonastore.InstallationActive, 1, 1
	request.CreatedAt, request.UpdatedAt = now, now
	if atomic, ok := e.InstallAuth.(interface {
		AuthorizeAndInstallPersona(context.Context, PersonaAdminCommandActor, agentpersonastore.PersonaInstallation, func(agentpersonastore.PersonaInstallation) error) error
	}); ok {
		return atomic.AuthorizeAndInstallPersona(ctx, actor, request, func(installation agentpersonastore.PersonaInstallation) error {
			return tenant.Install(ctx, installation)
		})
	}
	installation, err := e.InstallAuth.AuthorizePersonaInstallation(ctx, actor, request)
	if err != nil || installation.TenantID != actor.Tenant || installation.InstallerID != actor.Subject || installation.PersonaID != command.PersonaID || installation.PersonaVersion != version.Version || installation.InstallationID != installationID || installation.ConversationID != request.ConversationID {
		return ErrPersonaAdminLifecycleUnavailable
	}
	return tenant.Install(ctx, installation)
}

func (e *PersonaAdminLifecycleExecutor) uninstall(ctx context.Context, actor PersonaAdminCommandActor, command PersonaAdminCommand) error {
	if e.Store == nil || e.InstallAuth == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	tenant, err := e.Store.ForTenant(ctx, actor.Tenant)
	if err != nil || tenant == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	request := command.Installation
	request.TenantID, request.InstallerID = actor.Tenant, actor.Subject
	if _, err := e.InstallAuth.AuthorizePersonaInstallation(ctx, actor, request); err != nil {
		return err
	}
	_, _, err = tenant.RetireActiveInstallation(ctx, command.PersonaID, command.Installation.ConversationID, actor.Subject, "Persona installation removed by an authorized administrator")
	return err
}

func (e *PersonaAdminLifecycleExecutor) reinstall(ctx context.Context, actor PersonaAdminCommandActor, command PersonaAdminCommand) error {
	if e.Store == nil || e.InstallAuth == nil || e.Now == nil || e.NewEventID == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	tenant, err := e.Store.ForTenant(ctx, actor.Tenant)
	if err != nil || tenant == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	version, state, err := latestPersonaVersion(ctx, tenant, command.PersonaID, agentpersonastore.StatePublished)
	if err != nil || state != agentpersonastore.StatePublished {
		return ErrPersonaAdminLifecycleUnavailable
	}
	now, installationID := e.Now().UTC(), strings.TrimSpace(e.NewEventID())
	if now.IsZero() || installationID == "" {
		return ErrPersonaAdminLifecycleUnavailable
	}
	request := command.Installation
	request.TenantID, request.InstallerID, request.InstallationID = actor.Tenant, actor.Subject, installationID
	request.PersonaVersion, request.State, request.Revision, request.RevocationEpoch = version.Version, agentpersonastore.InstallationActive, 1, 1
	request.CreatedAt, request.UpdatedAt = now, now
	installation, err := e.InstallAuth.AuthorizePersonaInstallation(ctx, actor, request)
	if err != nil || installation.TenantID != actor.Tenant || installation.InstallerID != actor.Subject || installation.PersonaID != command.PersonaID || installation.PersonaVersion != version.Version || installation.InstallationID != installationID || installation.ConversationID != request.ConversationID {
		return ErrPersonaAdminLifecycleUnavailable
	}
	_, _, err = tenant.ReplaceActiveInstallation(ctx, installation)
	return err
}

func (e *PersonaAdminLifecycleExecutor) transition(ctx context.Context, actor PersonaAdminCommandActor, command PersonaAdminCommand) error {
	if e.Transitions == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	if command.Action == PersonaAdminSuspend {
		return e.Transitions.SuspendPersona(ctx, actor, command.PersonaID, command.Reason)
	}
	return e.Transitions.RetirePersona(ctx, actor, command.PersonaID, command.Reason)
}

func (e *PersonaAdminLifecycleExecutor) eventIdentity() (time.Time, string, error) {
	if e.Now == nil || e.NewEventID == nil {
		return time.Time{}, "", ErrPersonaAdminLifecycleUnavailable
	}
	now, id := e.Now().UTC(), strings.TrimSpace(e.NewEventID())
	if now.IsZero() || id == "" {
		return time.Time{}, "", ErrPersonaAdminLifecycleUnavailable
	}
	return now, id, nil
}

func latestPersonaVersion(ctx context.Context, store PersonaAdminLifecycleTenant, personaID string, required agentpersonastore.LifecycleState) (agentpersonastore.PersonaVersion, agentpersonastore.LifecycleState, error) {
	versions, err := store.ListVersions(ctx, personaID)
	if err != nil {
		return agentpersonastore.PersonaVersion{}, "", err
	}
	var selected agentpersonastore.PersonaVersion
	var selectedState agentpersonastore.LifecycleState
	for _, version := range versions {
		if strings.TrimSpace(version.PersonaID) != personaID || version.Version <= 0 {
			return agentpersonastore.PersonaVersion{}, "", ErrPersonaAdminLifecycleUnavailable
		}
		state, err := store.Lifecycle(ctx, personaID, version.Version)
		if err != nil {
			return agentpersonastore.PersonaVersion{}, "", err
		}
		if required != "" && state != required {
			continue
		}
		if version.Version > selected.Version {
			selected, selectedState = version, state
		}
	}
	if selected.Version == 0 {
		return agentpersonastore.PersonaVersion{}, "", fmt.Errorf("%w: no eligible version for %s", agentpersonastore.ErrNotFound, personaID)
	}
	return selected, selectedState, nil
}

var _ PersonaAdminCommandExecutor = (*PersonaAdminLifecycleExecutor)(nil)
