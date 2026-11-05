package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// agentUX079FakeAnnouncements is the announcement table behind proof 4, with
// the store's revision rule: the first write is revision 1 and a save must
// name the stored revision.
type agentUX079FakeAnnouncements struct {
	records map[string]agentstore.Announcement
	creates int
	saves   int
}

func agentUX079FakeAnnouncementKey(tenant uuid.UUID, id string) string {
	return tenant.String() + "\x00" + id
}

func (f *agentUX079FakeAnnouncements) Get(_ context.Context, tenant uuid.UUID, id string) (agentstore.Announcement, error) {
	if r, ok := f.records[agentUX079FakeAnnouncementKey(tenant, id)]; ok {
		return r, nil
	}
	return agentstore.Announcement{}, agentstore.ErrAnnouncementNotFound
}

func (f *agentUX079FakeAnnouncements) Create(_ context.Context, r agentstore.Announcement) error {
	if f.records == nil {
		f.records = map[string]agentstore.Announcement{}
	}
	key := agentUX079FakeAnnouncementKey(r.TenantID, r.ID)
	if _, ok := f.records[key]; ok || r.Revision != 1 {
		return agentstore.ErrAnnouncementRevision
	}
	f.records[key] = r
	f.creates++
	return nil
}

func (f *agentUX079FakeAnnouncements) Save(_ context.Context, r agentstore.Announcement, expected uint64) error {
	key := agentUX079FakeAnnouncementKey(r.TenantID, r.ID)
	stored, ok := f.records[key]
	if !ok || stored.Revision != expected || r.Revision != expected+1 {
		return agentstore.ErrAnnouncementRevision
	}
	f.records[key] = r
	f.saves++
	return nil
}

// agentUX079FakeSchedules is the native schedule substrate behind proof 4. It
// mirrors the native owner's semantics: creation names an active schedule and
// state changes are idempotent rewrites.
type agentUX079FakeSchedules struct {
	states    map[string]string
	revisions map[string]uint64
	creates   int
	pauses    int
	resumes   int
	now       time.Time
}

func (f *agentUX079FakeSchedules) CreateAnnouncementSchedule(_ context.Context, actor AgentAnnouncementActor, record agentstore.Announcement, cron string) (string, *time.Time, error) {
	if f.states == nil {
		f.states = map[string]string{}
		f.revisions = map[string]uint64{}
	}
	if cron == "" || actor.SubjectID != record.OwnerID {
		return "", nil, ErrAgentAnnouncementDenied
	}
	if _, ok := f.states[record.SchedulerID]; ok {
		return "", nil, agentstore.ErrAnnouncementRevision
	}
	f.states[record.SchedulerID] = scheduled.StateActive
	f.revisions[record.SchedulerID] = 1
	f.creates++
	next := f.now.Add(24 * time.Hour)
	return record.SchedulerID, &next, nil
}

func (f *agentUX079FakeSchedules) UpdateAnnouncementSchedule(context.Context, AgentAnnouncementActor, agentstore.Announcement, string) (*time.Time, error) {
	return nil, errors.New("not implemented")
}

func (f *agentUX079FakeSchedules) PauseAnnouncementSchedule(_ context.Context, _ AgentAnnouncementActor, id string, _ uint64) error {
	f.states[id] = scheduled.StatePaused
	f.revisions[id]++
	f.pauses++
	return nil
}

func (f *agentUX079FakeSchedules) ResumeAnnouncementSchedule(_ context.Context, _ AgentAnnouncementActor, id string, _ uint64) (*time.Time, error) {
	f.states[id] = scheduled.StateActive
	f.revisions[id]++
	f.resumes++
	next := f.now.Add(24 * time.Hour)
	return &next, nil
}

func (f *agentUX079FakeSchedules) DeleteAnnouncementSchedule(context.Context, AgentAnnouncementActor, string, uint64) error {
	return errors.New("not implemented")
}

func (f *agentUX079FakeSchedules) NextAnnouncementRun(context.Context, AgentAnnouncementActor, agentstore.Announcement) (*time.Time, error) {
	return nil, errors.New("not implemented")
}

// agentUX079FakeOwnerStore stands in for the native schedule table where the
// definition gate only checks it is present.
type agentUX079FakeOwnerStore struct{}

func (agentUX079FakeOwnerStore) Load(context.Context, string, string) (scheduled.Schedule, bool, error) {
	return scheduled.Schedule{}, false, errors.New("not implemented")
}

