package execute

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func sampleRepairRecord(stage RepairRecordStage) RepairRecord {
	return RepairRecord{
		TenantID: executeRepairTenant, FenceKey: "repair:plan-1:effect:iam", Stage: stage,
		FenceID: "repair-fence:sha256:evidence", PlanDigest: "sha256:plan-1",
		OriginalSemanticKey: "promotion:worker-1", FailedEffectKey: "effect:iam",
		Status: RepairUnknown, ConsistencyState: "UNKNOWN",
		RecordedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	}
}

// TestMemoryRepairRecordsAppendsEachStageOnce proves the in-process store has
// the same shape the PostgreSQL one enforces with its primary key: a stage is
// written by exactly one caller, and a second caller is told so rather than
// silently overwriting it.
func TestMemoryRepairRecordsAppendsEachStageOnce(t *testing.T) {
	ctx := context.Background()
	records := NewMemoryRepairRecords()
	first, err := records.AppendRepairRecord(ctx, sampleRepairRecord(RepairStageClaimed))
	if err != nil || !first {
		t.Fatalf("first claim = %t, %v, want true", first, err)
	}
	second, err := records.AppendRepairRecord(ctx, sampleRepairRecord(RepairStageClaimed))
	if err != nil || second {
		t.Fatalf("second claim = %t, %v, want false", second, err)
	}
	if _, err := records.AppendRepairRecord(ctx, sampleRepairRecord(RepairStageExecuted)); err != nil {
		t.Fatal(err)
	}
	stored, err := records.LoadRepairRecords(ctx, executeRepairTenant, "repair:plan-1:effect:iam")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored[0].Stage != RepairStageClaimed || stored[1].Stage != RepairStageExecuted {
		t.Fatalf("stored = %+v, want one claim and one executed row", stored)
	}
	// The returned slice is a copy: mutating it cannot corrupt the record.
	stored[0].Status = RepairCompleted
	reread, err := records.LoadRepairRecords(ctx, executeRepairTenant, "repair:plan-1:effect:iam")
	if err != nil {
		t.Fatal(err)
	}
	if reread[0].Status != RepairUnknown {
		t.Fatalf("record mutated through the returned slice: %+v", reread[0])
	}
}

// TestMemoryRepairRecordsIsTenantScoped proves one tenant's fence never
// answers another tenant's read, which is what the RLS policy enforces in
// PostgreSQL.
func TestMemoryRepairRecordsIsTenantScoped(t *testing.T) {
	ctx := context.Background()
	records := NewMemoryRepairRecords()
	if _, err := records.AppendRepairRecord(ctx, sampleRepairRecord(RepairStageClaimed)); err != nil {
		t.Fatal(err)
	}
	other := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	stored, err := records.LoadRepairRecords(ctx, other, "repair:plan-1:effect:iam")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 0 {
		t.Fatalf("another tenant read %d records for the same fence key", len(stored))
	}
	claimed, err := records.AppendRepairRecord(ctx, func() RepairRecord {
		r := sampleRepairRecord(RepairStageClaimed)
		r.TenantID = other
		return r
	}())
	if err != nil || !claimed {
		t.Fatalf("another tenant's claim = %t, %v, want an independent claim", claimed, err)
	}
}

// TestMemoryRepairRecordsClaimIsAtomic releases sixteen goroutines at one
// stage; exactly one may be told it wrote it.
func TestMemoryRepairRecordsClaimIsAtomic(t *testing.T) {
	const racers = 16
	records := NewMemoryRepairRecords()
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	won := make([]bool, racers)
	for i := 0; i < racers; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			claimed, err := records.AppendRepairRecord(context.Background(), sampleRepairRecord(RepairStageClaimed))
			if err != nil {
				t.Errorf("racer %d: %v", i, err)
			}
			won[i] = claimed
		}(i)
	}
	start.Done()
	done.Wait()
	winners := 0
	for _, claimed := range won {
		if claimed {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("winners = %d under %d concurrent claims, want exactly one", winners, racers)
	}
}

