package runstate

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// The AGENTCOST-001 run record: every model call seals one per-call ledger
// line beside the stage checkpoints, so spend regenerates from run records.
// Lines are append-only under the worker lease; committed lines never change.

func callLedgerTestLine(runID string, at time.Time) agentmodel.CallLine {
	return agentmodel.CallLine{
		TenantID: "tenant-a", AgentID: "agent-a", TriggerKind: "mention", RunID: runID,
		CallSite: "persona_run_executor:answer", Stage: agentmodel.CallStageAnswer, Kind: agentmodel.CallKindGeneration,
		ProviderID: "schemaflux", ModelID: "gpt-6-luna", ModelVersion: "2026-09-01",
		InputTokens: 1200, OutputTokens: 300, CostMicros: 255, Basis: agentmodel.CostMeasured,
		LatencyMillis: 812, StartedAt: at, ScheduleVersion: "schemaflux-2026-10-01",
	}
}

func TestRunstateRecordModelCalls(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	service, err := New(store, recheckerFunc(func(context.Context, string, string) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Start(ctx, acceptedAdmission(now))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.Claim(ctx, run.ID, "worker-1", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	first := callLedgerTestLine(run.ID, now)
	sealed, err := service.RecordModelCalls(ctx, run.ID, "worker-1", claimed.Fence, claimed.Version, []agentmodel.CallLine{first}, now)
	if err != nil {
		t.Fatalf("sealing one call line: %v", err)
	}
	if len(sealed.Calls) != 1 || sealed.Calls[0] != first {
		t.Fatalf("sealed calls = %+v", sealed.Calls)
	}
	stored, err := store.Get(ctx, run.ID)
	if err != nil || len(stored.Calls) != 1 || stored.Calls[0] != first {
		t.Fatalf("stored calls = %+v, %v", stored.Calls, err)
	}
	// A second call appends; the first line is untouched.
	second := callLedgerTestLine(run.ID, now.Add(time.Minute))
	second.CallSite = "persona_run_executor:tool_continuation"
	second.Stage = agentmodel.CallStageToolContinuation
	sealed, err = service.RecordModelCalls(ctx, run.ID, "worker-1", sealed.Fence, sealed.Version, []agentmodel.CallLine{second}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("sealing a second call line: %v", err)
	}
	if len(sealed.Calls) != 2 || sealed.Calls[0] != first || sealed.Calls[1] != second {
		t.Fatalf("appended calls = %+v", sealed.Calls)
	}
	// An invalid line seals nothing.
	broken := second
	broken.CostMicros = -1
	if _, err := service.RecordModelCalls(ctx, run.ID, "worker-1", sealed.Fence, sealed.Version, []agentmodel.CallLine{broken}, now); err == nil {
		t.Fatal("an invalid call line was sealed")
	}
	// A line for another run or tenant seals nothing.
	foreign := second
	foreign.RunID = "another-run"
	if _, err := service.RecordModelCalls(ctx, run.ID, "worker-1", sealed.Fence, sealed.Version, []agentmodel.CallLine{foreign}, now); err == nil {
		t.Fatal("a foreign call line was sealed")
	}
	// Without the lease nothing is sealed.
	if _, err := service.RecordModelCalls(ctx, run.ID, "worker-2", sealed.Fence, sealed.Version, []agentmodel.CallLine{second}, now); err == nil {
		t.Fatal("a call line was sealed without the lease")
	}
	stored, err = store.Get(ctx, run.ID)
	if err != nil || len(stored.Calls) != 2 {
		t.Fatalf("refused seals changed the record: %+v, %v", stored.Calls, err)
	}
}
