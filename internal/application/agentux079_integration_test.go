package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agenttriggerstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectboard"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentdemo"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_AGENTUX_079_Integration proves the ready-made agents' unfinished
// preparation across component boundaries: the desk files a ticket through
// the production target source and the production project service chain, and
// the birthday schedule converges paused-then-active on the real tables.
func TestTodo_AGENTUX_079_Integration(t *testing.T) {
	s15Run(t,
		s15Proof{"a stored email becomes a ticket through the served chain", TestAgentUX079_SupportDeskServedTicket_Integration},
		s15Proof{"the birthday schedule converges on the real tables", TestAgentUX079_BirthdayScheduleLive_Integration},
	)
}

// agentUX079ServedProjectFakes are the project database behind the served
// chain: the prepared project, its published support workflow, and the
// recorded tickets.
type agentUX079ServedProjectFakes struct {
	t        *testing.T
	tasks    map[string]projectservice.TaskRecord
	creates  int
	subjects []string
}

func (f *agentUX079ServedProjectFakes) CreateProject(_ context.Context, p projectservice.ProjectRecord, _, _ string) (projectservice.ProjectRecord, error) {
	return p, nil
}

func (f *agentUX079ServedProjectFakes) CreateTask(_ context.Context, t projectservice.TaskRecord, _ uint64, _ string, key string) (projectservice.TaskRecord, error) {
	if existing, ok := f.tasks[key]; ok {
		return existing, nil
	}
	if t.ProjectID != AgentUX079SupportProjectID {
		f.t.Fatalf("ticket filed in %q, want the Customer support project", t.ProjectID)
	}
	f.tasks[key] = t
	f.creates++
	return t, nil
}

func (f *agentUX079ServedProjectFakes) MoveTask(context.Context, string, string, string, string, uint64, uint64, []project.TaskFieldEdit, string, string) (projectservice.TaskRecord, error) {
	return projectservice.TaskRecord{}, errors.New("not implemented")
}

func (f *agentUX079ServedProjectFakes) GetProject(_ context.Context, tenant, id string) (projectservice.ProjectRecord, error) {
	if tenant != localAgentDemoTenant || id != AgentUX079SupportProjectID {
		return projectservice.ProjectRecord{}, errors.New("project not found")
	}
	return projectservice.ProjectRecord{ID: AgentUX079SupportProjectID, TenantID: tenant, OwnerID: localAgentDemoAdmin, Name: AgentUX079SupportProjectName}, nil
}

func (f *agentUX079ServedProjectFakes) GetTask(context.Context, string, string, string) (projectservice.TaskRecord, error) {
	return projectservice.TaskRecord{}, errors.New("not implemented")
}

func (f *agentUX079ServedProjectFakes) GetView(context.Context, string, string, string, string) (projectboard.BoardView, error) {
	return projectboard.BoardView{}, errors.New("not implemented")
}

func (f *agentUX079ServedProjectFakes) GetDraft(context.Context, string, string) (projectworkflow.Config, uint64, error) {
	return projectworkflow.Config{}, 0, errors.New("not implemented")
}

func (f *agentUX079ServedProjectFakes) SaveDraft(context.Context, string, string, string, projectworkflow.Config, uint64, string) (uint64, error) {
	return 0, errors.New("not implemented")
}

func (f *agentUX079ServedProjectFakes) PreviewDraft(context.Context, string, string, projectworkflow.Config, uint64, projectworkflow.MigrationMappings) (projectservice.WorkflowPreview, error) {
	return projectservice.WorkflowPreview{}, errors.New("not implemented")
}

func (f *agentUX079ServedProjectFakes) PublishDraft(context.Context, string, string, string, uint64, uint64, string, string, string, projectworkflow.MigrationMappings, projectservice.WorkflowReviewEvidence) (projectservice.WorkflowConfiguration, error) {
	return projectservice.WorkflowConfiguration{}, errors.New("not implemented")
}

