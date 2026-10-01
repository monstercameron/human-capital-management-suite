package application

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PersonaAdminSecurityScopes is the durable revocation authority used by
// operator lifecycle controls. Revocation commits before lifecycle changes.
type PersonaAdminSecurityScopes interface {
	RevokePersonaSecurityScope(context.Context, uuid.UUID, agentstore.PersonaSecurityScope, string, time.Time) (int64, error)
}

// PersonaAdminReviewedTransitions revalidates immutable profiles and durable
// publication evidence during rollback. A new draft never displaces an older
// published version until it is explicitly reviewed and published.
type PersonaAdminReviewedTransitions struct {
	Store      PersonaAdminLifecycleStore
	Authorizer PersonaAdminCommandAuthorizer
	Profiles   PersonaProfileBuilder
	Evidence   PersonaAdminPublicationEvidenceResolver
	Security   PersonaAdminSecurityScopes
	TenantUUID func(values.TenantId) uuid.UUID
	Now        func() time.Time
	NewEventID func() string
}

func (s *PersonaAdminReviewedTransitions) SuspendPersona(ctx context.Context, actor PersonaAdminCommandActor, personaID, reason string) error {
	return s.stop(ctx, actor, personaID, reason, PersonaAdminSuspend, agentpersonastore.StateSuspended)
}

func (s *PersonaAdminReviewedTransitions) RetirePersona(ctx context.Context, actor PersonaAdminCommandActor, personaID, reason string) error {
	return s.stop(ctx, actor, personaID, reason, PersonaAdminRetire, agentpersonastore.StateRetired)
}

func (s *PersonaAdminReviewedTransitions) checked(ctx context.Context, actor PersonaAdminCommandActor, personaID string, action PersonaAdminCommandAction) (PersonaAdminLifecycleTenant, time.Time, uuid.UUID, error) {
	if s == nil || s.Store == nil || s.Authorizer == nil || s.Security == nil || s.TenantUUID == nil || s.Now == nil || s.NewEventID == nil || ctx == nil || !validPersonaAdminActor(actor.Principal) || !personaAdminActorBound(ctx, actor) || strings.TrimSpace(personaID) != personaID || personaID == "" {
		return nil, time.Time{}, uuid.Nil, ErrPersonaAdminLifecycleUnavailable
	}
	if err := s.Authorizer.AuthorizePersonaAdminCommand(ctx, actor, action, personaID); err != nil {
		return nil, time.Time{}, uuid.Nil, ErrPersonaAdminCommandUnavailable
	}
	now, tenantID := s.Now().UTC(), s.TenantUUID(actor.Tenant)
	if now.IsZero() || tenantID == uuid.Nil {
		return nil, time.Time{}, uuid.Nil, ErrPersonaAdminLifecycleUnavailable
	}
	store, err := s.Store.ForTenant(ctx, actor.Tenant)
	if err != nil || store == nil {
		return nil, time.Time{}, uuid.Nil, ErrPersonaAdminLifecycleUnavailable
	}
	return store, now, tenantID, nil
}

func (s *PersonaAdminReviewedTransitions) stop(ctx context.Context, actor PersonaAdminCommandActor, personaID, reason string, action PersonaAdminCommandAction, target agentpersonastore.LifecycleState) error {
	store, now, tenantID, err := s.checked(ctx, actor, personaID, action)
	if err != nil {
		return err
	}
	if reason == "" {
		reason = "Persona " + strings.ToLower(string(action)) + " by an authorized administrator"
	}
	if reason != strings.TrimSpace(reason) || strings.ContainsAny(reason, "\r\n") {
		return ErrPersonaAdminCommandUnavailable
	}
	versions, err := store.ListVersions(ctx, personaID)
	if err != nil || len(versions) == 0 {
		return ErrPersonaAdminLifecycleUnavailable
	}
	// Fence every version first. If an event write fails, fresh and in-flight
	// work remains stopped and a retry can finish the lifecycle projection.
	for _, version := range versions {
		if version.TenantID != actor.Tenant || version.PersonaID != personaID || version.Version <= 0 {
			return ErrPersonaAdminLifecycleUnavailable
		}
		if _, err := s.Security.RevokePersonaSecurityScope(ctx, tenantID, personaVersionSecurityScope(personaID, version.Version), reason, now); err != nil {
			return err
		}
	}
	for _, version := range versions {
		state, err := store.Lifecycle(ctx, personaID, version.Version)
		if err != nil {
			return err
		}
		if state == agentpersonastore.StateRetired || state == target || target == agentpersonastore.StateSuspended && state != agentpersonastore.StatePublished {
			continue
		}
		eventID := strings.TrimSpace(s.NewEventID())
		if eventID == "" {
			return ErrPersonaAdminLifecycleUnavailable
		}
		if err := store.AppendLifecycle(ctx, agentpersonastore.LifecycleEvent{TenantID: actor.Tenant, EventID: eventID, PersonaID: personaID, PersonaVersion: version.Version, From: state, To: target, Reason: reason, ActorID: actor.Subject, OccurredAt: now}); err != nil {
			return err
		}
	}
	return nil
}

