package application

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var ErrPersonaAdminCommandUnavailable = errors.New("application: persona admin command transport unavailable")

// PersonaAdminCommandAction identifies a lifecycle operation requested by an
// authenticated persona administrator.
type PersonaAdminCommandAction string

const (
	PersonaAdminCreateDraft   PersonaAdminCommandAction = "CREATE_DRAFT"
	PersonaAdminCreateVersion PersonaAdminCommandAction = "CREATE_VERSION"
	PersonaAdminRequestReview PersonaAdminCommandAction = "REQUEST_REVIEW"
	PersonaAdminReview        PersonaAdminCommandAction = "REVIEW"
	PersonaAdminPublish       PersonaAdminCommandAction = "PUBLISH"
	PersonaAdminRollback      PersonaAdminCommandAction = "ROLLBACK"
	PersonaAdminInstall       PersonaAdminCommandAction = "INSTALL"
	PersonaAdminSuspend       PersonaAdminCommandAction = "SUSPEND"
	PersonaAdminRetire        PersonaAdminCommandAction = "RETIRE"
)

// PersonaAdminCommand carries only action data. Tenant and actor identity are
// deliberately absent and are supplied from the verified request context.
type PersonaAdminCommand struct {
	Action             PersonaAdminCommandAction
	PersonaID          string
	Version            agentpersona.PersonaVersion
	Starter            *PersonaStarterDraftRequest
	StarterVersionEdit *PersonaStarterVersionRequest
	BusinessOwnerID    string
	TechnicalStewardID string
	Installation       agentpersonastore.PersonaInstallation
	InstallationID     string
	ReviewID           string
	EvaluationRunID    string
	Decision           string
	Reason             string
}

// PersonaAdminCommandActor is derived only from the authenticated request.
type PersonaAdminCommandActor struct {
	Principal *trust.Principal
	Tenant    values.TenantId
	Subject   string
}

// PersonaAdminCommandAuthorizer checks the current action grant for the
// authenticated actor and requested persona in the actor's own tenant.
type PersonaAdminCommandAuthorizer interface {
	AuthorizePersonaAdminCommand(context.Context, PersonaAdminCommandActor, PersonaAdminCommandAction, string) error
}

// PersonaAdminCommandRoleAuthorizer checks the current tenant role grant for
// each lifecycle action. Review evidence issuance performs its own separate
// persona-review grant check.
type PersonaAdminCommandRoleAuthorizer struct{ Roles roleaccess.Store }

// AuthorizePersonaAdminCommand permits only an explicit page action for the
// principal's current effective roles.
func (a PersonaAdminCommandRoleAuthorizer) AuthorizePersonaAdminCommand(ctx context.Context, actor PersonaAdminCommandActor, action PersonaAdminCommandAction, _ string) error {
	if ctx == nil || a.Roles == nil || actor.Principal == nil || !validPersonaAdminActor(actor.Principal) || actor.Tenant != actor.Principal.Tenant() || actor.Subject != actor.Principal.Subject() {
		return ErrPersonaAdminCommandUnavailable
	}
	requestPrincipal, ok := trust.FromContext(ctx)
	if !ok || requestPrincipal == nil || requestPrincipal.Subject() != actor.Subject || requestPrincipal.Tenant() != actor.Tenant || requestPrincipal.SubjectKind() != trust.SubjectKindHuman {
		return ErrPersonaAdminCommandUnavailable
	}
	required, ok := personaAdminRoleAction(action)
	if !ok {
		return ErrPersonaAdminCommandUnavailable
	}
	snapshot, err := a.Roles.Load(ctx, actor.Tenant, actor.Principal.OrganizationScopeID())
	if err != nil {
		return ErrPersonaAdminCommandUnavailable
	}
	roles := roleaccess.AssignedRoles(snapshot, actor.Subject, actor.Principal.Roles())
	permissions := roleaccess.EffectivePagePermissions(snapshot, roles)
	allowed := roleaccess.CanPageAction(permissions, string(productui.PagePersonaAdmin), required)
	if len(snapshot.FeaturePermissions) > 0 {
		feature := string(productui.FeatureActions)
		if required == roleaccess.ActionView {
			feature = string(productui.FeatureContent)
		}
		allowed = roleaccess.CanFeatureAction(permissions, roleaccess.EffectiveFeaturePermissions(snapshot, roles), string(productui.PagePersonaAdmin), feature, required)
	}
	if !allowed {
		return ErrPersonaAdminCommandUnavailable
	}
	return nil
}

