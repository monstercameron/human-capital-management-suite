package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type agentUXAmbientScheduleMemory struct {
	records    map[string]scheduled.Schedule
	deliveries map[string]scheduled.Delivery
	receipts   map[string]scheduled.Receipt
	failAck    bool
}

func agentUXAmbientScheduleMemoryNew() *agentUXAmbientScheduleMemory {
	return &agentUXAmbientScheduleMemory{records: map[string]scheduled.Schedule{}, deliveries: map[string]scheduled.Delivery{}, receipts: map[string]scheduled.Receipt{}}
}
func (m *agentUXAmbientScheduleMemory) Load(_ context.Context, tenant, id string) (scheduled.Schedule, bool, error) {
	s, ok := m.records[id]
	return s, ok && s.Trigger.Definition.TenantID == tenant, nil
}
func (m *agentUXAmbientScheduleMemory) Save(_ context.Context, s scheduled.Schedule, expected uint64, _ scheduled.Audit) error {
	old, ok := m.records[s.Trigger.Definition.ID]
	if ok && old.Revision != expected {
		return scheduled.ErrRevision
	}
	m.records[s.Trigger.Definition.ID] = s
	return nil
}
func (m *agentUXAmbientScheduleMemory) AppendWindow(_ context.Context, s scheduled.Schedule, end values.Instant, deliveries []scheduled.Delivery) error {
	old := m.records[s.Trigger.Definition.ID]
	if old.Revision != s.Revision || old.Cursor != s.Cursor {
		return scheduled.ErrRevision
	}
	for _, d := range deliveries {
		m.deliveries[d.Key] = d
	}
	old.Cursor = end
	m.records[s.Trigger.Definition.ID] = old
	return nil
}
func (m *agentUXAmbientScheduleMemory) Pending(_ context.Context, tenant string, _ int) ([]scheduled.Delivery, error) {
	var result []scheduled.Delivery
	for key, d := range m.deliveries {
		if _, ok := m.receipts[key]; !ok && d.TenantID == tenant {
			result = append(result, d)
		}
	}
	return result, nil
}
func (m *agentUXAmbientScheduleMemory) GetDelivery(_ context.Context, tenant, key string) (scheduled.Delivery, bool, error) {
	d, ok := m.deliveries[key]
	return d, ok && d.TenantID == tenant, nil
}
func (m *agentUXAmbientScheduleMemory) Acknowledge(_ context.Context, tenant, key string, r scheduled.Receipt) error {
	if m.failAck {
		m.failAck = false
		return errors.New("receipt unavailable")
	}
	m.receipts[key] = r
	return nil
}
func (m *agentUXAmbientScheduleMemory) LoadReceipt(_ context.Context, tenant, key string) (scheduled.Receipt, bool, error) {
	r, ok := m.receipts[key]
	return r, ok, nil
}

type agentUXAmbientScheduleBinding struct{}

func (agentUXAmbientScheduleBinding) ResolveAmbientAnnouncement(_ context.Context, o AgentUXAmbientOffer) (scheduled.Schedule, error) {
	digest := "sha256:" + strings.Repeat("a", 64)
	audience := o.Conversation
	if o.Scope == "PRIVATE" {
		audience = "dm-" + o.Person
	}
	target := &schedule.AgentRunTarget{Agent: schedule.AgentVersionRef{ID: o.Agent, Version: "1", Digest: digest}, SponsorID: "agent-service", Purpose: "ambient-reminder", Budget: schedule.AgentRunBudget{MaxCostMicros: 100, MaxInputTokens: 100, MaxOutputTokens: 100}, Destination: schedule.AgentRunDestination{AudienceID: audience, AudienceSnapshotID: "members-v1", AudienceDigest: digest}}
	return scheduled.Schedule{Persona: &agentrun.PersonaRef{ID: o.Agent, Version: "1", Digest: digest}, OwnerID: "author", InstallationID: "tenant-a:general:reminder", AgentPrincipalID: "agent-service", LegalEntity: "company", RunTimeout: time.Minute, Trigger: schedule.PublishedTrigger{Definition: schedule.TriggerDefinition{TenantID: o.Tenant, TargetKind: schedule.TargetAgentRun, AgentRun: target, Purpose: "ambient-reminder", Storm: schedule.StormPolicy{MaxFiringsPerWindow: 10, Window: 366 * 24 * time.Hour}, ExecutionMode: intent.ModeExecute, ExecutionEnvironment: intent.EnvironmentProduction}}}, nil
}