func (f *agentUX079ServedProjectFakes) GetPublished(_ context.Context, tenant, id string) (projectservice.WorkflowConfiguration, error) {
	if tenant != localAgentDemoTenant || id != AgentUX079SupportProjectID {
		return projectservice.WorkflowConfiguration{}, errors.New("workflow not found")
	}
	return projectservice.WorkflowConfiguration{Version: 1, Config: AgentUXDemoSupportWorkflow("")}, nil
}

// agentUX079AllowHumanAuthorizer lets humans through; the served chain under
// test acts as the support service identity, never as a human.
type agentUX079AllowHumanAuthorizer struct{}

func (agentUX079AllowHumanAuthorizer) Authorize(context.Context, *trust.Principal, string, projectaccess.Capability) error {
	return nil
}

func (agentUX079AllowHumanAuthorizer) AuthorizeCreate(context.Context, *trust.Principal) error {
	return nil
}

func (agentUX079AllowHumanAuthorizer) AuthorizeListProjects(context.Context, *trust.Principal) error {
	return nil
}

// agentUX079DeskInbox is the agent database behind the served chain: the
// stored email, its plan and its effect reservations.
type agentUX079DeskInbox struct {
	inbox    map[string]agentstore.SupportInboxMessage
	plans    map[string]agentstore.SupportPlanReceipt
	reserved map[string]bool
	alerts   int
}

func (w *agentUX079DeskInbox) Receive(_ context.Context, r agentstore.SupportInboxMessage) (bool, error) {
	if _, ok := w.inbox[r.MessageID]; ok {
		return false, nil
	}
	w.inbox[r.MessageID] = r
	return true, nil
}

func (w *agentUX079DeskInbox) Get(_ context.Context, _ uuid.UUID, id string) (agentstore.SupportInboxMessage, error) {
	if r, ok := w.inbox[id]; ok {
		return r, nil
	}
	return agentstore.SupportInboxMessage{}, agentstore.ErrSupportInboxNotFound
}

func (w *agentUX079DeskInbox) WithSupportRunFence(_ context.Context, _ uuid.UUID, _ string, fn func() error) error {
	return fn()
}

func (w *agentUX079DeskInbox) GetSupportPlan(_ context.Context, _ uuid.UUID, id string) (agentstore.SupportPlanReceipt, error) {
	if p, ok := w.plans[id]; ok {
		return p, nil
	}
	return agentstore.SupportPlanReceipt{}, agentstore.ErrSupportInboxNotFound
}

func (w *agentUX079DeskInbox) RecordSupportPlan(_ context.Context, r agentstore.SupportPlanReceipt) error {
	w.plans[r.MessageID] = r
	return nil
}

func (w *agentUX079DeskInbox) ReserveSupportEffect(_ context.Context, _ uuid.UUID, message, skill string, _ time.Time, _ uint32) error {
	w.reserved[message+"\x00"+skill] = true
	return nil
}

func (w *agentUX079DeskInbox) PostSupportAlert(_ context.Context, tenant, conversation string, alert AgentUXDemoSupportAlert, _ string) (string, error) {
	if tenant != localAgentDemoTenant || conversation != localDevPersonaDemoConversationID(tenant, localAgentDemoSupportRoom) || !strings.HasPrefix(alert.TicketHref, "/workspace/app/project?project="+AgentUX079SupportProjectID) {
		return "", errors.New("alert outside the incident channel")
	}
	w.alerts++
	return "served-alert-post", nil
}