func (agentUX079FakeOwnerStore) Save(context.Context, scheduled.Schedule, uint64, scheduled.Audit) error {
	return errors.New("not implemented")
}

func agentUX079BirthdayTestDeps(announcements *agentUX079FakeAnnouncements, schedules *agentUX079FakeSchedules, now time.Time, routeExists bool) AgentUX079BirthdayScheduleDeps {
	return AgentUX079BirthdayScheduleDeps{
		Announcements: announcements, Schedules: schedules,
		TenantUUID:     func(values.TenantId) uuid.UUID { return uuid.NewSHA1(uuid.NameSpaceOID, []byte("agentux079-birthday")) },
		Tenant:         localAgentDemoTenant,
		Admin:          localAgentDemoAdmin,
		InstallationID: "birthday-installation",
		ConversationID: "general-conversation",
		RouteExists:    routeExists,
		Now:            func() time.Time { return now },
	}
}

var _ AgentUX079BirthdayAnnouncements = (*agentUX079FakeAnnouncements)(nil)

func agentUX079BirthdayTestRecord(deps AgentUX079BirthdayScheduleDeps, now time.Time) agentstore.Announcement {
	return agentstore.Announcement{TenantID: deps.TenantUUID(values.TenantId(deps.Tenant)), TenantKey: deps.Tenant, ID: AgentUX079BirthdayScheduleID, InstallationID: deps.InstallationID, PersonaID: AgentUXDemoBirthdayPersonaID, ConversationID: deps.ConversationID, Instruction: "Wish everyone whose birthday is today a happy birthday in the channel.", Cadence: "DAILY", LocalTime: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC), Zone: "UTC", State: agentstore.AnnouncementPaused, OwnerID: deps.Admin, SchedulerID: "announcement:" + AgentUX079BirthdayScheduleID, Revision: 1, CreatedAt: now, UpdatedAt: now}
}