func personaVersionSecurityScope(personaID string, version int64) agentstore.PersonaSecurityScope {
	return agentstore.PersonaSecurityScope{Kind: "VERSION", Key: personaID + ":" + strconv.FormatInt(version, 10)}
}

// RollbackPersona validates the immediately preceding published version before
// fencing any current version. Retired profiles and retired skill pins cannot
// be restored. Publication verifies review and evaluation again in its commit.
func (s *PersonaAdminReviewedTransitions) RollbackPersona(ctx context.Context, actor PersonaAdminCommandActor, personaID, reason string) error {
	store, now, tenantID, err := s.checked(ctx, actor, personaID, PersonaAdminRollback)
	if err != nil {
		return err
	}
	if s.Profiles == nil || s.Evidence == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	versions, err := store.ListVersions(ctx, personaID)
	if err != nil {
		return err
	}
	slices.SortFunc(versions, func(a, b agentpersonastore.PersonaVersion) int {
		if a.Version > b.Version {
			return -1
		}
		if a.Version < b.Version {
			return 1
		}
		return 0
	})
	var current, previous agentpersonastore.PersonaVersion
	var previousState agentpersonastore.LifecycleState
	for _, version := range versions {
		if version.TenantID != actor.Tenant || version.PersonaID != personaID {
			return ErrPersonaAdminLifecycleUnavailable
		}
		state, err := store.Lifecycle(ctx, personaID, version.Version)
		if err != nil {
			return err
		}
		if current.Version == 0 {
			if state == agentpersonastore.StatePublished {
				current = version
			}
			continue
		}
		if state == agentpersonastore.StatePublished || state == agentpersonastore.StateSuspended {
			previous, previousState = version, state
			break
		}
	}
	if current.Version == 0 || previous.Version == 0 {
		return ErrPersonaAdminLifecycleUnavailable
	}
	var profile agentpersona.PersonaProfile
	if err := json.Unmarshal(previous.Profile, &profile); err != nil {
		return ErrPersonaDraftInvalid
	}
	validated, err := buildPersonaProfile(ctx, s.Profiles, actor.Tenant, profile)
	if err != nil || validated.Digest != previous.ContentDigest {
		return ErrPersonaDraftInvalid
	}
	evidence, err := s.Evidence.ResolvePersonaPublicationEvidence(ctx, actor.Tenant, personaID, previous.Version)
	if err != nil || evidence.ReviewID == "" || evidence.EvaluationRunID == "" {
		return ErrPersonaAdminLifecycleUnavailable
	}
	if previousState == agentpersonastore.StateSuspended {
		// A durable scope may only be reopened with a new epoch. Until that
		// authority is configured, rollback of a suspended version fails closed.
		reactivator, ok := s.Security.(PersonaAdminSecurityReactivator)
		if !ok {
			return ErrPersonaAdminLifecycleUnavailable
		}
		eventID := strings.TrimSpace(s.NewEventID())
		if eventID == "" {
			return ErrPersonaAdminLifecycleUnavailable
		}
		if err := store.Publish(ctx, agentpersonastore.LifecycleEvent{TenantID: actor.Tenant, EventID: eventID, PersonaID: personaID, PersonaVersion: previous.Version, From: previousState, To: agentpersonastore.StatePublished, Reason: "Reviewed rollback target", ActorID: actor.Subject, OccurredAt: now}, evidence); err != nil {
			return err
		}
		if _, err := reactivator.ReactivatePersonaSecurityScope(ctx, tenantID, personaVersionSecurityScope(personaID, previous.Version), "Reviewed rollback target", now); err != nil {
			return err
		}
	}
	if reason == "" {
		reason = fmt.Sprintf("Rollback to reviewed persona version %d", previous.Version)
	}
	if _, err := s.Security.RevokePersonaSecurityScope(ctx, tenantID, personaVersionSecurityScope(personaID, current.Version), reason, now); err != nil {
		return err
	}
	return store.AppendLifecycle(ctx, agentpersonastore.LifecycleEvent{TenantID: actor.Tenant, EventID: s.NewEventID(), PersonaID: personaID, PersonaVersion: current.Version, From: agentpersonastore.StatePublished, To: agentpersonastore.StateSuspended, Reason: reason, ActorID: actor.Subject, OccurredAt: now})
}