// TestAgentUX079_SupportDeskServedTicket_Integration files one stored email as
// a ticket through the production target source, the production project
// service with the support grant authorizer, and the product's planner: the
// Simulate path that answered 503 without a project. A replay files nothing
// twice, and a revoked grant is refused by the authorizer itself.
func TestAgentUX079_SupportDeskServedTicket_Integration(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	tenantID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("agentux079-served"))
	mapper := func(values.TenantId) uuid.UUID { return tenantID }
	grants := &agentUX079FakeGrantLedger{allowed: map[string]bool{AgentUX079SupportServiceSubject + "\x00" + AgentUX079SupportProjectID: true}}
	targets := AgentUX079SupportTargets{
		TenantUUID: mapper,
		Projects:   agentUX079FakeProjectResolver{info: AgentUX079SupportProjectInfo{ProjectID: AgentUX079SupportProjectID, InitialStatusID: "support_new", WorkflowRevision: 1}},
		Grants:     grants,
		Now:        func() time.Time { return now },
	}
	stores := &agentUX079ServedProjectFakes{t: t, tasks: map[string]projectservice.TaskRecord{}}
	service := projectservice.Service{
		Auth:      AgentUXDemoSupportProjectAuthorizer{Human: agentUX079AllowHumanAuthorizer{}, Grants: grants, TenantUUID: mapper},
		Commands:  stores,
		Reads:     stores,
		Workflows: stores,
	}
	world := &agentUX079DeskInbox{inbox: map[string]agentstore.SupportInboxMessage{}, plans: map[string]agentstore.SupportPlanReceipt{}, reserved: map[string]bool{}}
	objects := AgentUXDemoLocalSupportObjects{Root: t.TempDir()}
	desk := &AgentUXDemoSupportDesk{Inbox: world, Journal: world, Objects: objects, Planner: AgentUXDemoLocalSupportPlanner{}, Projects: service, Alerts: world, Targets: targets, Now: func() time.Time { return now }}

	email := agentdemo.Email{From: "Ana Customer <ana@example.test>", Subject: "My delivery has not arrived", Body: "The delivery expected yesterday has not arrived. Could you check its status?", IdempotencyKey: "stored-support-email"}
	raw, err := json.Marshal(struct{ From, Subject, Body string }{email.From, email.Subject, email.Body})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := objects.PutSupportEmail(ctx, localAgentDemoTenant, "support-email:served-1", raw)
	if err != nil {
		t.Fatal(err)
	}
	id := "support-email:served-1"
	if _, err := world.Receive(ctx, agentstore.SupportInboxMessage{TenantID: tenantID, MessageID: id, ContentRef: ref, ContentDigest: personaRunBytesDigest(raw), ClaimedSenderName: "Ana Customer", ClaimedSenderAddress: "ana@example.test", ReceivedAt: now}); err != nil {
		t.Fatal(err)
	}
	result, err := desk.Process(ctx, localAgentDemoTenant, id)
	if err != nil {
		t.Fatal(err)
	}
	if result.TicketID == "" || result.AlertPost == "" || result.Severity == "" || result.NeedsHumanReview {
		t.Fatalf("result = %+v, want a filed ticket and its alert", result)
	}
	stored, ok := stores.tasks["support.create_ticket:"+id]
	if !ok || stored.ID != result.TicketID || stored.Title == "" || stored.Priority != "NORMAL" {
		t.Fatalf("ticket = %+v, want the filed ticket with normal priority", stored)
	}
	replay, err := desk.Process(ctx, localAgentDemoTenant, id)
	if err != nil {
		t.Fatal(err)
	}
	if replay.TicketID != result.TicketID || stores.creates != 1 {
		t.Fatalf("replay = %+v, creates = %d, want the same ticket filed once", replay, stores.creates)
	}

	revokedID := "support-email:served-2"
	revokedRef, err := objects.PutSupportEmail(ctx, localAgentDemoTenant, revokedID, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := world.Receive(ctx, agentstore.SupportInboxMessage{TenantID: tenantID, MessageID: revokedID, ContentRef: revokedRef, ContentDigest: personaRunBytesDigest(raw), ClaimedSenderName: "Ana Customer", ClaimedSenderAddress: "ana@example.test", ReceivedAt: now}); err != nil {
		t.Fatal(err)
	}
	revokedService := service
	revokedService.Auth = AgentUXDemoSupportProjectAuthorizer{Human: agentUX079AllowHumanAuthorizer{}, Grants: &agentUX079FakeGrantLedger{allowed: map[string]bool{}}, TenantUUID: mapper}
	revokedDesk := &AgentUXDemoSupportDesk{Inbox: world, Journal: world, Objects: objects, Planner: AgentUXDemoLocalSupportPlanner{}, Projects: revokedService, Alerts: world, Targets: targets, Now: func() time.Time { return now }}
	// The targets still resolve (their ledger allows); the ticket service
	// refuses on its own ledger read.
	if _, err := revokedDesk.Process(ctx, localAgentDemoTenant, revokedID); err == nil {
		t.Fatal("revoked grant was not refused by the service authorizer")
	}
	if stores.creates != 1 {
		t.Fatalf("creates = %d, want no ticket filed after the grant was revoked", stores.creates)
	}
}

