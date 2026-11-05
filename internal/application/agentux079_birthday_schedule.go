package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agenttriggerstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// The Birthday Buddy schedule (AGENTUX-079): one daily announcement that
// wishes the channel's birthday people well. Preparation creates it paused
// and resumes it once Birthday Buddy's model route exists, so a schedule can
// never fire before the agent that serves it.
const (
	// AgentUX079BirthdayScheduleID is the stable identity of the Birthday
	// Buddy schedule within the demo tenant.
	AgentUX079BirthdayScheduleID  = "birthday-buddy-daily"
	agentUX079BirthdayInstruction = "Wish everyone whose birthday is today a happy birthday in the channel."
	agentUX079BirthdayCadence     = "DAILY"
	agentUX079BirthdayZone        = "UTC"
	agentUX079BirthdayHour        = 9
)

// AgentUX079BirthdayScheduleReceipt is what one preparation run did about the
// Birthday Buddy schedule. Checked tells a run that found the schedule
// already converged from a run that never reached the step.
type AgentUX079BirthdayScheduleReceipt struct {
	Checked, Created, Resumed bool
}

// AgentUX079BirthdayAnnouncements is the announcement table the schedule is
// ensured through: read, first write, and revision-checked save. It is
// satisfied by the announcement store.
type AgentUX079BirthdayAnnouncements interface {
	Get(context.Context, uuid.UUID, string) (agentstore.Announcement, error)
	Create(context.Context, agentstore.Announcement) error
	Save(context.Context, agentstore.Announcement, uint64) error
}

// AgentUX079BirthdayScheduleDeps are the ports the Birthday Buddy schedule is
// ensured through. Announcements is the announcement table, Schedules the
// native schedule substrate with preparation bindings, and RouteExists is
// whether Birthday Buddy's model route exists right now.
var _ AgentUX079BirthdayAnnouncements = (*agentstore.AnnouncementStore)(nil)

type AgentUX079BirthdayScheduleDeps struct {
	Announcements  AgentUX079BirthdayAnnouncements
	Schedules      AgentAnnouncementScheduleOwner
	TenantUUID     func(values.TenantId) uuid.UUID
	Tenant         string
	Admin          string
	InstallationID string
	ConversationID string
	RouteExists    bool
	Now            func() time.Time
}

// ensureAgentUX079BirthdaySchedule creates the Birthday Buddy schedule paused
// when it is missing and resumes it once the model route exists. Every step
// is idempotent: a rerun, a racing preparation, or a schedule left behind by
// an interrupted run all converge instead of failing. A schedule that is
// already active, that was deleted, or that another owner filed under the
// same identity is left alone: active running is AGENTUX-082's to expire, a
// deletion is deliberate, and another owner's schedule is not ours to touch.
func ensureAgentUX079BirthdaySchedule(ctx context.Context, deps AgentUX079BirthdayScheduleDeps) (AgentUX079BirthdayScheduleReceipt, error) {
	var receipt AgentUX079BirthdayScheduleReceipt
	if ctx == nil || deps.Announcements == nil || deps.Schedules == nil || deps.TenantUUID == nil || deps.Now == nil ||
		deps.Tenant == "" || deps.Admin == "" || deps.InstallationID == "" || deps.ConversationID == "" {
		return receipt, ErrAgentAnnouncementUnavailable
	}
	tenantID := deps.TenantUUID(values.TenantId(deps.Tenant))
	if tenantID == uuid.Nil {
		return receipt, ErrAgentAnnouncementUnavailable
	}
	now := deps.Now().UTC()
	if now.IsZero() {
		return receipt, ErrAgentAnnouncementUnavailable
	}
	actor := AgentAnnouncementActor{TenantID: deps.Tenant, TenantUUID: tenantID, SubjectID: deps.Admin}
	record, err := deps.Announcements.Get(ctx, tenantID, AgentUX079BirthdayScheduleID)
	if err != nil && !errors.Is(err, agentstore.ErrAnnouncementNotFound) {
		return receipt, err
	}
	if errors.Is(err, agentstore.ErrAnnouncementNotFound) {
		record, err = agentUX079BirthdayCreate(ctx, deps, actor, tenantID, now)
		if err != nil {
			return receipt, err
		}
		receipt.Created = true
	}
	if record.OwnerID != deps.Admin || record.PersonaID != AgentUXDemoBirthdayPersonaID || record.State == agentstore.AnnouncementDeleted || record.State == agentstore.AnnouncementActive {
		receipt.Checked = true
		return receipt, nil
	}
	if record.State != agentstore.AnnouncementPaused || !deps.RouteExists {
		receipt.Checked = true
		return receipt, nil
	}
	next, err := deps.Schedules.ResumeAnnouncementSchedule(ctx, actor, record.SchedulerID, record.Revision)
	if err != nil {
		return receipt, err
	}
	resumed := record
	resumed.State, resumed.Revision, resumed.UpdatedAt, resumed.NextRunAt = agentstore.AnnouncementActive, record.Revision+1, now, next
	if err := deps.Announcements.Save(ctx, resumed, record.Revision); err != nil {
		return receipt, err
	}
	receipt.Checked = true
	receipt.Resumed = true
	return receipt, nil
}

