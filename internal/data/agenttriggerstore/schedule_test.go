package agenttriggerstore

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func physicalAgentDatabase(t *testing.T, admin *pgtest.DB) *pgtest.DB {
	t.Helper()
	ctx := context.Background()
	name := "agent_trigger_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.SQL.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(admin.URL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	q := parsed.Query()
	q.Set("search_path", "public")
	parsed.RawQuery = q.Encode()
	database, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	connection, err := pgxadapter.Connect(ctx, parsed.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = connection.Close(context.Background())
		_ = database.Close()
		if _, err := admin.SQL.ExecContext(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop test Agent database: %v", err)
		}
	})
	return &pgtest.DB{SQL: database, Conn: connection, Schema: "public", URL: parsed.String()}
}

const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type sourceAuthority struct{}

func (sourceAuthority) Authorize(_ context.Context, _ scheduled.Actor, _ scheduled.Action, s scheduled.Schedule) ([]schedule.AuthorizedTarget, error) {
	return []schedule.AuthorizedTarget{{AgentRun: s.Trigger.Definition.AgentRun}}, nil
}
func (sourceAuthority) CheckCurrent(context.Context, scheduled.Schedule) error { return nil }

type contextBuilder struct{}

func (contextBuilder) BuildScheduleContext(_ context.Context, s scheduled.Schedule, _ schedule.Occurrence) (agentrun.ContextScope, error) {
	return s.Context, nil
}

type currentAuthority struct{}