func personaAdminRoleAction(action PersonaAdminCommandAction) (string, bool) {
	switch action {
	case PersonaAdminReview:
		return roleaccess.ActionView, true
	case PersonaAdminCreateDraft:
		return roleaccess.ActionCreate, true
	case PersonaAdminCreateVersion, PersonaAdminRequestReview, PersonaAdminPublish, PersonaAdminRollback, PersonaAdminInstall, PersonaAdminSuspend:
		return roleaccess.ActionUpdate, true
	case PersonaAdminRetire:
		return roleaccess.ActionDelete, true
	default:
		return "", false
	}
}

// PersonaAdminCommandExecutor routes validated commands through application
// lifecycle services. Implementations must use the durable persona store and
// evidence authorities; they must never accept caller-asserted review or
// evaluation outcomes.
type PersonaAdminCommandExecutor interface {
	ExecutePersonaAdminCommand(context.Context, PersonaAdminCommandActor, PersonaAdminCommand) error
}

// PersonaAdminCommandFactory binds lifecycle controls to a metadata catalog.
// It creates a client only for a verified human principal and reauthorizes each
// command before calling the application lifecycle executor.
type PersonaAdminCommandFactory struct {
	catalog    PersonaAdminRoute
	authorizer PersonaAdminCommandAuthorizer
	executor   PersonaAdminCommandExecutor
}

// NewPersonaAdminCommandFactory builds a server-authenticated command factory.
// Missing command authorities fail closed at construction.
func NewPersonaAdminCommandFactory(catalog productui.PersonaAdminClient, authorizer PersonaAdminCommandAuthorizer, executor PersonaAdminCommandExecutor) (*PersonaAdminCommandFactory, error) {
	route, err := NewPersonaAdminRoute(catalog)
	if err != nil || authorizer == nil || executor == nil {
		return nil, ErrPersonaAdminCommandUnavailable
	}
	return &PersonaAdminCommandFactory{catalog: route, authorizer: authorizer, executor: executor}, nil
}

// ClientForRequest returns a metadata and lifecycle client bound to the
// verified human principal in ctx. Untrusted tenant and principal fields never
// enter the command executor.
func (f *PersonaAdminCommandFactory) ClientForRequest(ctx context.Context) productui.PersonaAdminClient {
	if f == nil || f.authorizer == nil || f.executor == nil {
		return nil
	}
	catalog := f.catalog.ClientForRequest(ctx)
	if catalog == nil {
		return nil
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return nil
	}
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	return personaAdminCommandClient{catalog: catalog, ctx: ctx, actor: actor, authorizer: f.authorizer, executor: f.executor}
}