// TestAgentUX079_BirthdaySchedulePausedWithoutRoute: without a model route the
// preparation creates the Birthday Buddy schedule paused and leaves it
// paused; a rerun changes nothing.
func TestAgentUX079_BirthdaySchedulePausedWithoutRoute(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	announcements, schedules := &agentUX079FakeAnnouncements{}, &agentUX079FakeSchedules{now: now}
	deps := agentUX079BirthdayTestDeps(announcements, schedules, now, false)
	receipt, err := ensureAgentUX079BirthdaySchedule(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Checked || !receipt.Created || receipt.Resumed {
		t.Fatalf("first prepare = %+v, want checked and created but not resumed", receipt)
	}
	tenant := deps.TenantUUID(values.TenantId(deps.Tenant))
	stored, err := announcements.Get(context.Background(), tenant, AgentUX079BirthdayScheduleID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != agentstore.AnnouncementPaused || stored.Revision != 1 || stored.NextRunAt != nil {
		t.Fatalf("record = %+v, want a paused revision 1 with no next run", stored)
	}
	if stored.PersonaID != AgentUXDemoBirthdayPersonaID || stored.Cadence != "DAILY" || stored.Zone != "UTC" || stored.LocalTime.Hour() != 9 || stored.Instruction == "" || len(stored.Documents) != 0 {
		t.Fatalf("record = %+v, want the daily birthday schedule with no documents", stored)
	}
	if state := schedules.states["announcement:"+AgentUX079BirthdayScheduleID]; state != scheduled.StatePaused {
		t.Fatalf("native schedule state = %q, want paused", state)
	}
	rerun, err := ensureAgentUX079BirthdaySchedule(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if !rerun.Checked || rerun.Created || rerun.Resumed {
		t.Fatalf("second prepare = %+v, want checked with no change", rerun)
	}
	if announcements.creates != 1 || schedules.creates != 1 || schedules.pauses != 1 {
		t.Fatalf("creates = %d, native creates = %d, pauses = %d, want one of each", announcements.creates, schedules.creates, schedules.pauses)
	}
}

// TestAgentUX079_BirthdayScheduleResumedWithRoute: once the model route exists
// the preparation resumes a paused schedule, and a schedule created while the
// route already exists is active from its first run.
func TestAgentUX079_BirthdayScheduleResumedWithRoute(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	announcements, schedules := &agentUX079FakeAnnouncements{}, &agentUX079FakeSchedules{now: now}
	deps := agentUX079BirthdayTestDeps(announcements, schedules, now, true)
	receipt, err := ensureAgentUX079BirthdaySchedule(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Checked || !receipt.Created || !receipt.Resumed {
		t.Fatalf("prepare with route = %+v, want checked, created and resumed", receipt)
	}
	tenant := deps.TenantUUID(values.TenantId(deps.Tenant))
	stored, err := announcements.Get(context.Background(), tenant, AgentUX079BirthdayScheduleID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != agentstore.AnnouncementActive || stored.Revision != 2 || stored.NextRunAt == nil {
		t.Fatalf("record = %+v, want an active revision 2 with a next run", stored)
	}
	if state := schedules.states["announcement:"+AgentUX079BirthdayScheduleID]; state != scheduled.StateActive {
		t.Fatalf("native schedule state = %q, want active", state)
	}

	paused := &agentUX079FakeAnnouncements{}
	pausedSchedules := &agentUX079FakeSchedules{now: now, states: map[string]string{"announcement:" + AgentUX079BirthdayScheduleID: scheduled.StatePaused}, revisions: map[string]uint64{"announcement:" + AgentUX079BirthdayScheduleID: 2}}
	pausedDeps := agentUX079BirthdayTestDeps(paused, pausedSchedules, now, true)
	record := agentUX079BirthdayTestRecord(pausedDeps, now)
	if err := paused.Create(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	resumed, err := ensureAgentUX079BirthdaySchedule(context.Background(), pausedDeps)
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.Checked || resumed.Created || !resumed.Resumed {
		t.Fatalf("resume = %+v, want checked and resumed without creating", resumed)
	}
	after, err := paused.Get(context.Background(), pausedDeps.TenantUUID(values.TenantId(pausedDeps.Tenant)), AgentUX079BirthdayScheduleID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != agentstore.AnnouncementActive || after.Revision != 2 || after.NextRunAt == nil {
		t.Fatalf("record = %+v, want an active revision 2 with a next run", after)
	}
}

// TestAgentUX079_BirthdayScheduleLeavesOthersAlone: an active schedule keeps
// running with or without a route, a deleted schedule is never resurrected,
// and a schedule another owner filed under the same identity is not ours to
// touch.
func TestAgentUX079_BirthdayScheduleLeavesOthersAlone(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name        string
		routeExists bool
		mutate      func(*agentstore.Announcement)
	}{
		{"active without route", false, func(r *agentstore.Announcement) { r.State = agentstore.AnnouncementActive }},
		{"active with route", true, func(r *agentstore.Announcement) { r.State = agentstore.AnnouncementActive }},
		{"deleted with route", true, func(r *agentstore.Announcement) { r.State = agentstore.AnnouncementDeleted }},
		{"another owner's schedule", true, func(r *agentstore.Announcement) { r.OwnerID = "another-owner" }},
		{"another persona's schedule", true, func(r *agentstore.Announcement) { r.PersonaID = "hcmnext.local.persona.policy_helper" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			announcements, schedules := &agentUX079FakeAnnouncements{}, &agentUX079FakeSchedules{now: now}
			deps := agentUX079BirthdayTestDeps(announcements, schedules, now, tc.routeExists)
			record := agentUX079BirthdayTestRecord(deps, now)
			tc.mutate(&record)
			if err := announcements.Create(context.Background(), record); err != nil {
				t.Fatal(err)
			}
			receipt, err := ensureAgentUX079BirthdaySchedule(context.Background(), deps)
			if err != nil {
				t.Fatal(err)
			}
			if !receipt.Checked || receipt.Created || receipt.Resumed {
				t.Fatalf("receipt = %+v, want checked with no change", receipt)
			}
			if announcements.saves != 0 || schedules.resumes != 0 {
				t.Fatalf("saves = %d, resumes = %d, want the record untouched", announcements.saves, schedules.resumes)
			}
		})
	}
}

// TestAgentUX079_BirthdayScheduleConverges: a native schedule left behind by
// an interrupted run, and a record created by a racing preparation, both
// converge instead of failing.
func TestAgentUX079_BirthdayScheduleConverges(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	announcements := &agentUX079FakeAnnouncements{}
	schedules := &agentUX079FakeSchedules{now: now, states: map[string]string{"announcement:" + AgentUX079BirthdayScheduleID: scheduled.StateActive}, revisions: map[string]uint64{"announcement:" + AgentUX079BirthdayScheduleID: 1}}
	deps := agentUX079BirthdayTestDeps(announcements, schedules, now, false)
	receipt, err := ensureAgentUX079BirthdaySchedule(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Checked || !receipt.Created || receipt.Resumed {
		t.Fatalf("orphan prepare = %+v, want checked and created", receipt)
	}
	if state := schedules.states["announcement:"+AgentUX079BirthdayScheduleID]; state != scheduled.StatePaused {
		t.Fatalf("native schedule state = %q, want the orphan paused", state)
	}
}

// agentUX079FakeBindingFacts answers every fact the preparation bindings read,
// failing the one the test names.
type agentUX079FakeBindingFacts struct {
	identity     AgentAnnouncementInstallationIdentity
	profile      agentpersona.PersonaProfile
	installation agentpersonastore.ActiveInstallation
	manifest     agentmanifest.Manifest
	policy       PersonaRunEffectivePolicy
	legal        string
	snapshot     chatrecipient.AudienceSnapshot
	errAt        string
}

func (f *agentUX079FakeBindingFacts) fail(at string) error {
	if f.errAt == at {
		return errors.New("unavailable")
	}
	return nil
}

func (f *agentUX079FakeBindingFacts) CurrentAnnouncementServicePrincipal(context.Context, string, string) (AgentAnnouncementInstallationIdentity, error) {
	return f.identity, f.fail("principals")
}

func (f *agentUX079FakeBindingFacts) ReadCurrentPersonaAuthority(context.Context, string, string) (agentpersonastore.PersonaVersion, agentpersonastore.ActiveInstallation, error) {
	raw, err := json.Marshal(f.profile)
	if err != nil {
		return agentpersonastore.PersonaVersion{}, agentpersonastore.ActiveInstallation{}, err
	}
	sealed, err := agentpersona.Seal(f.profile)
	if err != nil {
		return agentpersonastore.PersonaVersion{}, agentpersonastore.ActiveInstallation{}, err
	}
	return agentpersonastore.PersonaVersion{PersonaID: f.profile.PersonaID, Version: int64(f.profile.Version), Profile: raw, ContentDigest: sealed.Digest}, f.installation, f.fail("personas")
}

func (f *agentUX079FakeBindingFacts) ResolveAgentManifestContext(context.Context, agentpersona.AgentManifestRef) (agentmanifest.Manifest, error) {
	return f.manifest, f.fail("manifests")
}

func (f *agentUX079FakeBindingFacts) Resolve(context.Context, string, string) (PersonaRunEffectivePolicy, error) {
	return f.policy, f.fail("budgets")
}

func (f *agentUX079FakeBindingFacts) ResolveBirthdayLegalEntity(context.Context, string, string) (string, error) {
	return f.legal, f.fail("legal")
}

func (f *agentUX079FakeBindingFacts) SnapshotBirthdayAudience(context.Context, string, string) (chatrecipient.AudienceSnapshot, error) {
	return f.snapshot, f.fail("audience")
}

func agentUX079BirthdayTestBindings(now time.Time) (*AgentUX079BirthdayScheduleBindings, *agentUX079FakeBindingFacts, agentstore.Announcement, AgentAnnouncementActor) {
	instructions := "Wish everyone whose birthday is today a happy birthday in the channel."
	sum := sha256.Sum256([]byte(instructions))
	policySum := sha256.Sum256([]byte("birthday-model-policy"))
	manifest := agentmanifest.Manifest{SchemaVersion: agentmanifest.CurrentSchemaVersion, ID: "birthday-manifest", Version: 7, OwnerID: "owner", Purpose: "test", InstructionsDigest: "sha256:" + hex.EncodeToString(sum[:]),
		SourceCeiling: []agentmanifest.Reference{}, ToolCeiling: []agentmanifest.Reference{},
		ModelPolicy:     agentmanifest.Reference{ID: "model-policy", Version: 1, SchemaVersion: 1, Digest: "sha256:" + hex.EncodeToString(policySum[:])},
		AutonomyCeiling: "L2",
		Budget:          agentmanifest.Budget{MaxCostMicros: 1000, MaxInputTokens: 1000, MaxOutputTokens: 1000, MaxConcurrentRuns: 1},
		OutputSchema:    agentmanifest.Reference{ID: PersonaChatReplySchema, Version: 1, SchemaVersion: 1, Digest: PersonaChatReplySchemaDigest},
		EvaluationRefs:  []agentmanifest.Reference{{ID: "suite", Version: 1, SchemaVersion: 1, Digest: "sha256:" + hex.EncodeToString(policySum[:])}}}
	digest, err := manifest.Digest()
	if err != nil {
		panic(err)
	}
	profile := agentpersona.PersonaProfile{PersonaID: AgentUXDemoBirthdayPersonaID, Version: 3, Handle: "birthday-buddy", DisplayName: "Birthday Buddy", AvatarRef: "avatar", Purpose: "test", Owner: "owner", Steward: "steward", EvalSuiteRef: "suite",
		Audience:   agentpersona.Audience{Roles: []string{"member"}, Populations: []string{"workspace"}, OrganizationScopes: []string{"tenant"}},
		EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1}, Instructions: instructions,
		SkillPins: []agentskills.SkillPin{{ID: "persona.chat_reply", Version: 1, Digest: "sha256:test"}}, TierCeiling: agentskills.TierT1,
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationChannel}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPublic}}
	profile.Manifest = agentpersona.AgentManifestRef{ID: "birthday-manifest", Version: 7, SchemaVersion: 1, Digest: digest}
	facts := &agentUX079FakeBindingFacts{
		identity:     AgentAnnouncementInstallationIdentity{PersonaID: AgentUXDemoBirthdayPersonaID, ConversationID: "general-conversation", SubjectID: "birthday-service", PrincipalID: "birthday-principal"},
		profile:      profile,
		installation: agentpersonastore.ActiveInstallation{InstallationID: "birthday-installation", PersonaID: AgentUXDemoBirthdayPersonaID, ConversationID: "general-conversation"},
		manifest:     manifest,
		policy:       PersonaRunEffectivePolicy{Budget: agentrun.Budget{MaxCostMicros: 1000, MaxInputTokens: 1000, MaxOutputTokens: 1000}, Deadline: now.Add(time.Hour)},
		legal:        "birthday-legal",
		snapshot:     chatrecipient.AudienceSnapshot{Complete: true, GuestAndExternalComplete: true, Revision: 9},
	}
	bindings := &AgentUX079BirthdayScheduleBindings{Principals: facts, Personas: facts, Manifests: facts, Budgets: facts, Legal: facts, Audience: facts, Now: func() time.Time { return now }}
	record := agentstore.Announcement{TenantKey: localAgentDemoTenant, ID: AgentUX079BirthdayScheduleID, InstallationID: "birthday-installation", PersonaID: AgentUXDemoBirthdayPersonaID, ConversationID: "general-conversation", Zone: "UTC", OwnerID: localAgentDemoAdmin, Revision: 1, State: agentstore.AnnouncementPaused}
	actor := AgentAnnouncementActor{TenantID: localAgentDemoTenant, SubjectID: localAgentDemoAdmin}
	return bindings, facts, record, actor
}

// TestAgentUX079_BirthdayPersonaIdentity: the store's birthday exemption and
// the application's birthday persona are the same identity; if either moves,
// the documents rule drifts.
func TestAgentUX079_BirthdayPersonaIdentity(t *testing.T) {
	if agentstore.AnnouncementBirthdayPersonaID != AgentUXDemoBirthdayPersonaID {
		t.Fatalf("store birthday persona = %q, application = %q", agentstore.AnnouncementBirthdayPersonaID, AgentUXDemoBirthdayPersonaID)
	}
}

// TestAgentUX079_BirthdayScheduleBindings: the preparation bindings assemble
// the same executable schedule the served path would, from the stores'
// current facts, and refuse anything that does not line up.
func TestAgentUX079_BirthdayScheduleBindings(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	bindings, _, record, actor := agentUX079BirthdayTestBindings(now)
	record.SchedulerID = "announcement:" + record.ID
	assembled, err := bindings.ResolveAnnouncementScheduleBinding(context.Background(), actor, record)
	if err != nil {
		t.Fatal(err)
	}
	// The native owner publishes the bindings' definition and only accepts a
	// schedule that validates; the bindings are proved through that gate.
	published, err := (AgentAnnouncementNativeSchedules{Store: agentUX079FakeOwnerStore{}, Bindings: bindings, Now: func() time.Time { return now }}).definition(context.Background(), actor, record, "0 9 * * *", 0)
	if err != nil {
		t.Fatalf("the bindings do not publish a valid schedule: %v", err)
	}
	if published.State != scheduled.StateActive || published.Trigger.Definition.AgentRun == nil {
		t.Fatalf("published schedule = %+v", published)
	}
	definition := assembled.Trigger.Definition
	if definition.TenantID != localAgentDemoTenant || definition.TargetKind != schedule.TargetAgentRun || definition.AgentRun == nil {
		t.Fatalf("trigger definition = %+v", definition)
	}
	target := definition.AgentRun
	if target.Agent.ID != "birthday-manifest" || target.Agent.Version != "7" || target.Agent.Digest == "" || target.SponsorID != "birthday-service" || target.Purpose != personaChatReplyPurpose {
		t.Fatalf("agent run target = %+v", target)
	}
	if target.Destination.AudienceID != "general-conversation" || target.Destination.AudienceSnapshotID != "9" || target.Destination.AudienceDigest == "" {
		t.Fatalf("destination = %+v", target.Destination)
	}
	if assembled.Persona == nil || assembled.Persona.ID != AgentUXDemoBirthdayPersonaID || assembled.Persona.Version != "3" || assembled.Persona.Digest == "" {
		t.Fatalf("persona ref = %+v", assembled.Persona)
	}
	if assembled.InstallationID != "birthday-installation" || assembled.OwnerID != localAgentDemoAdmin || assembled.AgentPrincipalID != "birthday-principal" || assembled.LegalEntity != "birthday-legal" || assembled.Zone.ID != "UTC" || assembled.RunTimeout != time.Hour {
		t.Fatalf("schedule = %+v", assembled)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*agentUX079FakeBindingFacts, *agentstore.Announcement)
	}{
		{"principals unavailable", func(f *agentUX079FakeBindingFacts, _ *agentstore.Announcement) { f.errAt = "principals" }},
		{"personas unavailable", func(f *agentUX079FakeBindingFacts, _ *agentstore.Announcement) { f.errAt = "personas" }},
		{"manifests unavailable", func(f *agentUX079FakeBindingFacts, _ *agentstore.Announcement) { f.errAt = "manifests" }},
		{"budgets unavailable", func(f *agentUX079FakeBindingFacts, _ *agentstore.Announcement) { f.errAt = "budgets" }},
		{"legal unavailable", func(f *agentUX079FakeBindingFacts, _ *agentstore.Announcement) { f.errAt = "legal" }},
		{"audience unavailable", func(f *agentUX079FakeBindingFacts, _ *agentstore.Announcement) { f.errAt = "audience" }},
		{"identity names another persona", func(f *agentUX079FakeBindingFacts, _ *agentstore.Announcement) { f.identity.PersonaID = "other" }},
		{"identity names another conversation", func(f *agentUX079FakeBindingFacts, _ *agentstore.Announcement) { f.identity.ConversationID = "other" }},
		{"installation moved", func(f *agentUX079FakeBindingFacts, _ *agentstore.Announcement) {
			f.installation.InstallationID = "other"
		}},
		{"manifest digest moved", func(f *agentUX079FakeBindingFacts, _ *agentstore.Announcement) {
			f.profile.Manifest.Digest = "sha256:other"
		}},
		{"instructions moved", func(f *agentUX079FakeBindingFacts, _ *agentstore.Announcement) { f.profile.Instructions = "other" }},
		{"schema pin moved", func(f *agentUX079FakeBindingFacts, _ *agentstore.Announcement) { f.manifest.OutputSchema.ID = "other" }},
		{"audience incomplete", func(f *agentUX079FakeBindingFacts, _ *agentstore.Announcement) { f.snapshot.Complete = false }},
		{"deadline passed", func(f *agentUX079FakeBindingFacts, _ *agentstore.Announcement) {
			f.policy.Deadline = now.Add(-time.Minute)
		}},
		{"record outside the tenant", func(_ *agentUX079FakeBindingFacts, r *agentstore.Announcement) { r.TenantKey = "other-tenant" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutated, mutatedFacts, mutatedRecord, mutatedActor := agentUX079BirthdayTestBindings(now)
			tc.mutate(mutatedFacts, &mutatedRecord)
			if _, err := mutated.ResolveAnnouncementScheduleBinding(context.Background(), mutatedActor, mutatedRecord); err == nil {
				t.Fatal("misaligned facts were bound")
			}
		})
	}
	if _, err := (&AgentUX079BirthdayScheduleBindings{}).ResolveAnnouncementScheduleBinding(context.Background(), actor, record); err == nil {
		t.Fatal("bindings without facts were bound")
	}
}
