package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// localAgentDemoStarterPlan is what a starter's first version (and its later
// upgrades) is built from beyond the starter itself: which persona it is, the
// exact instruction text its manifest pins, the avatar and the tier ceiling the
// demo gives it (AGENTUX-049).
type localAgentDemoStarterPlan struct {
	PersonaID, Avatar, Instructions string
	Tier                            agentskills.SideEffectTier
	// UpgradeEvent and UpgradeReason label the lifecycle event that records a
	// new version of an already published agent.
	UpgradeEvent, UpgradeReason string
}

// ensureStarterManifest saves the starter's manifest and the instruction text
// it pins, once. A manifest identical to the current one is returned as it is;
// a changed one is saved as the next version of the same manifest, and the old
// version stays readable by the persona versions that pin it.
func ensureStarterManifest(ctx context.Context, agents *agentstore.Store, tenant uuid.UUID, manifest agentmanifest.Manifest, instructions string) (agentmanifest.Manifest, error) {
	var revision uint64
	if current, currentRevision, err := agents.CurrentManifest(ctx, tenant, manifest.ID); err == nil {
		manifest.Version = current.Version
		if samePersonaStarterManifest(current, manifest) {
			return current, nil
		}
		manifest.Version = current.Version + 1
		revision = currentRevision
	} else if !errors.Is(err, agentstore.ErrNotFound) {
		return agentmanifest.Manifest{}, err
	}
	digest, err := agents.SaveInstructionContent(ctx, tenant, instructions)
	if err != nil || digest != manifest.InstructionsDigest {
		return agentmanifest.Manifest{}, ErrLocalDevPersonaChatBootstrap
	}
	if _, err := agents.SaveManifest(ctx, tenant, manifest, revision); err != nil && !errors.Is(err, agentstore.ErrConflict) {
		return agentmanifest.Manifest{}, err
	}
	current, _, err := agents.CurrentManifest(ctx, tenant, manifest.ID)
	if err != nil || !samePersonaStarterManifest(current, manifest) {
		return agentmanifest.Manifest{}, ErrLocalDevPersonaChatBootstrap
	}
	return current, nil
}

