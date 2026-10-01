package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type agentRestoreTestPendingOwner func(context.Context, string, int) (int, error)

func (f agentRestoreTestPendingOwner) ReplayPending(ctx context.Context, tenant string, limit int) (int, error) {
	return f(ctx, tenant, limit)
}

func TestTodo_AGENT_046_RestoredUnknownEffectNeverReplayed(t *testing.T) {
	ctx := context.Background()
	source, target := pgtest.NewEmpty(t), pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, source.SQL); err != nil {
		t.Fatal(err)
	}
	if err := agentstore.Migrate(ctx, target.SQL); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	source.Exec(t, "INSERT INTO tenant(tenant_id) VALUES($1)", tenant)
	mapper := func(values.TenantId) uuid.UUID { return tenant }
	now := time.Date(2026, 9, 30, 17, 0, 0, 0, time.UTC)
	authority := &commonAgentTestAuthority{}
	agents := commonAgentOpenIntegrationStore(t, source)
	runtime, err := NewCommonAgentRuntime(CommonAgentRuntimeConfig{Stores: DatabaseCommonAgentStores{Agents: agents, TenantUUID: mapper}, Authority: authority, Now: func() time.Time { return now }, ExecutionFences: map[agentrun.SourceKind]CommonAgentExecutionFence{agentrun.SourceSchedule: commonAgentTestExecutionFence{}}})
	if err != nil {
		t.Fatal(err)
	}
	request := commonAgentTestRequest(now)
	request.Source, _ = (agentrun.CanonicalSourceConverter{}).ConvertSource(ctx, request)
	admitted, _, err := runtime.Admit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := runtime.Claim(ctx, request.Source.TenantID, admitted.ID, "worker-a", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	service, err := runtime.executionService(ctx, request.Source.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := service.BeginEffect(ctx, admitted.ID, "worker-a", "effect-a", "source-owner-dedupe-key", "sha256:"+strings.Repeat("c", 64), claimed.Fence, claimed.Version, now)
	if err != nil {
		t.Fatal(err)
	}
	image, err := agentstore.CaptureBackup(ctx, source.SQL, tenant, now)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if _, err := agentstore.RestoreBackup(ctx, target.SQL, tenant, image, now, time.Minute); err != nil {
		t.Fatal(err)
	}
	restored := commonAgentOpenIntegrationStore(t, target)
	restarted, err := NewCommonAgentRuntime(CommonAgentRuntimeConfig{Stores: DatabaseCommonAgentStores{Agents: restored, TenantUUID: mapper}, Authority: authority, Now: func() time.Time { return now }, ExecutionFences: map[agentrun.SourceKind]CommonAgentExecutionFence{agentrun.SourceSchedule: commonAgentTestExecutionFence{}}})
	if err != nil {
		t.Fatal(err)
	}
	recovery := AgentRestoreRuntime{Agents: restored, Runtime: restarted, TenantUUID: mapper}
	if _, err = recovery.TickTenant(ctx, request.Source.TenantID, now); err != nil {
		t.Fatal(err)
	}
	run, err := restarted.GetRun(ctx, request.Source.TenantID, admitted.ID)
	if err != nil || run.State != runstate.StateReconciling || len(run.Effects) != 1 || run.Effects[0].IdempotencyKey != pending.Effects[0].IdempotencyKey || run.Effects[0].Status != runstate.EffectUnknown || run.Fence != pending.Fence {
		t.Fatalf("ambiguous restored effect = %+v %v", run, err)
	}
	stale := errors.New("stale source owner")
	secondVisited := false
	recovery.PendingOwners = []AgentRestorePendingOwner{
		agentRestoreTestPendingOwner(func(context.Context, string, int) (int, error) { return 0, stale }),
		agentRestoreTestPendingOwner(func(context.Context, string, int) (int, error) { secondVisited = true; return 2, nil }),
	}
	if changed, err := recovery.TickTenant(ctx, request.Source.TenantID, now); changed != 2 || !errors.Is(err, stale) || !secondVisited {
		t.Fatalf("one unresolved source blocked other recovery owners: changes=%d err=%v second=%v", changed, err, secondVisited)
	}
	recovery.PendingOwners = nil
	authority.revoked = true
	if _, err = recovery.TickTenant(ctx, request.Source.TenantID, now); err != nil {
		t.Fatal(err)
	}
	cancelled, err := restarted.GetRun(ctx, request.Source.TenantID, admitted.ID)
	if err != nil || !cancelled.CancelRequested || cancelled.State != runstate.StateReconciling {
		t.Fatalf("revoked restored run = %+v %v", cancelled, err)
	}
	if _, err = recovery.TickTenant(ctx, request.Source.TenantID, now); err != nil {
		t.Fatal(err)
	}
	replay, err := restarted.GetRun(ctx, request.Source.TenantID, admitted.ID)
	if err != nil || replay.Version != cancelled.Version {
		t.Fatalf("replay advanced effect twice = %+v %v", replay, err)
	}
	if _, err := restarted.Claim(ctx, request.Source.TenantID, admitted.ID, "worker-b", time.Second); err == nil {
		t.Fatal("restored ambiguous effect got an executable lease")
	}
}