// PersonaAdminSecurityReactivator invalidates all prior leases while allowing
// new leases after a fresh reviewed publication.
type PersonaAdminSecurityReactivator interface {
	ReactivatePersonaSecurityScope(context.Context, uuid.UUID, agentstore.PersonaSecurityScope, string, time.Time) (int64, error)
}

// ResumePersonaVersion restores only fresh reviewed published versions, with
// a new durable epoch that cannot revive a previously issued lease.
func (s *PersonaAdminReviewedTransitions) ResumePersonaVersion(ctx context.Context, actor PersonaAdminCommandActor, personaID string, version int64) error {
	store, now, tenantID, err := s.checked(ctx, actor, personaID, PersonaAdminPublish)
	if err != nil {
		return err
	}
	if s.Evidence == nil || version <= 0 {
		return ErrPersonaAdminLifecycleUnavailable
	}
	state, err := store.Lifecycle(ctx, personaID, version)
	if err != nil || state != agentpersonastore.StatePublished {
		return ErrPersonaAdminLifecycleUnavailable
	}
	if _, err := s.Evidence.ResolvePersonaPublicationEvidence(ctx, actor.Tenant, personaID, version); err != nil {
		return err
	}
	reactivator, ok := s.Security.(PersonaAdminSecurityReactivator)
	if !ok {
		return ErrPersonaAdminLifecycleUnavailable
	}
	_, err = reactivator.ReactivatePersonaSecurityScope(ctx, tenantID, personaVersionSecurityScope(personaID, version), "Fresh reviewed persona publication", now)
	return err
}

// PersonaAdminPlacementFacts is an authoritative current room policy snapshot.
// The source verifies manager membership and resolves administered ceilings;
// callers cannot supply this image through the command request.
type PersonaAdminPlacementFacts struct {
	Tenant              values.TenantId
	ConversationID      string
	Class               agentpersonastore.ConversationClass
	PlacementClass      string
	ManagerID           string
	Revision            uint64
	ExternalMembers     bool
	CrossCompanyMembers bool
	Policy              agentpersonastore.ChannelPolicy
}

type PersonaAdminPlacementSource interface {
	ResolvePersonaAdminPlacement(context.Context, PersonaAdminCommandActor, string) (PersonaAdminPlacementFacts, error)
}

// PersonaAdminPlacementFence serializes room membership and policy changes
// with the final installation commit in the independent agent database.
type PersonaAdminPlacementFence interface {
	WithPersonaAdminPlacementFence(context.Context, PersonaAdminPlacementFacts, func() error) error
}