type agentUXAmbientAdmissionAuthority struct{}

func (agentUXAmbientAdmissionAuthority) VerifyAdmission(_ context.Context, r agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	return agentrun.AuthoritySnapshot{Agent: r.Agent, InstallationID: r.InstallationID, Principal: r.Principal, Audience: r.Audience, Context: r.Context, BudgetCeiling: r.Budget, GrantRef: "ambient-installation", PolicyDigest: "sha256:" + strings.Repeat("b", 64)}, nil
}

func TestAgentUXAmbient_SharedSchedulerAndDelivery_Fault_Integration(t *testing.T) {
	s, _, _ := agentUXAmbientFixture(t)
	ctx := t.Context()
	start := s.Now()
	now := start
	s.Now = func() time.Time { return now }
	agentUXAmbientPost(t, s, "private-reminder", "author", "Remind me tomorrow at 9 am to call the vendor")
	store := agentUXAmbientScheduleMemoryNew()
	calls := map[string]int{}
	notifications := map[string]int{}
	delivery := AgentUXAmbientDeliveryAdapter{Service: s, SourceLink: func(context.Context, string, string, string) (string, error) {
		return "/workspace/app/chat?post=private-reminder", nil
	}, PublicMessage: func(context.Context, AgentUXAmbientOffer, string, string) (string, error) {
		t.Fatal("private reminder posted publicly")
		return "", nil
	}, PrivateAgentDM: func(ctx context.Context, o AgentUXAmbientOffer, text, key string) (string, error) {
		if o.Person != "author" || !strings.Contains(text, "Remind me tomorrow") || !strings.Contains(text, "/workspace/app/chat?") {
			t.Fatal("recipient or source changed")
		}
		tx, ok := dbport.TxFromContext(ctx)
		if !ok {
			t.Fatal("delivery lost transaction fence")
		}
		// The sink writes the real message to PostgreSQL under its frozen key.
		var id string
		err := tx.QueryRow(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,client_key) VALUES($1,'tenant-a','direct','reminder','tenant-a',(SELECT COALESCE(max(sequence),0)+1 FROM chat_post WHERE tenant_id='tenant-a' AND conversation_id='direct'),$2,$3) ON CONFLICT(id) DO UPDATE SET id=EXCLUDED.id RETURNING id`, agentUXAmbientID(key), text, key).Scan(&id)
		if err == nil {
			calls[key] = 1
		}
		return id, err
	}, NotifyPerson: func(ctx context.Context, tenant, person, id, key string) error {
		if tenant != "tenant-a" || person != "author" || id == "" {
			t.Fatal("notification changed recipient")
		}
		notifications[key] = 1
		return nil
	}}
	admission, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: agentUXAmbientAdmissionAuthority{}, Store: agentrun.NewMemoryAdmissionStore(), Now: s.Now})
	if err != nil {
		t.Fatal(err)
	}
	announcements := AgentUXAmbientAnnouncements{Service: s, Store: store, Outbox: store, Bindings: agentUXAmbientScheduleBinding{}, Delivery: delivery, Admissions: admission}
	s.Effects = AgentUXAmbientEffectsAdapter{Chat: s.DB.(*chatstore.Store), Announcements: announcements, TodoAuthority: func(context.Context, string, string, string) error { return nil }}
	if err = s.ProcessMessage(ctx, "tenant-a", "general", "private-reminder", "reminder", "America/New_York"); err != nil {
		t.Fatal(err)
	}
	offers, err := s.ListOffers(ctx, "tenant-a", "general", "author")
	if err != nil || len(offers) != 1 {
		t.Fatalf("offers %v %v", offers, err)
	}
	o, err := s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: offers[0].ID, Action: "SET", ExpectedRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	id := agentUXAmbientScheduleID(o, o.Time.Fire[0])
	native := store.records[id]
	if native.SourceKind != agentrun.SourceAnnouncement || native.Zone.ID != "UTC" || native.Context.Digest != agentUXAmbientOfferDigest(o) {
		t.Fatalf("not message-sourced native schedule %+v", native)
	}
	previousID := id
	o, err = s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: o.ID, Action: "SNOOZE", ExpectedRevision: o.Revision, Date: "2026-10-02", Clock: "10:00"})
	if err != nil || store.records[previousID].State != scheduled.StateRetired {
		t.Fatalf("changed time failed to cancel original occurrence: %v state=%s", err, store.records[previousID].State)
	}
	id = agentUXAmbientScheduleID(o, o.Time.Fire[0])
	now = o.Time.Fire[0].Add(time.Minute)
	store.deliveries["other-source"] = scheduled.Delivery{TenantID: "tenant-a", Key: "other-source", ScheduleID: "unrelated-announcement"}
	invalid := store.records[id]
	invalid.Trigger.Definition.ID = "revoked-schedule"
	invalid.Context.ID = "unavailable-source"
	store.records["revoked-schedule"] = invalid
	store.deliveries["revoked-pending"] = scheduled.Delivery{TenantID: "tenant-a", Key: "revoked-pending", ScheduleID: "revoked-schedule"}
	store.failAck = true
	if err = announcements.Tick(ctx, "tenant-a", []string{id}); err == nil {
		t.Fatal("receipt failure not propagated")
	}
	if err = announcements.Tick(ctx, "tenant-a", []string{"revoked-schedule", previousID, id}); err != nil {
		t.Fatal(err)
	}
	if _, claimed := store.receipts["other-source"]; claimed {
		t.Fatal("ambient workload claimed another announcement's outbox")
	}
	if _, claimed := store.receipts["revoked-pending"]; claimed {
		t.Fatal("unavailable source received a forged receipt")
	}
	if err = announcements.Tick(ctx, "tenant-a", []string{id}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM chat_post WHERE tenant_id='tenant-a' AND conversation_id='direct' AND author_id='reminder'`).Scan(&count)
	}); err != nil || count != 1 || len(calls) != 1 || len(notifications) != 1 || store.records[id].State != scheduled.StateRetired {
		t.Fatalf("delivery duplicated/skipped: posts=%d calls=%v notifications=%v state=%s err=%v", count, calls, notifications, store.records[id].State, err)
	}
	if err = announcements.CancelAmbientMessageAnnouncement(ctx, o); err != nil {
		t.Fatal(err)
	}
}

