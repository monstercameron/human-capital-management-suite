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
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agenttriggerstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func scheduleHumanContext(t *testing.T, actor string, at time.Time) context.Context {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("common-tenant"), Subject: actor, SubjectKind: trust.SubjectKindHuman, Assurance: trust.AssuranceLow, AuthenticationMethod: trust.AuthenticationMethodBearerToken, SessionRef: "session", IssuedAt: at.Add(-time.Hour), ExpiresAt: at.Add(24 * time.Hour), CredentialDigest: "verified-credential"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), p)
}
func TestTodo_AGENT_030_ServedIntegration(t *testing.T) {
	ctx := context.Background()
	core := pgtest.New(t)
	agentsDB := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, agentsDB.SQL); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	core.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,'common-tenant','cell-local','Common','ACTIVE',CURRENT_TIMESTAMP)`, tenant)
	agentsDB.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenant)
	// The shared admission authority refuses every agent request for a tenant
	// whose administrator has not turned agents on.
	core.Exec(t, `INSERT INTO tenant_agent_setting(tenant_id,enabled,revision,updated_by) VALUES($1,true,1,'test-admin')`, tenant)
	agents := commonAgentOpenIntegrationStore(t, agentsDB)
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	r := commonAgentTestRequest(now)
	r.Principal.AgentPrincipalID = uuid.NewString()
	r.Principal.SponsorID = uuid.NewString()
	r.Deadline = now.Add(24 * time.Hour)
	manifest := commonAgentIntegrationManifest(t, agents, tenant, r)
	r.Agent.Digest, _ = manifest.Digest()
	source := uuid.New()
	core.Exec(t, `INSERT INTO authority_source(tenant_id,authority_source_id,kind,display_name,uri,valid_interval,content_digest) VALUES($1,$2,'POLICY_BUNDLE','Reviewed agent schedule','policy:reviewed-schedule',tstzrange($3,$4,'[)'),$5)`, tenant, source, now.Add(-time.Hour), now.Add(24*time.Hour), strings.TrimPrefix(r.Agent.Digest, "sha256:"))
	binding := CommonAgentBindingScope{SchemaVersion: 1, TenantID: "common-tenant", Agent: r.Agent, InstallationID: r.InstallationID, AgentPrincipal: r.Principal.AgentPrincipalID, SponsorID: r.Principal.SponsorID, LegalEntity: r.LegalEntity, Purpose: r.Purpose, Audience: r.Audience, Context: r.Context, Sources: []agentrun.SourceKind{agentrun.SourceSchedule}, BudgetCeiling: r.Budget, Deadline: r.Deadline}
	raw, _ := json.Marshal(binding)
	for _, principal := range []string{r.Principal.AgentPrincipalID, r.Principal.SponsorID} {
		core.Exec(t, `INSERT INTO principal(tenant_id,principal_id,kind,subject,assurance,authn_method) VALUES($1,$2,'SERVICE',$3,'AAL1','workload')`, tenant, principal, "service:"+principal)
		core.Exec(t, `INSERT INTO authority_binding(tenant_id,binding_id,principal_id,authority_source_id,scope,valid_from,valid_to) VALUES($1,$2,$3,$4,$5::jsonb,$6,$7)`, tenant, uuid.New(), principal, source, string(raw), now.Add(-time.Hour), now.Add(24*time.Hour))
	}
	target := schedule.AgentRunTarget{Agent: schedule.AgentVersionRef{ID: r.Agent.AgentID, Version: r.Agent.Version, Digest: r.Agent.Digest}, SponsorID: r.Principal.SponsorID, Purpose: r.Purpose, Budget: schedule.AgentRunBudget{MaxCostMicros: r.Budget.MaxCostMicros, MaxInputTokens: r.Budget.MaxInputTokens, MaxOutputTokens: r.Budget.MaxOutputTokens}, Destination: schedule.AgentRunDestination{AudienceID: r.Audience.ID, AudienceSnapshotID: r.Audience.SnapshotID, AudienceDigest: r.Audience.Digest}}
	def := schedule.TriggerDefinition{ID: "digest", Version: "1", TenantID: "common-tenant", TargetKind: schedule.TargetAgentRun, AgentRun: &target, InputTemplateDigest: r.Context.Digest, Purpose: r.Purpose, Owner: "owner", Source: schedule.TriggerSource{Kind: schedule.SourceCron, Cron: schedule.CronSource{Expression: "*/5 * * * *"}}, Overlap: schedule.OverlapSkip, Storm: schedule.StormPolicy{MaxFiringsPerWindow: 12, Window: time.Hour}, ExecutionMode: intent.ModeExecute, ExecutionEnvironment: intent.EnvironmentProduction}
	publication, err := schedule.Publish(schedule.NewRegistry(), def, []schedule.AuthorizedTarget{{AgentRun: &target}})
	if err != nil {
		t.Fatal(err)
	}
	reference := scheduled.Schedule{Trigger: publication, State: scheduled.StateDraft, OwnerID: "owner", InstallationID: r.InstallationID, AgentPrincipalID: r.Principal.AgentPrincipalID, LegalEntity: r.LegalEntity, Zone: values.ZoneRef{ID: "UTC", TzdbVersion: "2026a"}, Misfire: schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce}, RunTimeout: time.Minute, Context: r.Context, DST: "REJECT"}
	management := AgentScheduleManagementScope{SchemaVersion: 1, TenantID: "common-tenant", ScheduleID: "digest", Actions: []string{"DRAFT", "PREVIEW", "PUBLISH", "PAUSE", "RESUME", "SKIP", "DRY_RUN", "RETIRE"}, Schedule: reference, CronExpressions: []string{"*/5 * * * *"}, MisfirePolicies: []schedule.MisfirePolicy{schedule.MisfireCatchUpOnce}, OverlapPolicies: []schedule.OverlapPolicy{schedule.OverlapSkip}, DSTPolicies: []string{"REJECT"}}
	managementRaw, err := json.Marshal(management)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"owner", "reviewer"} {
		principal := uuid.New()
		core.Exec(t, `INSERT INTO principal(tenant_id,principal_id,kind,subject,assurance,authn_method) VALUES($1,$2,'USER',$3,'AAL1','password')`, tenant, principal, user)
		core.Exec(t, `INSERT INTO authority_binding(tenant_id,binding_id,principal_id,authority_source_id,scope,valid_from,valid_to) VALUES($1,$2,$3,$4,$5::jsonb,$6,$7)`, tenant, uuid.New(), principal, source, string(managementRaw), now.Add(-time.Hour), now.Add(24*time.Hour))
	}
	connection := core.NewConn(t)
	if _, err := connection.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	mapper := func(ref values.TenantId) uuid.UUID {
		if ref == "common-tenant" {
			return tenant
		}
		return uuid.Nil
	}
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
	service, err := NewAgentScheduleService(AgentScheduleServiceConfig{CoreDB: connection, SourceDB: sourceDB, SourceFenceDB: fenceDB, Agents: agents, TenantUUID: mapper, Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := NewCommonAgentAuthority(CommonAgentAuthorityConfig{CoreDB: connection, Agents: agents, TenantUUID: mapper, Sources: map[agentrun.SourceKind]CommonAgentSourceAuthority{agentrun.SourceSchedule: service}, Models: &commonAgentTestModelPolicy{}, Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewCommonAgentRuntime(CommonAgentRuntimeConfig{Stores: DatabaseCommonAgentStores{Agents: agents, TenantUUID: mapper, SourceKeys: service}, Authority: authority, Now: clock, ExecutionFences: map[agentrun.SourceKind]CommonAgentExecutionFence{agentrun.SourceSchedule: service}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.BindCommon(runtime, authority); err != nil {
		t.Fatal(err)
	}
	ownerCtx := scheduleHumanContext(t, "owner", now)
	reviewCtx := scheduleHumanContext(t, "reviewer", now)
	input := productui.AgentScheduleDraft{ID: "digest", Version: "1", Installation: r.InstallationID, Recurrence: "*/5 * * * *", Zone: "UTC", Destination: r.Audience.ID, Budget: `{"MaxCostMicros":100,"MaxInputTokens":200,"MaxOutputTokens":100}`, Misfire: "CATCH_UP_ONCE", Overlap: "SKIP", DST: "REJECT"}
	if _, err := service.Preview(ownerCtx, input); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Draft(ownerCtx, input); err != nil {
		t.Fatal(err)
	}
	command := productui.AgentControlsCommand{Kind: "schedule", ID: "digest", ExpectedRevision: 1, Action: "publish", IdempotencyKey: "publish-1", Reason: "reviewed"}
	if _, err := service.Control(ownerCtx, command); !errors.Is(err, agentcontrols.ErrDenied) {
		t.Fatalf("self publication=%v", err)
	}
	if _, err := service.Control(reviewCtx, command); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if err := service.TickTenant(ctx, "common-tenant"); err != nil {
		t.Fatal(err)
	}
	store, err := agenttriggerstore.NewScheduleSource(agenttriggerstore.ScheduleSourceRunner{DB: sourceDB, FenceDB: fenceDB}, tenant, "common-tenant", agenttriggerstore.AgentScheduleExecutions{Database: agents})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := core.SQL.QueryRowContext(ctx, `SELECT count(*) FROM agent_schedule_receipt WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 1 {
		t.Fatalf("actual native receipts=%d err=%v", count, err)
	}
	var key string
	if err := core.SQL.QueryRowContext(ctx, `SELECT source_key FROM agent_schedule_receipt WHERE tenant_id=$1`, tenant).Scan(&key); err != nil {
		t.Fatal(err)
	}
	receipt, found, err := store.LoadReceipt(ctx, "common-tenant", key)
	if err != nil || !found || receipt.Decision != agentrun.DecisionAccepted {
		t.Fatalf("receipt=%+v found=%t err=%v", receipt, found, err)
	}
	if _, err := runtime.GetRun(ctx, "common-tenant", receipt.RunRequestID); err != nil {
		t.Fatal(err)
	}
	command.ExpectedRevision = 2
	command.Action = "pause"
	command.IdempotencyKey = "pause-1"
	if _, err := service.Control(ownerCtx, command); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Recheck(ctx, "common-tenant", receipt.RunRequestID); err == nil {
		t.Fatal("paused schedule accepted worker claim")
	}
	command.ExpectedRevision = 3
	command.Action = "resume"
	command.IdempotencyKey = "resume-1"
	if _, err := service.Control(ownerCtx, command); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Recheck(ctx, "common-tenant", receipt.RunRequestID); err != nil {
		t.Fatal(err)
	}
	claimed, err := runtime.Claim(ctx, "common-tenant", receipt.RunRequestID, "worker", 30*time.Second)
	if err != nil || claimed.State != runstate.StateRunning {
		t.Fatalf("actual fenced claim=%+v err=%v", claimed, err)
	}
	now = now.Add(5 * time.Minute)
	if err := service.TickTenant(ctx, "common-tenant"); err != nil {
		t.Fatal(err)
	}
	var secondID string
	if err := core.SQL.QueryRowContext(ctx, `SELECT payload->>'RunRequestID' FROM agent_schedule_receipt WHERE tenant_id=$1 AND source_key<>$2`, tenant, key).Scan(&secondID); err != nil {
		t.Fatal(err)
	}
	skipped, err := runtime.Claim(ctx, "common-tenant", secondID, "second-worker", 30*time.Second)
	var refusal *CommonAgentExecutionRefused
	if !errors.As(err, &refusal) || skipped.State != runstate.StateCancelled || skipped.TerminalCode != "SCHEDULE_OVERLAP_SKIPPED" {
		t.Fatalf("actual source overlap=%+v err=%v", skipped, err)
	}
	if reloaded, err := runtime.GetRun(ctx, "common-tenant", secondID); err != nil || reloaded.State != runstate.StateCancelled {
		t.Fatalf("overlap terminal reload=%+v err=%v", reloaded, err)
	}
	if _, err := service.Snapshot(context.Background()); !errors.Is(err, agentcontrols.ErrUnauthenticated) {
		t.Fatalf("missing authentication=%v", err)
	}
	input.Destination = "unapproved"
	if _, err := service.Preview(ownerCtx, input); !errors.Is(err, agentcontrols.ErrInvalid) {
		t.Fatalf("unapproved destination=%v", err)
	}
	// Turning agents off for the tenant stops schedule management as well.
	input.Destination = r.Audience.ID
	core.Exec(t, `UPDATE tenant_agent_setting SET enabled=false,revision=revision+1,updated_by='test-admin' WHERE tenant_id=$1`, tenant)
	if _, err := service.Preview(ownerCtx, input); !errors.Is(err, agentcontrols.ErrDenied) {
		t.Fatalf("preview with agents turned off=%v", err)
	}
}