// WithPersonaAdminPlacementFence uses the same authoritative chat revision as
// public delivery and policy administration. It holds the conversation lock
// while the separate persona-store installation transaction commits.
func (a *PersonaPublicAudienceAuthority) WithPersonaAdminPlacementFence(ctx context.Context, facts PersonaAdminPlacementFacts, work func() error) error {
	if a == nil || a.Store == nil || work == nil || facts.Revision == 0 {
		return ErrPersonaAdminLifecycleUnavailable
	}
	fence, ok := a.Store.(interface {
		WithConversationAuthorityFence(context.Context, string, string, uint64, func(dbport.Tx) error) error
	})
	if !ok {
		return ErrPersonaAdminLifecycleUnavailable
	}
	return fence.WithConversationAuthorityFence(ctx, facts.Tenant.String(), facts.ConversationID, facts.Revision, func(dbport.Tx) error { return work() })
}

type PersonaAdminInstallationProfiles interface {
	GetVersion(context.Context, values.TenantId, string, int64) (agentpersonastore.PersonaVersion, error)
}

// GovernedPersonaAdminInstallation revalidates exact persona pins and applies
// the current room ceiling to every placement, with external rooms disabled.
type GovernedPersonaAdminInstallation struct {
	Placement PersonaAdminPlacementSource
	Versions  PersonaAdminInstallationProfiles
	Profiles  PersonaProfileBuilder
}

func (a GovernedPersonaAdminInstallation) AuthorizePersonaInstallation(ctx context.Context, actor PersonaAdminCommandActor, request agentpersonastore.PersonaInstallation) (agentpersonastore.PersonaInstallation, error) {
	if ctx == nil || a.Placement == nil || a.Versions == nil || a.Profiles == nil || !personaAdminActorBound(ctx, actor) || request.TenantID != actor.Tenant || request.InstallerID != actor.Subject {
		return agentpersonastore.PersonaInstallation{}, ErrPersonaAdminLifecycleUnavailable
	}
	facts, err := a.Placement.ResolvePersonaAdminPlacement(ctx, actor, request.ConversationID)
	if err != nil || facts.Revision == 0 || facts.Tenant != actor.Tenant || facts.ConversationID != request.ConversationID || facts.ManagerID != actor.Subject || facts.ExternalMembers || facts.CrossCompanyMembers || facts.Class == agentpersonastore.ConversationExternal || facts.Class == agentpersonastore.ConversationCrossCompany || !slices.Contains(facts.Policy.AllowedChannelClasses, facts.Class) || facts.Policy.AllowExternalMembers || facts.Policy.AllowCrossCompanyMembers {
		return agentpersonastore.PersonaInstallation{}, ErrPersonaAdminLifecycleUnavailable
	}
	row, err := a.Versions.GetVersion(ctx, actor.Tenant, request.PersonaID, request.PersonaVersion)
	if err != nil || row.TenantID != actor.Tenant || row.PersonaID != request.PersonaID || row.Version != request.PersonaVersion {
		return agentpersonastore.PersonaInstallation{}, ErrPersonaAdminLifecycleUnavailable
	}
	var profile agentpersona.PersonaProfile
	if err := json.Unmarshal(row.Profile, &profile); err != nil {
		return agentpersonastore.PersonaInstallation{}, ErrPersonaDraftInvalid
	}
	validated, err := buildPersonaProfile(ctx, a.Profiles, actor.Tenant, profile)
	if err != nil || validated.Digest != row.ContentDigest || !personaAllowsChannelClass(validated.Profile, facts.Class) {
		return agentpersonastore.PersonaInstallation{}, ErrPersonaDraftInvalid
	}
	ceiling, ok := catalogPlacementTier(facts.Policy.MaxTier)
	if !ok || ceiling > agentskills.TierT3 {
		return agentpersonastore.PersonaInstallation{}, ErrPersonaAdminLifecycleUnavailable
	}
	data := append(slices.Clone(validated.Profile.DataClassesRead), validated.Profile.DataClassesWritten...)
	for _, class := range data {
		if !slices.Contains(facts.Policy.AllowedDataClasses, class) {
			return agentpersonastore.PersonaInstallation{}, ErrPersonaAdminLifecycleUnavailable
		}
	}
	kind := agentpersona.ConversationChannel
	if facts.Class == agentpersonastore.ConversationOneToOne {
		kind = agentpersona.ConversationDirect
	}
	if facts.Class == agentpersonastore.ConversationGroupDM {
		kind = agentpersona.ConversationGroup
	}
	if !slices.Contains(validated.Profile.ConversationKinds, kind) {
		return agentpersonastore.PersonaInstallation{}, ErrPersonaDraftInvalid
	}
	if !personaAllowsPlacement(validated.Profile, facts.Class, facts.PlacementClass) {
		return agentpersonastore.PersonaInstallation{}, ErrPersonaDraftInvalid
	}
	request.ConversationClass, request.ChannelPolicy = facts.Class, facts.Policy
	request.ChannelPolicy.AlwaysPrivate = facts.Policy.AlwaysPrivate || validated.Profile.AlwaysPrivate
	if profileTier := validated.Profile.TierForConversation(kind); profileTier < ceiling {
		request.ChannelPolicy.MaxTier = profileTier.String()
	}
	request.ChannelPolicy.AllowedDataClasses = slices.Clone(facts.Policy.AllowedDataClasses)
	request.ChannelPolicy.AllowedChannelClasses = slices.Clone(facts.Policy.AllowedChannelClasses)
	return request, nil
}