// ensureStarterVersion makes sure the starter's persona has a version built on
// the given manifest: the first draft when the persona does not exist, a new
// draft version when the manifest changed under a published version, and the
// latest version unchanged otherwise. Owner, steward, audience, data classes
// and evaluation limits come from the policy agent's profile, which the
// administrator already reviewed; a later version keeps its predecessor's.
func ensureStarterVersion(ctx context.Context, scoped *agentpersonastore.TenantStore, starter agenttemplate.PersonaStarter, manifest agentmanifest.Manifest, plan localAgentDemoStarterPlan, policy agentpersona.PersonaProfile, now time.Time) (agentpersonastore.PersonaVersion, agentpersonastore.LifecycleState, error) {
	if row, state, err := latestPersonaVersion(ctx, scoped, plan.PersonaID, ""); err == nil {
		var current agentpersona.PersonaProfile
		if json.Unmarshal(row.Profile, &current) != nil || current.PersonaID != plan.PersonaID || current.Handle != starter.Handle || current.Manifest.ID != manifest.ID || current.EvalSuiteRef != starter.EvaluationSuite {
			return agentpersonastore.PersonaVersion{}, "", ErrPersonaDraftInvalid
		}
		digest, digestErr := manifest.Digest()
		if digestErr != nil {
			return agentpersonastore.PersonaVersion{}, "", digestErr
		}
		if current.Manifest.Digest == digest && uint64(current.Manifest.Version) == manifest.Version {
			return row, state, nil
		}
		if state != agentpersonastore.StatePublished {
			return agentpersonastore.PersonaVersion{}, "", fmt.Errorf("%s version %d must finish review before another upgrade", starter.DisplayName, row.Version)
		}
		upgraded := personaStarterProfile(starter, PersonaStarterDraftRequest{PersonaID: current.PersonaID, AvatarRef: current.AvatarRef, OrganizationScopes: current.Audience.OrganizationScopes, BusinessOwnerID: current.Owner, TechnicalStewardID: current.Steward}, manifest, plan.Instructions)
		upgraded.Version = current.Version + 1
		upgraded.Audience = current.Audience
		upgraded.DataClassesRead = current.DataClassesRead
		upgraded.EvalLimits = current.EvalLimits
		upgraded.AlwaysPrivate = current.AlwaysPrivate
		upgraded.TierCeiling = current.TierCeiling
		upgraded.ConversationKinds = current.ConversationKinds
		upgraded.ChannelClasses = current.ChannelClasses
		upgraded.AllowedPlacementClasses = current.AllowedPlacementClasses
		upgraded.ConversationTierCeilings = current.ConversationTierCeilings
		sealed, err := agentpersona.Seal(upgraded)
		if err != nil {
			return agentpersonastore.PersonaVersion{}, "", err
		}
		raw, _ := json.Marshal(sealed.Profile)
		next := row
		next.Version = int64(upgraded.Version)
		next.AgentVersion = fmt.Sprintf("%s@%d", manifest.ID, manifest.Version)
		next.Profile = raw
		next.ContentDigest = sealed.Digest
		next.CreatedAt = now
		if err := scoped.PutVersion(ctx, next); err != nil {
			return agentpersonastore.PersonaVersion{}, "", err
		}
		if err := scoped.AppendLifecycle(ctx, agentpersonastore.LifecycleEvent{TenantID: next.TenantID, EventID: plan.UpgradeEvent + "-" + uuid.NewString(), PersonaID: next.PersonaID, PersonaVersion: next.Version, To: agentpersonastore.StateDraft, Reason: plan.UpgradeReason, ActorID: localAgentDemoAdmin, OccurredAt: now}); err != nil {
			return agentpersonastore.PersonaVersion{}, "", err
		}
		return next, agentpersonastore.StateDraft, nil
	} else if !errors.Is(err, agentpersonastore.ErrNotFound) {
		return agentpersonastore.PersonaVersion{}, "", err
	}
	profile := personaStarterProfile(starter, PersonaStarterDraftRequest{PersonaID: plan.PersonaID, AvatarRef: plan.Avatar, OrganizationScopes: append([]string(nil), policy.Audience.OrganizationScopes...), BusinessOwnerID: policy.Owner, TechnicalStewardID: policy.Steward}, manifest, plan.Instructions)
	profile.Audience = agentpersona.Audience{Roles: append([]string(nil), policy.Audience.Roles...), Populations: append([]string(nil), policy.Audience.Populations...), OrganizationScopes: append([]string(nil), policy.Audience.OrganizationScopes...)}
	profile.DataClassesRead = append([]string(nil), policy.DataClassesRead...)
	profile.EvalLimits = policy.EvalLimits
	profile.TierCeiling = plan.Tier
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		return agentpersonastore.PersonaVersion{}, "", err
	}
	raw, _ := json.Marshal(sealed.Profile)
	row := agentpersonastore.PersonaVersion{TenantID: values.TenantId(localAgentDemoTenant), PersonaID: profile.PersonaID, Version: int64(profile.Version), AgentVersion: fmt.Sprintf("%s@%d", profile.Manifest.ID, profile.Manifest.Version), Handle: profile.Handle, DisplayName: profile.DisplayName, Profile: raw, ContentDigest: sealed.Digest, CreatedAt: now}
	owner := agentpersonastore.PersonaOwner{TenantID: row.TenantID, PersonaID: row.PersonaID, Role: agentpersonastore.BusinessOwner, PrincipalID: profile.Owner}
	steward := agentpersonastore.PersonaOwner{TenantID: row.TenantID, PersonaID: row.PersonaID, Role: agentpersonastore.TechnicalSteward, PrincipalID: profile.Steward}
	if err := scoped.CreateDraft(ctx, row, owner, steward, localAgentDemoAdmin, now); err != nil {
		return agentpersonastore.PersonaVersion{}, "", err
	}
	return row, agentpersonastore.StateDraft, nil
}

// renewPublishedPersonaVersion creates the draft of the next version of a
// published agent, identical to the published one but for its number. It is how
// the preparation carries an agent across the lapse of its runtime evidence: the
// runtime evidence (a model route, a review, a recorded evaluation) is issued
// per version and expires, and a published version is never reissued, so the
// agent continues as a new version that passes through the same steps. The
// published version is left as it was; nothing is deleted or rewritten.
func renewPublishedPersonaVersion(ctx context.Context, scoped *agentpersonastore.TenantStore, row agentpersonastore.PersonaVersion, profile agentpersona.PersonaProfile, now time.Time) error {
	if latest, _, err := latestPersonaVersion(ctx, scoped, row.PersonaID, ""); err != nil || latest.Version != row.Version {
		return ErrPersonaDraftInvalid
	}
	renewed := profile
	renewed.Version = profile.Version + 1
	sealed, err := agentpersona.Seal(renewed)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(sealed.Profile)
	next := row
	next.Version = int64(renewed.Version)
	next.Profile = raw
	next.ContentDigest = sealed.Digest
	next.CreatedAt = now
	if err := scoped.PutVersion(ctx, next); err != nil {
		return err
	}
	return scoped.AppendLifecycle(ctx, agentpersonastore.LifecycleEvent{TenantID: next.TenantID, EventID: "renewed-" + uuid.NewString(), PersonaID: next.PersonaID, PersonaVersion: next.Version, To: agentpersonastore.StateDraft, Reason: "Renewed with current review and evaluation", ActorID: localAgentDemoAdmin, OccurredAt: now})
}