// agentUX079LiveScheduleBindings answers the one executable schedule the
// birthday step files, with the same shape the preparation bindings
// assemble; the announcement table and the native substrate are real.
type agentUX079LiveScheduleBindings struct {
	now time.Time
}

func (b agentUX079LiveScheduleBindings) ResolveAnnouncementScheduleBinding(_ context.Context, actor AgentAnnouncementActor, record agentstore.Announcement) (scheduled.Schedule, error) {
	digest := "sha256:" + strings.Repeat("ab", 32)
	target := &schedule.AgentRunTarget{Agent: schedule.AgentVersionRef{ID: "birthday-manifest", Version: "7", Digest: digest}, SponsorID: "birthday-service", Purpose: personaChatReplyPurpose,
		Budget:      schedule.AgentRunBudget{MaxCostMicros: 1000, MaxInputTokens: 1000, MaxOutputTokens: 1000},
		Destination: schedule.AgentRunDestination{AudienceID: record.ConversationID, AudienceSnapshotID: "9", AudienceDigest: digest}}
	return scheduled.Schedule{SourceKind: agentrun.SourceAnnouncement, Persona: &agentrun.PersonaRef{ID: record.PersonaID, Version: "3", Digest: digest}, RequesterID: actor.SubjectID, OwnerID: actor.SubjectID,
		InstallationID: record.InstallationID, AgentPrincipalID: "birthday-principal", LegalEntity: "birthday-legal", Zone: values.ZoneRef{ID: record.Zone, TzdbVersion: "2026a"}, RunTimeout: time.Hour,
		Trigger: schedule.PublishedTrigger{Definition: schedule.TriggerDefinition{TenantID: actor.TenantID, TargetKind: schedule.TargetAgentRun, AgentRun: target, Purpose: personaChatReplyPurpose,
			Storm: schedule.StormPolicy{MaxFiringsPerWindow: 400, Window: 366 * 24 * time.Hour}, ExecutionMode: intent.ModeExecute, ExecutionEnvironment: intent.EnvironmentProduction}}}, nil
}

// agentUX079LiveTriggerStore is the native schedule substrate over the live
// agent database.
type agentUX079LiveTriggerStore struct {
	agents     *agentstore.Store
	tenantUUID func(values.TenantId) uuid.UUID
}

func (s agentUX079LiveTriggerStore) source(tenant string) (*agenttriggerstore.Store, error) {
	return agenttriggerstore.NewAnnouncementSource(s.agents, s.tenantUUID(values.TenantId(tenant)), tenant, agenttriggerstore.AgentScheduleExecutions{Database: s.agents})
}

func (s agentUX079LiveTriggerStore) Load(ctx context.Context, tenant, id string) (scheduled.Schedule, bool, error) {
	store, err := s.source(tenant)
	if err != nil {
		return scheduled.Schedule{}, false, err
	}
	return store.Load(ctx, tenant, id)
}