func personaAllowsPlacement(profile agentpersona.PersonaProfile, class agentpersonastore.ConversationClass, placement string) bool {
	if len(profile.AllowedPlacementClasses) == 0 {
		return true
	}
	if class == agentpersonastore.ConversationExternal || class == agentpersonastore.ConversationCrossCompany {
		return false
	}
	if slices.Contains(profile.AllowedPlacementClasses, "ANY_INTERNAL") {
		return true
	}
	if class == agentpersonastore.ConversationOneToOne && slices.Contains(profile.AllowedPlacementClasses, "ONE_TO_ONE_DM") {
		return true
	}
	if (class == agentpersonastore.ConversationPrivate || class == agentpersonastore.ConversationGroupDM) && slices.Contains(profile.AllowedPlacementClasses, "PRIVATE") {
		return true
	}
	return placement != "" && slices.Contains(profile.AllowedPlacementClasses, placement)
}

// AuthorizeAndInstallPersona rejects a changed room before invoking the
// durable installation writer. A source without a conversation fence cannot
// enable installation commands.
func (a GovernedPersonaAdminInstallation) AuthorizeAndInstallPersona(ctx context.Context, actor PersonaAdminCommandActor, request agentpersonastore.PersonaInstallation, write func(agentpersonastore.PersonaInstallation) error) error {
	fence, ok := a.Placement.(PersonaAdminPlacementFence)
	if !ok || write == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	facts, err := a.Placement.ResolvePersonaAdminPlacement(ctx, actor, request.ConversationID)
	if err != nil {
		return err
	}
	installation, err := a.AuthorizePersonaInstallation(ctx, actor, request)
	if err != nil {
		return err
	}
	return fence.WithPersonaAdminPlacementFence(ctx, facts, func() error { return write(installation) })
}

type personaAdminStoredEvidence struct{ store *agentpersonastore.Store }

func (s personaAdminStoredEvidence) ResolvePersonaPublicationEvidence(ctx context.Context, tenant values.TenantId, personaID string, version int64) (agentpersonastore.PublicationEvidence, error) {
	if s.store == nil {
		return agentpersonastore.PublicationEvidence{}, ErrPersonaAdminLifecycleUnavailable
	}
	scoped, err := s.store.ForTenant(ctx, tenant)
	if err != nil {
		return agentpersonastore.PublicationEvidence{}, err
	}
	return scoped.ResolvePublicationEvidence(ctx, personaID, version)
}

type personaAdminInstallationVersions struct{ store *agentpersonastore.Store }

func (s personaAdminInstallationVersions) GetVersion(ctx context.Context, tenant values.TenantId, personaID string, version int64) (agentpersonastore.PersonaVersion, error) {
	if s.store == nil {
		return agentpersonastore.PersonaVersion{}, ErrPersonaAdminLifecycleUnavailable
	}
	scoped, err := s.store.ForTenant(ctx, tenant)
	if err != nil {
		return agentpersonastore.PersonaVersion{}, err
	}
	return scoped.GetVersion(ctx, personaID, version)
}