func TestAgentUXAmbient_DeliveryMembership_Security_Integration(t *testing.T) {
	s, _, _ := agentUXAmbientFixture(t)
	ctx := t.Context()
	agentUXAmbientPost(t, s, "deadline-security", "author", "Timesheets are due Friday at 5 pm")
	if err := s.ProcessMessage(ctx, "tenant-a", "general", "deadline-security", "reminder", "UTC"); err != nil {
		t.Fatal(err)
	}
	offers, err := s.ListOffers(ctx, "tenant-a", "general", "author")
	if err != nil {
		t.Fatal(err)
	}
	o, err := s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: offers[0].ID, Action: "SET", ExpectedRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	now := o.Time.Fire[0]
	s.Now = func() time.Time { return now }
	public, private := 0, 0
	a := AgentUXAmbientDeliveryAdapter{Service: s, SourceLink: func(context.Context, string, string, string) (string, error) {
		return "/workspace/app/chat?post=deadline-security", nil
	}, PublicMessage: func(context.Context, AgentUXAmbientOffer, string, string) (string, error) {
		public++
		return "public-post", nil
	}, PrivateAgentDM: func(context.Context, AgentUXAmbientOffer, string, string) (string, error) {
		private++
		return "private-post", nil
	}, NotifyPerson: func(context.Context, string, string, string, string) error { return nil }}
	if _, err = a.DeliverAmbientAnnouncement(ctx, o, "public-key", now); err != nil || public != 1 || private != 0 {
		t.Fatalf("public route %d/%d %v", public, private, err)
	}
	if err = s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_membership SET history_visibility='FROM_JOIN',joined_at=$1 WHERE tenant_id='tenant-a' AND conversation_id='general' AND member_id='peer'`, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = a.DeliverAmbientAnnouncement(ctx, o, "new-member-key", now); !errors.Is(err, ErrAgentUXAmbientDenied) || public != 1 {
		t.Fatalf("unreadable source posted: %v", err)
	}
	o.Scope = "PRIVATE"
	o.Person = "peer"
	if _, err = a.DeliverAmbientAnnouncement(ctx, o, "private-key", now); !errors.Is(err, ErrAgentUXAmbientDenied) || private != 0 {
		t.Fatalf("recipient lacks source but got reminder: %v", err)
	}
}