// agentUX079BirthdayCreate files the paused schedule: the native entry first
// (created active by the substrate, then paused), then the announcement row.
// A native entry left behind by an interrupted run reports a revision
// conflict and is paused into place; a record created by a racing preparation
// reports a revision conflict and the winner's row is adopted.
func agentUX079BirthdayCreate(ctx context.Context, deps AgentUX079BirthdayScheduleDeps, actor AgentAnnouncementActor, tenantID uuid.UUID, now time.Time) (agentstore.Announcement, error) {
	at := time.Date(now.Year(), now.Month(), now.Day(), agentUX079BirthdayHour, 0, 0, 0, time.UTC)
	draft := AgentAnnouncementDraft{ID: AgentUX079BirthdayScheduleID, InstallationID: deps.InstallationID, PersonaID: AgentUXDemoBirthdayPersonaID, ConversationID: deps.ConversationID, Instruction: agentUX079BirthdayInstruction, Schedule: AgentAnnouncementSchedule{Cadence: agentUX079BirthdayCadence, At: at, Zone: agentUX079BirthdayZone}}
	if validateAgentAnnouncementDraft(draft) != nil {
		return agentstore.Announcement{}, ErrAgentAnnouncementInvalid
	}
	cron, err := AgentAnnouncementCron(draft.Schedule)
	if err != nil {
		return agentstore.Announcement{}, err
	}
	record := agentstore.Announcement{
		TenantID: tenantID, TenantKey: deps.Tenant, ID: draft.ID, InstallationID: draft.InstallationID, PersonaID: draft.PersonaID,
		ConversationID: draft.ConversationID, Instruction: draft.Instruction,
		Cadence: draft.Schedule.Cadence, LocalTime: draft.Schedule.At, Zone: draft.Schedule.Zone,
		State: agentstore.AnnouncementPaused, OwnerID: actor.SubjectID, SchedulerID: "announcement:" + draft.ID, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, _, err := deps.Schedules.CreateAnnouncementSchedule(ctx, actor, record, cron); err != nil && !errors.Is(err, agentstore.ErrAnnouncementRevision) {
		return agentstore.Announcement{}, err
	}
	if err := deps.Schedules.PauseAnnouncementSchedule(ctx, actor, record.SchedulerID, 0); err != nil {
		return agentstore.Announcement{}, err
	}
	if err := deps.Announcements.Create(ctx, record); err != nil {
		if !errors.Is(err, agentstore.ErrAnnouncementRevision) {
			return agentstore.Announcement{}, err
		}
		adopted, getErr := deps.Announcements.Get(ctx, tenantID, AgentUX079BirthdayScheduleID)
		if getErr != nil {
			return agentstore.Announcement{}, err
		}
		return adopted, nil
	}
	return record, nil
}

// The preparation bindings assemble the Birthday Buddy schedule's executable
// trigger from the stores' current facts. They enforce the same alignment the
// served runtime enforces at admission — service identity, current persona
// authority and seal, manifest pin and instructions, budget, legal entity and
// audience — over narrow ports so tests substitute fakes. The firing path
// re-resolves every fact at admission, so a schedule bound here is checked
// again before it ever runs.
type (
	AgentUX079BirthdayBindingPrincipals interface {
		CurrentAnnouncementServicePrincipal(context.Context, string, string) (AgentAnnouncementInstallationIdentity, error)
	}
	AgentUX079BirthdayBindingPersonas interface {
		ReadCurrentPersonaAuthority(context.Context, string, string) (agentpersonastore.PersonaVersion, agentpersonastore.ActiveInstallation, error)
	}
	AgentUX079BirthdayBindingManifests interface {
		ResolveAgentManifestContext(context.Context, agentpersona.AgentManifestRef) (agentmanifest.Manifest, error)
	}
	AgentUX079BirthdayBindingBudgets interface {
		Resolve(context.Context, string, string) (PersonaRunEffectivePolicy, error)
	}
	AgentUX079BirthdayBindingLegal interface {
		ResolveBirthdayLegalEntity(context.Context, string, string) (string, error)
	}
	AgentUX079BirthdayBindingAudience interface {
		SnapshotBirthdayAudience(context.Context, string, string) (chatrecipient.AudienceSnapshot, error)
	}
)

// AgentUX079BirthdayScheduleBindings resolves one announcement record into
// its executable native schedule.
type AgentUX079BirthdayScheduleBindings struct {
	Principals AgentUX079BirthdayBindingPrincipals
	Personas   AgentUX079BirthdayBindingPersonas
	Manifests  AgentUX079BirthdayBindingManifests
	Budgets    AgentUX079BirthdayBindingBudgets
	Legal      AgentUX079BirthdayBindingLegal
	Audience   AgentUX079BirthdayBindingAudience
	Now        func() time.Time
}

// ResolveAnnouncementScheduleBinding binds the record to the current service
// identity, persona authority, manifest, budget, legal entity and audience.
// Anything missing or misaligned is denied.
func (b *AgentUX079BirthdayScheduleBindings) ResolveAnnouncementScheduleBinding(ctx context.Context, actor AgentAnnouncementActor, record agentstore.Announcement) (scheduled.Schedule, error) {
	if b == nil || b.Principals == nil || b.Personas == nil || b.Manifests == nil || b.Budgets == nil || b.Legal == nil || b.Audience == nil || b.Now == nil || ctx == nil {
		return scheduled.Schedule{}, ErrAgentAnnouncementDenied
	}
	if actor.TenantID == "" || actor.SubjectID == "" || record.TenantKey != actor.TenantID || record.OwnerID != actor.SubjectID {
		return scheduled.Schedule{}, ErrAgentAnnouncementDenied
	}
	identity, err := b.Principals.CurrentAnnouncementServicePrincipal(ctx, record.TenantKey, record.InstallationID)
	if err != nil || identity.PersonaID != record.PersonaID || identity.ConversationID != record.ConversationID {
		return scheduled.Schedule{}, ErrAgentAnnouncementDenied
	}
	version, installation, err := b.Personas.ReadCurrentPersonaAuthority(ctx, record.ConversationID, record.PersonaID)
	var profile agentpersona.PersonaProfile
	if err != nil || installation.InstallationID != record.InstallationID || json.Unmarshal(version.Profile, &profile) != nil {
		return scheduled.Schedule{}, fmt.Errorf("%w: current installation", ErrAgentAnnouncementDenied)
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil || sealed.Digest != version.ContentDigest {
		return scheduled.Schedule{}, fmt.Errorf("%w: sealed profile", ErrAgentAnnouncementDenied)
	}
	manifest, err := b.Manifests.ResolveAgentManifestContext(ctx, profile.Manifest)
	if err != nil || !validPersonaChatReplySchemaPin(manifest.OutputSchema) || !personaInstructionsMatchDigest(profile.Instructions, manifest.InstructionsDigest) {
		return scheduled.Schedule{}, fmt.Errorf("%w: sealed instructions", ErrAgentAnnouncementDenied)
	}
	digest, err := manifest.Digest()
	if err != nil || digest != profile.Manifest.Digest {
		return scheduled.Schedule{}, fmt.Errorf("%w: sealed manifest", ErrAgentAnnouncementDenied)
	}
	legal, err := b.Legal.ResolveBirthdayLegalEntity(ctx, record.TenantKey, record.OwnerID)
	if err != nil || legal == "" {
		return scheduled.Schedule{}, ErrAgentAnnouncementDenied
	}
	policy, err := b.Budgets.Resolve(ctx, record.TenantKey, legal)
	if err != nil || !policy.Deadline.After(b.Now()) {
		return scheduled.Schedule{}, ErrAgentAnnouncementDenied
	}
	snapshot, err := b.Audience.SnapshotBirthdayAudience(ctx, record.TenantKey, record.ConversationID)
	if err != nil || !snapshot.Complete || !snapshot.GuestAndExternalComplete || snapshot.Revision == 0 {
		return scheduled.Schedule{}, ErrAgentAnnouncementDenied
	}
	members, _ := json.Marshal(snapshot.CurrentMembers)
	audience := agentrun.AudienceScope{ID: record.ConversationID, SnapshotID: fmt.Sprint(snapshot.Revision), Digest: personaRunBytesDigest(members)}
	target := &schedule.AgentRunTarget{Agent: schedule.AgentVersionRef{ID: manifest.ID, Version: fmt.Sprint(manifest.Version), Digest: digest}, SponsorID: identity.SubjectID, Purpose: personaChatReplyPurpose, Budget: schedule.AgentRunBudget{MaxCostMicros: policy.Budget.MaxCostMicros, MaxInputTokens: policy.Budget.MaxInputTokens, MaxOutputTokens: policy.Budget.MaxOutputTokens}, Destination: schedule.AgentRunDestination{AudienceID: record.ConversationID, AudienceSnapshotID: audience.SnapshotID, AudienceDigest: audience.Digest}}
	return scheduled.Schedule{SourceKind: agentrun.SourceAnnouncement, Persona: &agentrun.PersonaRef{ID: profile.PersonaID, Version: fmt.Sprint(profile.Version), Digest: mustAnnouncementProfileDigest(profile)}, RequesterID: record.OwnerID, OwnerID: record.OwnerID, InstallationID: record.InstallationID, AgentPrincipalID: identity.PrincipalID, LegalEntity: legal, Zone: values.ZoneRef{ID: record.Zone, TzdbVersion: "2026a"}, RunTimeout: policy.Deadline.Sub(b.Now()), Trigger: schedule.PublishedTrigger{Definition: schedule.TriggerDefinition{TenantID: actor.TenantID, TargetKind: schedule.TargetAgentRun, AgentRun: target, Purpose: personaChatReplyPurpose, Storm: schedule.StormPolicy{MaxFiringsPerWindow: 400, Window: 366 * 24 * time.Hour}, ExecutionMode: intent.ModeExecute, ExecutionEnvironment: intent.EnvironmentProduction}}}, nil
}

var _ AgentAnnouncementScheduleBindingSource = (*AgentUX079BirthdayScheduleBindings)(nil)

// agentUX079BirthdayLegal resolves the legal entity a schedule runs under.
type agentUX079BirthdayLegal struct {
	Resolver PersonaRunLegalEntityResolver
	Now      func() time.Time
}

func (a agentUX079BirthdayLegal) ResolveBirthdayLegalEntity(ctx context.Context, tenant, owner string) (string, error) {
	if a.Now == nil {
		return "", ErrAgentAnnouncementDenied
	}
	return a.Resolver.Resolve(ctx, agentinvoke.RunRequest{TenantID: tenant, Mode: agentinvoke.OnBehalfOf, InvokerID: owner})
}

// agentUX079BirthdayAudience snapshots the audience of the channel the
// schedule posts in.
type agentUX079BirthdayAudience struct {
	Chat      *chatstore.Store
	Authority *PersonaAudienceFloorAdapter
}

func (a agentUX079BirthdayAudience) SnapshotBirthdayAudience(ctx context.Context, tenant, conversation string) (chatrecipient.AudienceSnapshot, error) {
	if a.Chat == nil || a.Authority == nil || ctx == nil {
		return chatrecipient.AudienceSnapshot{}, ErrAgentAnnouncementDenied
	}
	current := chat.Conversation{TenantID: tenant, ID: conversation}
	err := a.Chat.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT kind,lifecycle<>'ACTIVE',settings_revision FROM chat_conversation WHERE tenant_id=$1 AND id=$2`, tenant, conversation).Scan(&current.Kind, &current.Archived, &current.Revision)
	})
	if current.Archived {
		return chatrecipient.AudienceSnapshot{}, errors.Join(ErrAgentAnnouncementDenied, ErrAgentAnnouncementChannelLocked)
	}
	if err != nil || !agentAnnouncementChannel(current.Kind) || current.Revision == 0 {
		return chatrecipient.AudienceSnapshot{}, fmt.Errorf("%w: current conversation kind=%s revision=%d: %v", ErrAgentAnnouncementDenied, current.Kind, current.Revision, err)
	}
	return a.Authority.CurrentAudience(ctx, current)
}

// agentUX079BirthdayTriggerStore is the native schedule substrate over the
// agent database.
type agentUX079BirthdayTriggerStore struct {
	Agents     *agentstore.Store
	TenantUUID func(values.TenantId) uuid.UUID
}

func (s agentUX079BirthdayTriggerStore) tenant(tenant string) (*agenttriggerstore.Store, error) {
	if s.Agents == nil || s.TenantUUID == nil {
		return nil, ErrAgentAnnouncementUnavailable
	}
	return agenttriggerstore.NewAnnouncementSource(s.Agents, s.TenantUUID(values.TenantId(tenant)), tenant, agenttriggerstore.AgentScheduleExecutions{Database: s.Agents})
}

func (s agentUX079BirthdayTriggerStore) Load(ctx context.Context, tenant, id string) (scheduled.Schedule, bool, error) {
	store, err := s.tenant(tenant)
	if err != nil {
		return scheduled.Schedule{}, false, err
	}
	return store.Load(ctx, tenant, id)
}

func (s agentUX079BirthdayTriggerStore) Save(ctx context.Context, record scheduled.Schedule, expected uint64, audit scheduled.Audit) error {
	store, err := s.tenant(record.Trigger.Definition.TenantID)
	if err != nil {
		return err
	}
	return store.Save(ctx, record, expected, audit)
}

// AgentUX079BirthdayScheduleWiring is everything preparation hands the
// birthday schedule step: the stores, the tenant, and the channel Birthday
// Buddy is installed in.
type AgentUX079BirthdayScheduleWiring struct {
	Agents       *agentstore.Store
	Personas     *agentpersonastore.Store
	Core         dbport.Beginner
	Chat         *chatstore.Store
	TenantUUID   func(values.TenantId) uuid.UUID
	Tenant       string
	Admin        string
	Conversation string
	Now          func() time.Time
}

// prepareAgentUX079BirthdaySchedule ensures the Birthday Buddy schedule from
// the preparation's stores: the current installation, the model route's
// existence, the announcement table and the native substrate with preparation
// bindings.
func prepareAgentUX079BirthdaySchedule(ctx context.Context, wiring AgentUX079BirthdayScheduleWiring) (AgentUX079BirthdayScheduleReceipt, error) {
	var receipt AgentUX079BirthdayScheduleReceipt
	if ctx == nil || wiring.Agents == nil || wiring.Personas == nil || wiring.Core == nil || wiring.Chat == nil || wiring.TenantUUID == nil || wiring.Now == nil ||
		wiring.Tenant == "" || wiring.Admin == "" || wiring.Conversation == "" {
		return receipt, ErrAgentAnnouncementUnavailable
	}
	now := wiring.Now().UTC()
	if now.IsZero() {
		return receipt, ErrAgentAnnouncementUnavailable
	}
	scoped, err := wiring.Personas.Scoped(values.TenantId(wiring.Tenant))
	if err != nil {
		return receipt, err
	}
	version, installation, err := scoped.ReadCurrentPersonaAuthority(ctx, wiring.Conversation, AgentUXDemoBirthdayPersonaID)
	if err != nil {
		return receipt, fmt.Errorf("birthday installation: %w", err)
	}
	var profile agentpersona.PersonaProfile
	if err := json.Unmarshal(version.Profile, &profile); err != nil {
		return receipt, fmt.Errorf("birthday profile: %w", err)
	}
	routeExists, err := agentUX079BirthdayRouteExists(ctx, wiring, profile.Manifest.Digest, now)
	if err != nil {
		return receipt, err
	}
	budgets, err := NewPersonaRunEffectivePolicyResolver(wiring.Agents, wiring.TenantUUID, wiring.Now)
	if err != nil {
		return receipt, err
	}
	chatAuthority := NewPersonaPublicAudienceAuthority(wiring.Chat)
	bindings := &AgentUX079BirthdayScheduleBindings{
		Principals: AgentAnnouncementServicePrincipal{Agents: wiring.Agents, Personas: wiring.Personas, Principals: GovernancePersonaPrincipalAuthority{DB: wiring.Core, TenantUUID: wiring.TenantUUID}, TenantUUID: wiring.TenantUUID, Now: wiring.Now},
		Personas:   scoped,
		Manifests:  AgentManifestStoreAdapter{Store: wiring.Agents, TenantID: wiring.TenantUUID(values.TenantId(wiring.Tenant))},
		Budgets:    budgets,
		Legal:      agentUX079BirthdayLegal{Resolver: PersonaRunLegalEntityResolver{DB: wiring.Core, TenantUUID: wiring.TenantUUID, Now: wiring.Now}, Now: wiring.Now},
		Audience:   agentUX079BirthdayAudience{Chat: wiring.Chat, Authority: NewPersonaAudienceFloorAdapter(chatAuthority, chatAuthority, chatAuthority)},
		Now:        wiring.Now,
	}
	announcements, err := agentstore.NewAnnouncementStore(wiring.Agents)
	if err != nil {
		return receipt, err
	}
	return ensureAgentUX079BirthdaySchedule(ctx, AgentUX079BirthdayScheduleDeps{
		Announcements:  announcements,
		Schedules:      AgentAnnouncementNativeSchedules{Store: agentUX079BirthdayTriggerStore{Agents: wiring.Agents, TenantUUID: wiring.TenantUUID}, Bindings: bindings, Now: wiring.Now},
		TenantUUID:     wiring.TenantUUID,
		Tenant:         wiring.Tenant,
		Admin:          wiring.Admin,
		InstallationID: installation.InstallationID,
		ConversationID: wiring.Conversation,
		RouteExists:    routeExists,
		Now:            wiring.Now,
	})
}

// agentUX079BirthdayRouteExists reports whether Birthday Buddy's model route
// policy exists right now: a missing policy is the ordinary paused state, any
// other failure aborts the step.
func agentUX079BirthdayRouteExists(ctx context.Context, wiring AgentUX079BirthdayScheduleWiring, manifestDigest string, now time.Time) (bool, error) {
	legalEntity, err := (PersonaRunLegalEntityResolver{DB: wiring.Core, TenantUUID: wiring.TenantUUID, Now: wiring.Now}).Resolve(ctx, agentinvoke.RunRequest{TenantID: wiring.Tenant, Mode: agentinvoke.OnBehalfOf, InvokerID: wiring.Admin})
	if err != nil {
		return false, err
	}
	ref, _ := LocalPersonaOpenAIModelPolicyReference()
	_, err = wiring.Agents.CurrentPersonaModelRoutePolicyForAgent(ctx, wiring.TenantUUID(values.TenantId(wiring.Tenant)), legalEntity, ref.ID, int64(ref.Version), int64(ref.SchemaVersion), ref.Digest, manifestDigest, now)
	if errors.Is(err, agentstore.ErrPersonaModelRouteNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