// Execute submits an action-only command using the principal carried by ctx.
// It supports draft/version creation and installation controls in addition to
// the lifecycle actions exposed by the catalog page.
func (f *PersonaAdminCommandFactory) Execute(ctx context.Context, command PersonaAdminCommand) error {
	if f == nil || ctx == nil || f.authorizer == nil || f.executor == nil {
		return personaAdminCommandError(personaAdminCodeUnavailable, ErrPersonaAdminCommandUnavailable)
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || !validPersonaAdminActor(principal) {
		return personaAdminCommandError(personaAdminCodeForbidden, ErrPersonaAdminCommandUnavailable)
	}
	if !validPersonaAdminCommand(command) {
		return personaAdminCommandError(personaAdminCodeInvalid, ErrPersonaAdminCommandUnavailable)
	}
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	if err := f.authorizer.AuthorizePersonaAdminCommand(ctx, actor, command.Action, command.PersonaID); err != nil {
		return personaAdminCommandError(personaAdminCodeForbidden, err)
	}
	if err := f.executor.ExecutePersonaAdminCommand(ctx, actor, command); err != nil {
		return classifyPersonaAdminCommandError(err)
	}
	return nil
}

// ExecutePersonaAdminCommand maps the JSON-safe product UI request into the
// typed application command and then uses the same authenticated authorization
// path as the lifecycle client. Unsupported overrides are rejected rather than
// silently ignored.
func (f *PersonaAdminCommandFactory) ExecutePersonaAdminCommand(ctx context.Context, request productui.PersonaAdminCommandRequest) error {
	if strings.TrimSpace(request.Action) != request.Action {
		return personaAdminCommandError(personaAdminCodeInvalid, ErrPersonaAdminCommandUnavailable)
	}
	command := PersonaAdminCommand{Action: PersonaAdminCommandAction(request.Action), PersonaID: request.PersonaID, ReviewID: request.ReviewID, EvaluationRunID: request.EvaluationRunID, Decision: request.Decision, Reason: request.Reason}
	switch command.Action {
	case PersonaAdminCreateDraft:
		if !personaAdminRequestHasSupportedFields(request) {
			return personaAdminCommandError(personaAdminCodeInvalid, ErrPersonaAdminCommandUnavailable)
		}
		command.Starter = &PersonaStarterDraftRequest{StarterID: request.StarterID, StarterVersion: request.StarterVersion, PersonaID: request.PersonaID, AvatarRef: request.AvatarRef, OrganizationScopes: append([]string(nil), request.OrganizationScopes...), ManifestID: request.ManifestID, BusinessOwnerID: request.BusinessOwnerID, TechnicalStewardID: request.TechnicalStewardID}
	case PersonaAdminCreateVersion:
		channels, ok := personaAdminChannelClasses(request.AllowedChannels)
		if !ok || len(request.OrganizationScopes) > 0 || request.AvatarRef != "" || request.ManifestID != "" || request.ReviewID != "" || request.EvaluationRunID != "" || request.Decision != "" || request.Reason != "" || request.ConversationID != "" {
			return personaAdminCommandError(personaAdminCodeInvalid, ErrPersonaAdminCommandUnavailable)
		}
		command.StarterVersionEdit = &PersonaStarterVersionRequest{StarterID: request.StarterID, StarterVersion: request.StarterVersion, Handle: request.Handle, DisplayName: request.DisplayName, Purpose: request.Purpose, Instructions: request.Instructions, ChannelClasses: channels, BusinessOwnerID: request.BusinessOwnerID, TechnicalStewardID: request.TechnicalStewardID}
	case PersonaAdminInstall:
		if !personaAdminLifecycleRequestHasSupportedFields(request) || request.ReviewID != "" || request.EvaluationRunID != "" || request.Decision != "" || request.Reason != "" {
			return personaAdminCommandError(personaAdminCodeInvalid, ErrPersonaAdminCommandUnavailable)
		}
		command.Installation = agentpersonastore.PersonaInstallation{PersonaID: request.PersonaID, ConversationID: request.ConversationID}
	case PersonaAdminRequestReview, PersonaAdminReview, PersonaAdminPublish, PersonaAdminRollback, PersonaAdminSuspend, PersonaAdminRetire:
		isReview := command.Action == PersonaAdminRequestReview || command.Action == PersonaAdminReview
		if !personaAdminLifecycleRequestHasSupportedFields(request) || request.ConversationID != "" || (!isReview && request.Decision != "") || (command.Action != PersonaAdminPublish && (request.ReviewID != "" || request.EvaluationRunID != "")) || (isReview && request.Reason != "") {
			return personaAdminCommandError(personaAdminCodeInvalid, ErrPersonaAdminCommandUnavailable)
		}
	default:
		return personaAdminCommandError(personaAdminCodeInvalid, ErrPersonaAdminCommandUnavailable)
	}
	return f.Execute(ctx, command)
}

// CreatePersonaDraft submits a starter-backed draft from the admin editor.
func (c personaAdminCommandClient) CreatePersonaDraft(ctx context.Context, request productui.PersonaAdminDraftInput) error {
	if c.ctx == nil || ctx == nil {
		return ErrPersonaAdminCommandUnavailable
	}
	commandRequest := request.PersonaAdminCommandRequest
	if !personaAdminRequestHasSupportedFields(commandRequest) {
		return ErrPersonaAdminCommandUnavailable
	}
	return c.execute(PersonaAdminCommand{Action: PersonaAdminCreateDraft, PersonaID: commandRequest.PersonaID, Starter: &PersonaStarterDraftRequest{StarterID: commandRequest.StarterID, StarterVersion: commandRequest.StarterVersion, PersonaID: commandRequest.PersonaID, AvatarRef: commandRequest.AvatarRef, OrganizationScopes: append([]string(nil), commandRequest.OrganizationScopes...), ManifestID: commandRequest.ManifestID, BusinessOwnerID: commandRequest.BusinessOwnerID, TechnicalStewardID: commandRequest.TechnicalStewardID}})
}

func personaAdminRequestHasSupportedFields(request productui.PersonaAdminCommandRequest) bool {
	return request.Handle == "" && request.DisplayName == "" && request.Purpose == "" && request.Instructions == "" && len(request.AllowedChannels) == 0 && request.ConversationID == "" && request.ReviewID == "" && request.EvaluationRunID == "" && request.Decision == "" && request.Reason == ""
}

func personaAdminLifecycleRequestHasSupportedFields(request productui.PersonaAdminCommandRequest) bool {
	return request.StarterID == "" && request.StarterVersion == 0 && request.AvatarRef == "" && len(request.OrganizationScopes) == 0 && request.ManifestID == "" && request.BusinessOwnerID == "" && request.TechnicalStewardID == "" && request.Handle == "" && request.DisplayName == "" && request.Purpose == "" && request.Instructions == "" && len(request.AllowedChannels) == 0
}

func personaAdminChannelClasses(values []string) ([]agentpersona.ChannelClass, bool) {
	classes := make([]agentpersona.ChannelClass, 0, len(values))
	for _, value := range values {
		switch agentpersona.ChannelClass(value) {
		case agentpersona.ChannelPrivate, agentpersona.ChannelPublic:
			classes = append(classes, agentpersona.ChannelClass(value))
		default:
			return nil, false
		}
	}
	return classes, true
}

type personaAdminCommandClient struct {
	catalog    productui.PersonaAdminClient
	ctx        context.Context
	actor      PersonaAdminCommandActor
	authorizer PersonaAdminCommandAuthorizer
	executor   PersonaAdminCommandExecutor
}

func (c personaAdminCommandClient) Snapshot(_ context.Context, req productui.PersonaAdminSnapshotRequest) (productui.PersonaAdminSnapshot, error) {
	snapshot, err := c.catalog.Snapshot(c.ctx, req)
	if err != nil {
		return snapshot, err
	}
	snapshot.CommandPermissionsAvailable = true
	snapshot.AllowedCommands = nil
	for _, action := range []PersonaAdminCommandAction{PersonaAdminCreateDraft, PersonaAdminCreateVersion, PersonaAdminRequestReview, PersonaAdminReview, PersonaAdminPublish, PersonaAdminRollback, PersonaAdminInstall, PersonaAdminSuspend, PersonaAdminRetire} {
		if availability, ok := c.executor.(interface {
			PersonaAdminCommandAvailable(context.Context, PersonaAdminCommandAction) bool
		}); ok && !availability.PersonaAdminCommandAvailable(c.ctx, action) {
			continue
		}
		if c.authorizer.AuthorizePersonaAdminCommand(c.ctx, c.actor, action, "") == nil {
			snapshot.AllowedCommands = append(snapshot.AllowedCommands, string(action))
		}
	}
	return snapshot, nil
}

func (c personaAdminCommandClient) Preview(_ context.Context, req productui.PersonaAdminPreviewRequest) (productui.PersonaAdminPreview, error) {
	return c.catalog.Preview(c.ctx, req)
}

func (c personaAdminCommandClient) RequestReview(id string) error {
	return c.execute(PersonaAdminCommand{Action: PersonaAdminRequestReview, PersonaID: id})
}

func (c personaAdminCommandClient) ReviewPersona(id, decision string) error {
	return c.execute(PersonaAdminCommand{Action: PersonaAdminReview, PersonaID: id, Decision: decision})
}

func (c personaAdminCommandClient) PublishPersona(id string) error {
	return c.execute(PersonaAdminCommand{Action: PersonaAdminPublish, PersonaID: id})
}

func (c personaAdminCommandClient) RollbackPersona(id string) error {
	return c.execute(PersonaAdminCommand{Action: PersonaAdminRollback, PersonaID: id})
}

func (c personaAdminCommandClient) SuspendPersona(id string) error {
	return c.execute(PersonaAdminCommand{Action: PersonaAdminSuspend, PersonaID: id})
}

func (c personaAdminCommandClient) RetirePersona(id string) error {
	return c.execute(PersonaAdminCommand{Action: PersonaAdminRetire, PersonaID: id})
}

func (c personaAdminCommandClient) execute(command PersonaAdminCommand) error {
	if c.catalog == nil || c.ctx == nil || c.authorizer == nil || c.executor == nil || !validPersonaAdminActor(c.actor.Principal) || !validPersonaAdminCommand(command) {
		return personaAdminCommandError(personaAdminCodeInvalid, ErrPersonaAdminCommandUnavailable)
	}
	if err := c.authorizer.AuthorizePersonaAdminCommand(c.ctx, c.actor, command.Action, command.PersonaID); err != nil {
		return personaAdminCommandError(personaAdminCodeForbidden, err)
	}
	return classifyPersonaAdminCommandError(c.executor.ExecutePersonaAdminCommand(c.ctx, c.actor, command))
}

func validPersonaAdminActor(p *trust.Principal) bool {
	return p != nil && p.SubjectKind() == trust.SubjectKindHuman && p.Subject() != "" && strings.TrimSpace(p.Subject()) == p.Subject() && p.Tenant().Validate() == nil
}

func validPersonaAdminCommand(command PersonaAdminCommand) bool {
	if strings.TrimSpace(command.PersonaID) == "" || strings.TrimSpace(command.PersonaID) != command.PersonaID {
		return false
	}
	switch command.Action {
	case PersonaAdminCreateDraft:
		if command.Action == PersonaAdminCreateDraft && command.Starter != nil {
			return command.Version.Digest == "" && command.StarterVersionEdit == nil && command.PersonaID == command.Starter.PersonaID && command.Starter.BusinessOwnerID != "" && command.Starter.TechnicalStewardID != ""
		}
		return command.Version.Verify() == nil && command.Version.Profile.PersonaID == command.PersonaID && command.BusinessOwnerID != "" && command.TechnicalStewardID != "" && command.BusinessOwnerID != command.TechnicalStewardID
	case PersonaAdminCreateVersion:
		if command.StarterVersionEdit != nil {
			return command.Version.Digest == "" && command.StarterVersionEdit.StarterID != "" && command.StarterVersionEdit.StarterVersion > 0
		}
		return command.Version.Verify() == nil && command.Version.Profile.PersonaID == command.PersonaID && command.BusinessOwnerID != "" && command.TechnicalStewardID != "" && command.BusinessOwnerID != command.TechnicalStewardID
	case PersonaAdminRequestReview, PersonaAdminPublish, PersonaAdminRollback:
		return command.Installation.InstallationID == "" && command.InstallationID == "" && command.Version.Profile.PersonaID == "" &&
			(command.Action != PersonaAdminRequestReview || command.Decision == "" || command.Decision == "APPROVE" || command.Decision == "REJECT")
	case PersonaAdminReview:
		return command.Installation.InstallationID == "" && command.InstallationID == "" && command.Version.Digest == "" && command.Reason == "" && command.ReviewID == "" && command.EvaluationRunID == "" && (command.Decision == "APPROVE" || command.Decision == "REJECT")
	case PersonaAdminInstall:
		return command.Installation.PersonaID == command.PersonaID && command.Installation.TenantID == "" && command.Installation.InstallerID == "" && command.Installation.InstallationID == "" && command.Installation.PersonaVersion == 0 && command.Installation.ConversationID != "" && command.Installation.ConversationClass == "" && command.Installation.State == "" && command.Installation.Revision == 0 && command.Installation.RevocationEpoch == 0 && emptyPersonaChannelPolicy(command.Installation.ChannelPolicy)
	case PersonaAdminSuspend, PersonaAdminRetire:
		return command.Installation.InstallationID == "" && command.InstallationID == "" && command.Version.Digest == ""
	default:
		return false
	}
}

func emptyPersonaChannelPolicy(policy agentpersonastore.ChannelPolicy) bool {
	return policy.MaxTier == "" && policy.PlacementClass == "" && len(policy.AllowedDataClasses) == 0 && !policy.AlwaysPrivate && !policy.ConversationSearchAllowed && len(policy.AllowedChannelClasses) == 0 && !policy.AllowExternalMembers && !policy.AllowCrossCompanyMembers
}

var _ productui.PersonaAdminClient = personaAdminCommandClient{}
var _ productui.PersonaAdminCommandTransport = (*PersonaAdminCommandFactory)(nil)
var _ productui.PersonaAdminDraftClient = personaAdminCommandClient{}
