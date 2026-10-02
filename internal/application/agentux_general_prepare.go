package application

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/roleaccessstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type localAgentDemoPreparedAgent struct {
	version                    int64
	state                      agentpersonastore.LifecycleState
	evaluationRecorded         bool
	published                  bool
	modelRouteCreated          bool
	providerDeploymentCreated  bool
	chatConversationRepaired   bool
	installationsCreated       int
	installationsRefreshed     int
	duplicateInstallationsGone int
	skillGrantsCreated         int
}

type localAgentDemoSeed struct {
	personaID, starterID string
}

type localAgentDemoPreparation struct {
	config             LocalAgentDemoConfig
	core               dbport.Beginner
	agents             *agentstore.Store
	personas           *agentpersonastore.Store
	evaluations        *agentpersonastore.EvaluationSealAuthority
	mapper             func(values.TenantId) uuid.UUID
	key                ed25519.PrivateKey
	material           LocalPersonaModelSigningMaterial
	admin              *trust.Principal
	chat, repairStore  *chatstore.Store
	chatService        chatcore.ConversationService
	roleAccess         *roleaccessstore.Store
	publicConversation string
	deploymentPath     string
	now                time.Time
}

func (p localAgentDemoPreparation) prepare(adminCtx context.Context, seed localAgentDemoSeed, policyProfile agentpersona.PersonaProfile) (localAgentDemoPreparedAgent, string, error) {
	ctx := adminCtx
	config, core, agents, personas, evaluations := p.config, p.core, p.agents, p.personas, p.evaluations
	mapper, key, material, now := p.mapper, p.key, p.material, p.now
	chat, repairStore, chatService, roleAccess := p.chat, p.repairStore, p.chatService, p.roleAccess
	publicConversation, adminPrincipal := p.publicConversation, p.admin
	var out localAgentDemoPreparedAgent
	starter, ok := agenttemplate.PersonaStarterFor(seed.starterID, 1)
	if !ok {
		return out, "", ErrLocalDevPersonaChatBootstrap
	}
	if seed.personaID == localAgentDemoAssistantPersonaID && LocalPersonaOpenAIPolicySelection().EvaluationSuites[localAgentDemoAssistantSuiteID].Version >= 3 {
		starter = AssistantWorkspaceStarter()
	}
	scoped, err := personas.Scoped(values.TenantId(config.Tenant))
	if err != nil {
		return out, "", err
	}
	row, state, err := latestPersonaVersion(ctx, scoped, seed.personaID, "")
	if (err == nil || errors.Is(err, agentpersonastore.ErrNotFound)) && seed.personaID == localAgentDemoAssistantPersonaID {
		manifest, manifestErr := ensureLocalAgentDemoAssistantManifest(ctx, agents, mapper(values.TenantId(config.Tenant)), starter)
		if manifestErr != nil {
			return out, "", manifestErr
		}
		row, state, err = ensureLocalAgentDemoAssistantVersion(ctx, scoped, starter, manifest, policyProfile, now)
	}
	if err != nil {
		return out, "", err
	}
	out.version, out.state = row.Version, state
	var profile agentpersona.PersonaProfile
	if json.Unmarshal(row.Profile, &profile) != nil {
		return out, "", ErrPersonaDraftInvalid
	}
	manifest, err := (AgentManifestStoreAdapter{Store: agents, TenantID: mapper(row.TenantID)}).ResolveAgentManifestContext(ctx, profile.Manifest)
	if err != nil {
		return out, "", err
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil || sealed.Digest != row.ContentDigest {
		return out, "", ErrPersonaDraftInvalid
	}
	if seed.personaID == localAgentDemoAssistantPersonaID {
		// CHATLIVE-001: a version that pins a skill its predecessor did not is
		// listed in no conversation until that skill is granted.
		if out.skillGrantsCreated, err = ensureLocalAgentDemoSkillGrants(ctx, core, mapper, row.TenantID, profile, now); err != nil {
			return out, "", err
		}
	}
	if state == agentpersonastore.StateDraft {
		if err := scoped.AppendLifecycle(ctx, agentpersonastore.LifecycleEvent{TenantID: row.TenantID, EventID: "local-assistant-review-request-" + uuid.NewString(), PersonaID: row.PersonaID, PersonaVersion: row.Version, From: state, To: agentpersonastore.StateInReview, Reason: "Independent persona review requested", ActorID: localAgentDemoAdmin, OccurredAt: now}); err != nil {
			return out, "", err
		}
		state, out.state = agentpersonastore.StateInReview, agentpersonastore.StateInReview
	}
	if state != agentpersonastore.StateInReview && state != agentpersonastore.StatePublished {
		return out, "", fmt.Errorf("%s must be IN_REVIEW or PUBLISHED, found %s", row.DisplayName, state)
	}
	if state == agentpersonastore.StateInReview {
		if _, reviewErr := scoped.ResolveCurrentReview(ctx, row.PersonaID, row.Version); reviewErr != nil {
			if err := issueLocalAgentDemoAssistantReview(ctx, config, agents, mapper, scoped, policyProfile, row, now); err != nil {
				return out, "", err
			}
		}
	}
	candidate, _, err := NewLocalPersonaOpenAICandidate(manifest)
	if err != nil {
		return out, "", err
	}
	suite, err := localAgentDemoEvaluationSuiteFor(profile)
	if err != nil {
		return out, "", err
	}
	modelDigest := "sha256:" + candidate.ProfileDigest
	target := agenteval.PersonaEvaluationTarget{TenantID: config.Tenant, SyntheticTenantID: localAgentDemoSynthetic, InvokerID: localAgentDemoAdmin, PersonaID: row.PersonaID, PersonaVersion: row.Version, ProfileDigest: row.ContentDigest, ModelDigest: modelDigest}
	report, measuredSuite, err := localAgentDemoEvaluationReportForSuite(ctx, target, suite, now.Add(-time.Second))
	if err != nil {
		return out, "", err
	}
	qualified, err := QualifyPersonaCandidateModelProfile(report, candidate, profile.Manifest.Digest, measuredSuite)
	if err != nil {
		return out, "", err
	}
	// A published version whose runtime is already in place is left alone. Its
	// review and evaluation evidence expire (a review lasts as long as its
	// reviewer's grant, a day locally); that expiry must not fail a rerun, and
	// publication evidence is only needed to publish or to provision again.
	settled := false
	if state == agentpersonastore.StatePublished {
		settled, err = p.publishedRuntimeSettled(ctx, adminCtx, row, profile, qualified, &out)
		if err != nil {
			return out, "", err
		}
	}
	runID := localAgentDemoEvaluationRunID(row)
	evidence, evidenceErr := scoped.ResolvePublicationEvidence(ctx, row.PersonaID, row.Version)
	if settled {
		// Nothing to record for a settled published version.
	} else if evidenceErr != nil || evidence.EvaluationRunID == "" {
		issuer := &PersonaEvaluationIssuer{Versions: personaAdminInstallationVersions{store: personas}, Recorder: &PersonaEvaluationEvidenceService{store: personas}, TenantID: row.TenantID, SuiteID: suite.ID, SuiteDigest: agenteval.PersonaSuiteDigest(suite), KeyID: localAgentDemoEvaluationKeyID, PrivateKey: key, ModelDigest: modelDigest, Now: func() time.Time { return now }, FreshFor: localAgentDemoFreshFor, NewRunID: func() string { return runID }}
		if _, issueErr := issuer.Issue(ctx, report); issueErr != nil && !errors.Is(issueErr, agentpersonastore.ErrConflict) {
			return out, "", fmt.Errorf("record Assistant evaluation: %w", issueErr)
		} else {
			out.evaluationRecorded = issueErr == nil
		}
	} else {
		runID = evidence.EvaluationRunID
	}
	if err := ensureLocalAgentDemoChatIdentity(ctx, scoped, profile.Handle, row.PersonaID, now); err != nil {
		return out, "", err
	}
	if err := ensureLocalAgentDemoRoleVisibility(ctx, roleAccess, row.TenantID, profile.Audience); err != nil {
		return out, "", err
	}
	directConversation, repaired, err := ensureLocalAgentDemoDirectConversation(adminCtx, repairStore, chatService, config.Tenant, localAgentDemoAdmin, profile.Handle)
	if err != nil {
		return out, "", err
	}
	out.chatConversationRepaired = repaired
	if err := provisionLocalAgentDemoDirectPolicy(ctx, chat, profile.Handle, directConversation); err != nil {
		return out, "", err
	}
	builder := agentDemoProfileBuilder{manifest: manifest}
	authorizer := localAgentDemoAuthorizer{tenant: row.TenantID, subject: localAgentDemoAdmin, persona: row.PersonaID}
	install := GovernedPersonaAdminInstallation{Placement: NewPersonaPublicAudienceAuthority(chat), Versions: personaAdminInstallationVersions{store: personas}, Profiles: builder}
	runtime := localAgentDemoRecordingProvisioner{changes: &out, runtime: &LocalPersonaRuntimeProvisioner{Config: config, Core: core, Agents: agents, Personas: personas, Evaluations: evaluations, Signing: material, DeploymentPath: p.deploymentPath, Now: func() time.Time { return now }}}
	executor := NewPersonaAdminLifecycleExecutor(personaAdminLifecycleStoreAdapter{store: personas}, authorizer, &PersonaAdminDraftService{Profiles: builder}, nil, nil, personaAdminStoredEvidence{store: personas}, install, nil, func() time.Time { return now }, uuid.NewString, runtime)
	actor := PersonaAdminCommandActor{Principal: adminPrincipal, Tenant: row.TenantID, Subject: localAgentDemoAdmin}
	if state == agentpersonastore.StateInReview {
		if err := executor.ExecutePersonaAdminCommand(adminCtx, actor, PersonaAdminCommand{Action: PersonaAdminPublish, PersonaID: row.PersonaID}); err != nil {
			return out, "", fmt.Errorf("publish "+row.DisplayName+": %w", err)
		}
		out.published, out.state = true, agentpersonastore.StatePublished
	} else if !settled {
		evidence, resolveErr := scoped.ResolvePublicationEvidence(ctx, row.PersonaID, row.Version)
		if resolveErr != nil {
			return out, "", fmt.Errorf("%s version %d is published but its runtime is incomplete and its review or evaluation evidence is no longer current: %w", row.DisplayName, row.Version, resolveErr)
		}
		if err := runtime.ProvisionPersonaRuntime(adminCtx, actor, row, profile, evidence); err != nil {
			return out, "", err
		}
	}
	for _, conversation := range []string{publicConversation, directConversation} {
		retired, retireErr := scoped.RetireSuspendedDuplicateInstallations(ctx, row.PersonaID, conversation)
		if retireErr != nil {
			return out, "", retireErr
		}
		out.duplicateInstallationsGone += retired
		installed, refresh, stateErr := localAgentDemoInstallationState(ctx, scoped, chat, config.Tenant, conversation, row.PersonaID, row.Version)
		if stateErr != nil {
			return out, "", stateErr
		}
		if installed && !refresh {
			continue
		}
		action := PersonaAdminInstall
		if refresh {
			action = PersonaAdminReinstall
		}
		if err := executor.ExecutePersonaAdminCommand(adminCtx, actor, PersonaAdminCommand{Action: action, PersonaID: row.PersonaID, Installation: agentpersonastore.PersonaInstallation{PersonaID: row.PersonaID, ConversationID: conversation}}); err != nil {
			return out, "", fmt.Errorf("install "+row.DisplayName+" in %s: %w", conversation, err)
		}
		if refresh {
			out.installationsRefreshed++
		} else {
			out.installationsCreated++
		}
	}
	return out, directConversation, nil
}

// Assistant's private conversation uses the same reviewed local DM ceiling.
// Policy Helper's existing bootstrap contract and identifiers stay unchanged.
func provisionLocalAgentDemoDirectPolicy(ctx context.Context, chat *chatstore.Store, handle, conversation string) error {
	if handle == localAgentDemoAgentID {
		_, err := ProvisionLocalDevPersonaDirectPolicy(ctx, chat, ServeProfileLocalDev, localAgentDemoTenant, conversation, localAgentDemoAdmin)
		return err
	}
	expected, err := chatcore.DirectPairConversationID(localAgentDemoTenant, []chatcore.MemberRef{{TenantID: localAgentDemoTenant, SubjectID: localAgentDemoAdmin}, {TenantID: localAgentDemoTenant, SubjectID: localAgentDemoAssistantAgentID}})
	if chat == nil || handle != localAgentDemoAssistantAgentID || err != nil || conversation != expected {
		return ErrLocalDevPersonaChatBootstrap
	}
	ceiling, _, _ := localDevPersonaRoomCeiling("dm-0")
	ceiling.ConversationSearchAllowed = true
	if _, err := chat.PutAudiencePolicy(ctx, localAgentDemoTenant, conversation, 0, chatstore.AudiencePolicy{RoleMode: 1, Classification: "INTERNAL"}); err != nil && !errors.Is(err, chatstore.ErrAudiencePolicyConflict) {
		return err
	}
	current, err := chat.CapturePersonaChannelPolicy(ctx, localAgentDemoTenant, conversation, "")
	if err == nil {
		if !reflect.DeepEqual(current.Policy, ceiling) {
			return ErrLocalDevPersonaChatBootstrap
		}
		return nil
	}
	if !errors.Is(err, dbport.ErrNoRows) && !errors.Is(err, chatstore.ErrAudienceEligibilityUnavailable) {
		return err
	}
	_, err = chat.PutPersonaChannelPolicy(ctx, localAgentDemoTenant, conversation, 0, ceiling)
	return err
}

func ensureLocalAgentDemoAssistantManifest(ctx context.Context, agents *agentstore.Store, tenant uuid.UUID, starter agenttemplate.PersonaStarter) (agentmanifest.Manifest, error) {
	selection := LocalPersonaOpenAIPolicySelection()
	instructions := personaStarterInstructions(starter)
	manifest := localDevPersonaStarterManifest(starter, instructions, localDevPersonaStarterReferences{modelPolicy: selection.ModelPolicy, outputSchema: selection.OutputSchema, evaluation: selection.EvaluationSuites[starter.EvaluationSuite]})
	if starter.Version >= 2 {
		manifest.EvaluationRefs = []agentmanifest.Reference{AssistantWorkspaceEvaluationRecord().Reference}
	} else if starter.ID == localAgentDemoAssistantStarterID {
		// The first Assistant image keeps referencing the v2 suite it was
		// evaluated against; the selection now names v3.
		manifest.EvaluationRefs = []agentmanifest.Reference{LocalPersonaOpenAIPolicyRecords()[3].Reference}
	}
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

func ensureLocalAgentDemoAssistantVersion(ctx context.Context, scoped *agentpersonastore.TenantStore, starter agenttemplate.PersonaStarter, manifest agentmanifest.Manifest, policy agentpersona.PersonaProfile, now time.Time) (agentpersonastore.PersonaVersion, agentpersonastore.LifecycleState, error) {
	if row, state, err := latestPersonaVersion(ctx, scoped, localAgentDemoAssistantPersonaID, ""); err == nil {
		var current agentpersona.PersonaProfile
		if json.Unmarshal(row.Profile, &current) != nil || current.PersonaID != localAgentDemoAssistantPersonaID || current.Handle != localAgentDemoAssistantAgentID || current.Manifest.ID != manifest.ID || current.EvalSuiteRef != starter.EvaluationSuite {
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
			return agentpersonastore.PersonaVersion{}, "", fmt.Errorf("Assistant version %d must finish review before another upgrade", row.Version)
		}
		upgraded := personaStarterProfile(starter, PersonaStarterDraftRequest{PersonaID: current.PersonaID, AvatarRef: current.AvatarRef, OrganizationScopes: current.Audience.OrganizationScopes, BusinessOwnerID: current.Owner, TechnicalStewardID: current.Steward}, manifest, personaStarterInstructions(starter))
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
		if err := scoped.AppendLifecycle(ctx, agentpersonastore.LifecycleEvent{TenantID: next.TenantID, EventID: "assistant-workspace-upgrade-" + uuid.NewString(), PersonaID: next.PersonaID, PersonaVersion: next.Version, To: agentpersonastore.StateDraft, Reason: "Assistant workspace search upgrade", ActorID: localAgentDemoAdmin, OccurredAt: now}); err != nil {
			return agentpersonastore.PersonaVersion{}, "", err
		}
		return next, agentpersonastore.StateDraft, nil
	} else if !errors.Is(err, agentpersonastore.ErrNotFound) {
		return agentpersonastore.PersonaVersion{}, "", err
	}
	profile := personaStarterProfile(starter, PersonaStarterDraftRequest{PersonaID: localAgentDemoAssistantPersonaID, AvatarRef: "avatar:assistant", OrganizationScopes: append([]string(nil), policy.Audience.OrganizationScopes...), BusinessOwnerID: policy.Owner, TechnicalStewardID: policy.Steward}, manifest, personaStarterInstructions(starter))
	profile.Audience = agentpersona.Audience{Roles: append([]string(nil), policy.Audience.Roles...), Populations: append([]string(nil), policy.Audience.Populations...), OrganizationScopes: append([]string(nil), policy.Audience.OrganizationScopes...)}
	profile.DataClassesRead = append([]string(nil), policy.DataClassesRead...)
	profile.EvalLimits = policy.EvalLimits
	profile.TierCeiling = agentskills.TierT0
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

func issueLocalAgentDemoAssistantReview(ctx context.Context, config LocalAgentDemoConfig, agents *agentstore.Store, mapper func(values.TenantId) uuid.UUID, scoped *agentpersonastore.TenantStore, policyProfile agentpersona.PersonaProfile, row agentpersonastore.PersonaVersion, now time.Time) error {
	// The independent reviewer is the demo pack's fixed reviewer, the same person
	// who reviewed Policy Helper. It is derived from the pack, not read back from
	// Policy Helper's review: that review expires with the reviewer's grant, and a
	// rerun a day later must still be able to review a new version.
	pack, ok := demoworkforce.PackFor(config.Tenant)
	if !ok {
		return ErrPersonaReviewUnavailable
	}
	reviewerID, err := localDevPolicyHelperReviewer(pack, mapper(row.TenantID))
	if err != nil || strings.TrimSpace(reviewerID) == "" || reviewerID == policyProfile.Owner || reviewerID == policyProfile.Steward {
		return ErrPersonaReviewUnavailable
	}
	reviewStore, err := agentstore.NewPersonaReviewAuthorityStore(ctx, agentstore.PersonaReviewAuthorityConfig{DSN: config.ReviewAuthorityDatabaseURL, AgentDSN: config.AgentDatabaseURL, CoreDSN: config.DatabaseURL, ChatDSN: config.ChatDatabaseURL, DocumentDSN: config.DocumentDatabaseURL})
	if err != nil {
		return err
	}
	defer reviewStore.Close()
	if err := ensureLocalAgentDemoReviewerGrant(ctx, reviewStore, mapper(row.TenantID), reviewerID, policyProfile.Owner, now); err != nil {
		return err
	}
	sources, err := NewPersonaAdminReviewSources(agents, reviewStore, mapper)
	if err != nil {
		return err
	}
	issuer, err := NewPersonaReviewIssuanceService(sources.Authority, sources.Writer)
	if err != nil {
		return err
	}
	reviewer, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: row.TenantID, Subject: reviewerID, SubjectKind: trust.SubjectKindHuman, Roles: []string{localDevPersonaReviewerRole}, OrganizationScopeID: policyProfile.Audience.OrganizationScopes[0], AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "local-agent-demo-assistant-review", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:local-agent-demo-assistant-review"})
	if err != nil {
		return err
	}
	_, err = issuer.Issue(trust.WithPrincipal(ctx, reviewer), row.PersonaID, row.Version, "APPROVE")
	return err
}
