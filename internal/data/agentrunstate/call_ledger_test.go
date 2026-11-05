package agentrunstate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// The AGENTCOST-001 run record in PostgreSQL: per-call ledger lines sealed by
// the run survive a store round trip with every field intact, stay ordered,
// and cannot be rewritten: a changed committed line is a revision conflict.

func callLedgerSQLLine(runID string, at time.Time) agentmodel.CallLine {
	return agentmodel.CallLine{
		TenantID: "tenant-a", AgentID: "agent-a", TriggerKind: "mention", RunID: runID, QuestionID: "q1",
		CallSite: "persona_run_executor:answer", Stage: agentmodel.CallStageAnswer, Kind: agentmodel.CallKindGeneration,
		ProviderID: "schemaflux", ModelID: "gpt-6-luna", ModelVersion: "2026-09-01",
		InputTokens: 1200, OutputTokens: 300, CachedInputTokens: 400,
		CostMicros: 255, Basis: agentmodel.CostMeasured, LatencyMillis: 812,
		StartedAt: at, PromptDigest: "sha256:aaa", ScheduleVersion: "schemaflux-2026-10-01",
	}
}

func TestTodo_AGENTCOST_001_CallLinesSurvivePostgres(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("agent migrations: %v", err)
	}
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1)`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	store := openAgentStore(t, db)
	defer store.Close()
	repository, err := New(store, func(tenant string) uuid.UUID {
		if tenant == "tenant-a" {
			return tenantID
		}
		return uuid.Nil
	})
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := repository.ForTenant("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	admission := testAcceptedAdmission(now)
	if err := insertAcceptedAdmission(ctx, store, tenantID, admission); err != nil {
		t.Fatalf("insert accepted admission fixture: %v", err)
	}
	service, err := runstate.New(tenant, allowRecheck{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Start(ctx, admission)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.Claim(ctx, run.ID, "worker-1", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	first := callLedgerSQLLine(run.ID, now)
	sealed, err := service.RecordModelCalls(ctx, run.ID, "worker-1", claimed.Fence, claimed.Version, []agentmodel.CallLine{first}, now)
	if err != nil {
		t.Fatalf("sealing one call line: %v", err)
	}
	second := callLedgerSQLLine(run.ID, now.Add(time.Minute))
	second.CallSite = "persona_run_executor:tool_continuation"
	second.Stage = agentmodel.CallStageToolContinuation
	sealed, err = service.RecordModelCalls(ctx, run.ID, "worker-1", sealed.Fence, sealed.Version, []agentmodel.CallLine{second}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("sealing a second call line: %v", err)
	}
	// Every field survives the round trip, in order. StartedAt keeps the
	// precision the run record keeps after it is stored.
	stored, err := tenant.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Calls) != 2 {
		t.Fatalf("stored calls = %+v", stored.Calls)
	}
	for i, want := range []agentmodel.CallLine{first, second} {
		got := stored.Calls[i]
		want.StartedAt, got.StartedAt = want.StartedAt.UTC(), got.StartedAt.UTC()
		if got != want {
			t.Fatalf("stored call %d = %+v, want %+v", i, got, want)
		}
	}
	// A rewritten committed line is refused, not stored.
	stored.Calls[0].CostMicros++
	if err := tenant.Save(ctx, stored, sealed.Version); err == nil {
		t.Fatal("a rewritten call line was stored")
	} else if !errors.Is(err, ErrConflict) {
		t.Fatalf("rewritten call line error = %v, want a revision conflict", err)
	}
	fresh, err := tenant.Get(ctx, run.ID)
	if err != nil || len(fresh.Calls) != 2 || fresh.Calls[0].CostMicros != first.CostMicros {
		t.Fatalf("the refused rewrite changed the record: %+v, %v", fresh.Calls, err)
	}
}