func (s agentUX079LiveTriggerStore) Save(ctx context.Context, record scheduled.Schedule, expected uint64, audit scheduled.Audit) error {
	store, err := s.source(record.Trigger.Definition.TenantID)
	if err != nil {
		return err
	}
	return store.Save(ctx, record, expected, audit)
}

// TestAgentUX079_BirthdayScheduleLive_Integration files the birthday schedule
// paused and resumes it on the real announcement and trigger tables: the
// record passes store validation, the native entry converges paused then
// active, and a rerun is a no-op.
func TestAgentUX079_BirthdayScheduleLive_Integration(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	agents, err := agentstore.New(ctx, agentstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema), CoreDSN: "postgres://unused@127.0.0.1:1/unused", MaxConns: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer agents.Close()
	tenantID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("agentux079-birthday-live"))
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenantID)
	mapper := func(values.TenantId) uuid.UUID { return tenantID }
	announcements, err := agentstore.NewAnnouncementStore(agents)
	if err != nil {
		t.Fatal(err)
	}
	deps := func(routeExists bool) AgentUX079BirthdayScheduleDeps {
		return AgentUX079BirthdayScheduleDeps{
			Announcements: announcements,
			Schedules:     AgentAnnouncementNativeSchedules{Store: agentUX079LiveTriggerStore{agents: agents, tenantUUID: mapper}, Bindings: agentUX079LiveScheduleBindings{now: now}, Now: func() time.Time { return now }},
			TenantUUID:    mapper, Tenant: localAgentDemoTenant, Admin: localAgentDemoAdmin,
			InstallationID: "birthday-installation", ConversationID: "general-conversation",
			RouteExists: routeExists, Now: func() time.Time { return now },
		}
	}
	paused, err := ensureAgentUX079BirthdaySchedule(ctx, deps(false))
	if err != nil {
		t.Fatal(err)
	}
	if !paused.Checked || !paused.Created || paused.Resumed {
		t.Fatalf("filed = %+v, want checked and created but not resumed", paused)
	}
	stored, err := announcements.Get(ctx, tenantID, AgentUX079BirthdayScheduleID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != agentstore.AnnouncementPaused || stored.Revision != 1 || stored.NextRunAt != nil || stored.SchedulerID != "announcement:"+AgentUX079BirthdayScheduleID {
		t.Fatalf("record = %+v, want a paused revision 1", stored)
	}
	native, found, err := agentUX079LiveTriggerStore{agents: agents, tenantUUID: mapper}.Load(ctx, localAgentDemoTenant, stored.SchedulerID)
	if err != nil || !found || native.State != scheduled.StatePaused {
		t.Fatalf("native = %+v, found = %t, err = %v, want a paused entry", native, found, err)
	}
	resumed, err := ensureAgentUX079BirthdaySchedule(ctx, deps(true))
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.Checked || resumed.Created || !resumed.Resumed {
		t.Fatalf("resumed = %+v, want checked and resumed without creating", resumed)
	}
	after, err := announcements.Get(ctx, tenantID, AgentUX079BirthdayScheduleID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != agentstore.AnnouncementActive || after.Revision != 2 || after.NextRunAt == nil {
		t.Fatalf("record = %+v, want an active revision 2 with a next run", after)
	}
	active, found, err := agentUX079LiveTriggerStore{agents: agents, tenantUUID: mapper}.Load(ctx, localAgentDemoTenant, stored.SchedulerID)
	if err != nil || !found || active.State != scheduled.StateActive {
		t.Fatalf("native = %+v, found = %t, err = %v, want an active entry", active, found, err)
	}
	idle, err := ensureAgentUX079BirthdaySchedule(ctx, deps(true))
	if err != nil {
		t.Fatal(err)
	}
	if !idle.Checked || idle.Created || idle.Resumed {
		t.Fatalf("rerun = %+v, want checked with no change", idle)
	}
}