// TestRepairRecordValidationRefusesIncompleteRows names every precondition, so
// an unscoped or unstaged record cannot reach a store.
func TestRepairRecordValidationRefusesIncompleteRows(t *testing.T) {
	records := NewMemoryRepairRecords()
	cases := map[string]func(RepairRecord) RepairRecord{
		"no tenant":   func(r RepairRecord) RepairRecord { r.TenantID = uuid.Nil; return r },
		"no fence":    func(r RepairRecord) RepairRecord { r.FenceKey = "  "; return r },
		"bad stage":   func(r RepairRecord) RepairRecord { r.Stage = "DONE"; return r },
		"no status":   func(r RepairRecord) RepairRecord { r.Status = ""; return r },
		"no instant":  func(r RepairRecord) RepairRecord { r.RecordedAt = time.Time{}; return r },
		"valid input": nil,
	}
	for name, mutate := range cases {
		record := sampleRepairRecord(RepairStageSettled)
		if mutate != nil {
			record = mutate(record)
		}
		claimed, err := records.AppendRepairRecord(context.Background(), record)
		if mutate == nil {
			if err != nil || !claimed {
				t.Fatalf("%s: %t, %v", name, claimed, err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	if _, err := records.LoadRepairRecords(context.Background(), uuid.Nil, "repair:plan-1"); err == nil {
		t.Fatal("an unscoped load was accepted")
	}
	if _, err := records.LoadRepairRecords(context.Background(), executeRepairTenant, " "); err == nil {
		t.Fatal("a load without a fence key was accepted")
	}
}

// TestRepairRecordRebuildsEffectAndObservation proves a settled or executed
// record carries enough to resume without asking the provider again.
func TestRepairRecordRebuildsEffectAndObservation(t *testing.T) {
	record := sampleRepairRecord(RepairStageExecuted)
	record.Executed = true
	record.EffectRef = "operation:iam-1"
	record.EffectResultRef = "effect-result:1"
	record.ObservationState = "IAM_EXPECTED"
	record.ObservationDigest = "sha256:observed"
	record.ObservationComplete = true

	effect := record.effectOf()
	if effect.EffectKey != "effect:iam" || effect.EffectRef != "operation:iam-1" || !effect.Accepted || effect.ResultRef != "effect-result:1" {
		t.Fatalf("rebuilt effect = %+v", effect)
	}
	observation := record.observationOf()
	if !observation.Observed || !observation.Complete || observation.State != "IAM_EXPECTED" || observation.Digest != "sha256:observed" {
		t.Fatalf("rebuilt observation = %+v", observation)
	}
	empty := sampleRepairRecord(RepairStageClaimed).observationOf()
	if empty.Observed {
		t.Fatalf("a claim with no observation state reported one: %+v", empty)
	}
}

// TestFindRepairStageAndStageValidity pins the two small helpers the executor
// branches on.
func TestFindRepairStageAndStageValidity(t *testing.T) {
	records := []RepairRecord{sampleRepairRecord(RepairStageClaimed), sampleRepairRecord(RepairStageSettled)}
	if _, ok := findRepairStage(records, RepairStageExecuted); ok {
		t.Fatal("found an EXECUTED stage that was never written")
	}
	found, ok := findRepairStage(records, RepairStageSettled)
	if !ok || found.Stage != RepairStageSettled {
		t.Fatalf("findRepairStage(SETTLED) = %+v, %t", found, ok)
	}
	for _, stage := range []RepairRecordStage{RepairStageClaimed, RepairStageExecuted, RepairStageSettled} {
		if !stage.Valid() {
			t.Fatalf("%s reported invalid", stage)
		}
	}
	if RepairRecordStage("ABORTED").Valid() {
		t.Fatal("an undeclared stage reported valid")
	}
}