func (currentAuthority) VerifyAdmission(_ context.Context, r agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	return agentrun.AuthoritySnapshot{Agent: r.Agent, InstallationID: r.InstallationID, Principal: r.Principal, Audience: r.Audience, Context: r.Context, BudgetCeiling: r.Budget, GrantRef: "grant", PolicyDigest: digest}, nil
}
func scheduleFixture(t *testing.T) scheduled.Schedule {
	t.Helper()
	target := schedule.AgentRunTarget{Agent: schedule.AgentVersionRef{ID: "agent", Version: "1", Digest: digest}, SponsorID: "sponsor", Purpose: "digest", Budget: schedule.AgentRunBudget{MaxCostMicros: 100, MaxInputTokens: 100, MaxOutputTokens: 100}, Destination: schedule.AgentRunDestination{AudienceID: "audience", AudienceSnapshotID: "snapshot", AudienceDigest: digest}}
	def := schedule.TriggerDefinition{ID: "daily", Version: "1", TenantID: "tenant-a", TargetKind: schedule.TargetAgentRun, AgentRun: &target, InputTemplateDigest: digest, Purpose: "digest", Owner: "owner", Source: schedule.TriggerSource{Kind: schedule.SourceCron, Cron: schedule.CronSource{Expression: "0 9 * * *"}}, Overlap: schedule.OverlapSkip, Storm: schedule.StormPolicy{MaxFiringsPerWindow: 10, Window: 24 * time.Hour}, ExecutionMode: intent.ModeExecute, ExecutionEnvironment: intent.EnvironmentProduction}
	pub, err := schedule.Publish(schedule.NewRegistry(), def, []schedule.AuthorizedTarget{{AgentRun: &target}})
	if err != nil {
		t.Fatal(err)
	}
	return scheduled.Schedule{Trigger: pub, Revision: 1, State: scheduled.StateActive, OwnerID: "owner", InstallationID: "installation", AgentPrincipalID: "agent-principal", LegalEntity: "entity", Zone: values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"}, Misfire: schedule.MisfireConfig{Policy: schedule.MisfireCatchUpAll, MaxCatchUp: 10}, RunTimeout: time.Hour, Cursor: values.NewInstant(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)), DST: "BOTH", Context: agentrun.ContextScope{ID: "context", SnapshotID: "snapshot", Digest: digest}}
}
func TestTodo_AGENT_031_Integration(t *testing.T) {
	ctx := context.Background()
	admin := pgtest.NewEmpty(t)
	db := physicalAgentDatabase(t, admin)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenantA, tenantB := uuid.New(), uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES($1),($2)`, tenantA, tenantB); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()
	q.Set("search_path", db.Schema)
	parsed.RawQuery = q.Encode()
	ownerDB, err := agentstore.New(ctx, agentstore.Config{DSN: parsed.String(), CoreDSN: "postgres://core:unused@localhost/core", MaxConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer ownerDB.Close()
	subStore, err := New(ownerDB, tenantA, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	testSubscriptionStore(t, subStore)
	core := pgtest.New(t)
	var sourceName, agentName string
	if err := core.SQL.QueryRowContext(ctx, `SELECT current_database()`).Scan(&sourceName); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL.QueryRowContext(ctx, `SELECT current_database()`).Scan(&agentName); err != nil || sourceName == agentName {
		t.Fatalf("databases source=%s agent=%s err=%v", sourceName, agentName, err)
	}
	core.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,'source-a','cell-local','Source A','ACTIVE',CURRENT_TIMESTAMP),($2,'source-b','cell-local','Source B','ACTIVE',CURRENT_TIMESTAMP)`, tenantA, tenantB)
	sourceDB, err := pgxadapter.NewPool(ctx, core.URL, map[string]string{"search_path": core.Schema})
	if err != nil {
		t.Fatal(err)
	}
	defer sourceDB.Close()
	fenceDB, err := pgxadapter.NewPool(ctx, core.URL, map[string]string{"search_path": core.Schema})
	if err != nil {
		t.Fatal(err)
	}
	defer fenceDB.Close()
	sourceRunner := ScheduleSourceRunner{DB: sourceDB, FenceDB: fenceDB}
	executionReader := AgentScheduleExecutions{Database: ownerDB}
	store, err := NewScheduleSource(sourceRunner, tenantA, "tenant-a", executionReader)
	if err != nil {
		t.Fatal(err)
	}
	s := scheduleFixture(t)
	audit := scheduled.Audit{TenantID: "tenant-a", ScheduleID: "daily", ActorID: "reviewer", Revision: 1, Action: scheduled.ActionPublish, At: s.Cursor.Time()}
	if err := store.Save(ctx, s, 0, audit); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, s, 0, audit); !errors.Is(err, scheduled.ErrRevision) {
		t.Fatalf("duplicate init=%v", err)
	}
	loaded, found, err := store.Load(ctx, "tenant-a", "daily")
	if err != nil || !found || loaded.Trigger.Digest != s.Trigger.Digest || loaded.Cursor.Compare(s.Cursor) != 0 {
		t.Fatalf("reload=%+v found=%t err=%v", loaded, found, err)
	}
	list, err := store.List(ctx, "tenant-a")
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	owner, err := scheduled.NewOwner(store, sourceAuthority{})
	if err != nil {
		t.Fatal(err)
	}
	at := s.Cursor.Time().Add(24 * time.Hour)
	admission, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Store: agentrun.NewMemoryAdmissionStore(), Authority: currentAuthority{}, Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := scheduled.NewOutboxWorker(owner, store, contextBuilder{}, admission)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Plan(ctx, "tenant-a", "daily", at); err != nil {
		t.Fatal(err)
	}
	pending, err := store.Pending(ctx, "tenant-a", 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	delivery := pending[0]
	if delivery.Firing.Occurrence.ScheduledAt.Validate() != nil || delivery.Firing.Occurrence.ScheduledAt.Disambiguation() != values.DisambiguationRejectGap {
		t.Fatalf("lost DST evidence: %+v", delivery.Firing.Occurrence)
	}
	if err := worker.CheckRequest(ctx, delivery.Request); err != nil {
		t.Fatal(err)
	}
	tampered := delivery.Request
	tampered.Budget.MaxCostMicros++
	if err := worker.CheckRequest(ctx, tampered); !errors.Is(err, scheduled.ErrFiringRefused) {
		t.Fatalf("forged request=%v", err)
	}
	if key, err := worker.ResolveSourceKey(ctx, delivery.Request); err != nil || key != delivery.Key {
		t.Fatalf("restore key=%q err=%v", key, err)
	}
	if err := store.AppendWindow(ctx, s, values.NewInstant(at), pending); !errors.Is(err, scheduled.ErrRevision) {
		t.Fatalf("replayed cursor=%v", err)
	}
	if done, err := worker.Replay(ctx, "tenant-a", 10); err != nil || done != 1 {
		t.Fatalf("replay=%d err=%v", done, err)
	}
	receipt, found, err := store.LoadReceipt(ctx, "tenant-a", delivery.Key)
	if err != nil || !found || receipt.Decision != agentrun.DecisionAccepted {
		t.Fatalf("receipt=%+v found=%t err=%v", receipt, found, err)
	}
	if err := store.Acknowledge(ctx, "tenant-a", delivery.Key, receipt); err != nil {
		t.Fatal(err)
	}
	acknowledged,next,err:=store.AcknowledgedPage(ctx,"tenant-a","",1)
	if err!=nil || len(acknowledged)!=1 || acknowledged[0].Receipt!=receipt || next!=delivery.Key {t.Fatalf("ack page=%+v cursor=%q err=%v",acknowledged,next,err)}
	if after,cursor,err:=store.AcknowledgedPage(ctx,"tenant-a",next,1);err!=nil || len(after)!=0 || cursor!="" {t.Fatalf("duplicate ack page=%+v cursor=%q err=%v",after,cursor,err)}
	if _,_,err:=store.AcknowledgedPage(ctx,"tenant-b","",1);!errors.Is(err,scheduled.ErrAuthority) {t.Fatalf("cross tenant ack page=%v",err)}
	changed := receipt
	changed.RunRequestID = "different"
	if err := store.Acknowledge(ctx, "tenant-a", delivery.Key, changed); !errors.Is(err, scheduled.ErrReceiptConflict) {
		t.Fatalf("changed receipt=%v", err)
	}
	if pending, err := store.Pending(ctx, "tenant-a", 10); err != nil || len(pending) != 0 {
		t.Fatalf("ack pending=%d err=%v", len(pending), err)
	}
	restarted, err := NewScheduleSource(sourceRunner, tenantA, "tenant-a", executionReader)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := restarted.LoadReceipt(ctx, "tenant-a", delivery.Key); err != nil || !found {
		t.Fatalf("restart receipt found=%t err=%v", found, err)
	}
	if _, _, err := store.GetDelivery(ctx, "tenant-b", delivery.Key); !errors.Is(err, scheduled.ErrAuthority) {
		t.Fatalf("cross tenant delivery=%v", err)
	}
	if err := sourceRunner.RunTenantTx(ctx, tenantB, func(tx dbport.Tx) error {
		var count int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM agent_schedule_outbox`).Scan(&count)
		if err == nil && count != 0 {
			t.Fatalf("tenant B sees %d rows", count)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := sourceRunner.RunTenantTx(ctx, tenantA, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agent_schedule_outbox SET request_digest='changed'`)
		if err == nil {
			t.Fatal("immutable outbox accepted mutation")
		}
		if !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("mutation error=%v", err)
		}
		return err
	}); err == nil {
		t.Fatal("application role can update immutable outbox")
	}
	if _, err := core.SQL.ExecContext(ctx, `UPDATE agent_schedule_outbox SET request_digest='changed'`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("owner mutation=%v", err)
	}
	testScheduleExecutionFence(t, ownerDB, store, tenantA)
	var legacyRows int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM agent_schedule_outbox`).Scan(&legacyRows); err != nil || legacyRows != 0 {
		t.Fatalf("source outbox written to Agent DB rows=%d err=%v", legacyRows, err)
	}
}
