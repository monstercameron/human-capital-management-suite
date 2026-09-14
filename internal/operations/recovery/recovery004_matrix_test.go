package recovery

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func mustGameDayScenario(t *testing.T, asset LostAsset) GameDayScenario {
	t.Helper()
	return GameDayScenario{
		DrillID: "gameday-" + string(asset) + "-1", Asset: asset, Owner: "ops-commander",
		DegradedMode: "read-only pilot", BudgetRPO: time.Minute, BudgetRTO: 5 * time.Minute,
		Fence: "recovery-cell-1", CutoverFence: "recovery-cell-1", FailbackFence: "recovery-cell-1",
	}
}

func seedGameDayEvidence() GameDayEvidence {
	return GameDayEvidence{Incident: "INC", Advisory: "ADV", Repair: "REPAIR", PostReview: "REVIEW"}
}

func mustExecuteGameDay(t *testing.T, in GameDayInput) GameDayReport {
	t.Helper()
	report, err := ExecuteGameDay(in)
	if err != nil {
		t.Fatalf("ExecuteGameDay(%s): %v", in.Scenario.DrillID, err)
	}
	return report
}

func requireGameDayError(t *testing.T, err error, fragment string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", fragment)
	}
	if !strings.Contains(err.Error(), fragment) {
		t.Fatalf("expected error containing %q, got %v", fragment, err)
	}
}

// TestTodo_RECOVERY_004_Integration: all seven lost assets drill with
// owner, degraded mode and fences.
func TestTodo_RECOVERY_004_Integration(t *testing.T) {
	for _, asset := range []LostAsset{LostDatabase, LostRegion, LostLedgerShard, LostSigningKey, LostAdmin, LostIdP, LostProvider} {
		report := mustExecuteGameDay(t, GameDayInput{
			Scenario:    mustGameDayScenario(t, asset),
			ObservedRPO: 30 * time.Second, ObservedRTO: 4 * time.Minute,
			Evidence: seedGameDayEvidence(), FailedBack: true,
		})
		if report.Status != GameDayPass || !report.FailedBack {
			t.Fatalf("asset %s: %+v", asset, report)
		}
		if report.Digest == "" {
			t.Fatalf("asset %s: report without digest", asset)
		}
	}
}

// TestTodo_RECOVERY_004_Fault: missing owners, fences, evidence and
// production injection fail closed.
func TestTodo_RECOVERY_004_Fault(t *testing.T) {
	good := mustGameDayScenario(t, LostRegion)
	ownerless := good
	ownerless.Owner = ""
	_, err := ExecuteGameDay(GameDayInput{Scenario: ownerless, Evidence: seedGameDayEvidence()})
	requireGameDayError(t, err, "decision owner")
	modeless := good
	modeless.DegradedMode = ""
	_, err = ExecuteGameDay(GameDayInput{Scenario: modeless, Evidence: seedGameDayEvidence()})
	requireGameDayError(t, err, "degraded mode")
	fenceless := good
	fenceless.CutoverFence = ""
	_, err = ExecuteGameDay(GameDayInput{Scenario: fenceless, Evidence: seedGameDayEvidence()})
	requireGameDayError(t, err, "fences are required")
	// Failure injection cannot escape to production.
	escaped := good
	escaped.Fence = "production"
	_, err = ExecuteGameDay(GameDayInput{Scenario: escaped, Evidence: seedGameDayEvidence()})
	requireGameDayError(t, err, "cannot target production")
	unknown := good
	unknown.Asset = "moon"
	_, err = ExecuteGameDay(GameDayInput{Scenario: unknown, Evidence: seedGameDayEvidence()})
	requireGameDayError(t, err, "unknown lost asset")
	// Missing evidence fences the gate per artifact.
	bare := mustExecuteGameDay(t, GameDayInput{Scenario: good, Evidence: GameDayEvidence{}})
	if bare.Status != GameDayFailedGate || len(bare.Findings) != 4 {
		t.Fatalf("bare evidence: %+v", bare)
	}
	negative := good
	_, err = ExecuteGameDay(GameDayInput{Scenario: negative, ObservedRPO: -time.Second, Evidence: seedGameDayEvidence()})
	requireGameDayError(t, err, "cannot be negative")
}

// TestTodo_RECOVERY_004_Security: drill reports are digest-bound to
// their inputs; altered outcomes do not verify.
func TestTodo_RECOVERY_004_Security(t *testing.T) {
	first := mustExecuteGameDay(t, GameDayInput{
		Scenario:    mustGameDayScenario(t, LostSigningKey),
		ObservedRPO: 30 * time.Second, ObservedRTO: 4 * time.Minute,
		Evidence: seedGameDayEvidence(),
	})
	second := mustExecuteGameDay(t, GameDayInput{
		Scenario:    mustGameDayScenario(t, LostSigningKey),
		ObservedRPO: 30 * time.Second, ObservedRTO: 4 * time.Minute,
		Evidence: GameDayEvidence{Incident: "INC-FORGED", Advisory: "ADV", Repair: "REPAIR", PostReview: "REVIEW"},
	})
	if first.Digest == second.Digest {
		t.Fatal("forged evidence verifies against the honest digest")
	}
	if first.Status != GameDayPass || second.Status != GameDayPass {
		t.Fatalf("evidence swap changed the gate: %+v %+v", first, second)
	}
}

// TestTodo_RECOVERY_004_Recovery: failback restores service with review
// evidence; the cell files each drill exactly once.
func TestTodo_RECOVERY_004_Recovery(t *testing.T) {
	cell := NewGameDayCell()
	in := GameDayInput{
		Scenario:    mustGameDayScenario(t, LostDatabase),
		ObservedRPO: 30 * time.Second, ObservedRTO: 4 * time.Minute,
		Evidence: seedGameDayEvidence(), FailedBack: true,
	}
	first, err := cell.Record(in)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	second, err := cell.Record(in)
	if err != nil {
		t.Fatalf("re-Record: %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("drill digest drift:\n got=%q\nwant=%q", second.Digest, first.Digest)
	}
	// A failed gate replays deterministically too.
	in.Scenario.DrillID = "gameday-database-2"
	in.ObservedRTO = time.Hour
	failed, err := cell.Record(in)
	if err != nil {
		t.Fatalf("failed Record: %v", err)
	}
	if failed.Status != GameDayFailedGate {
		t.Fatalf("over-budget replay passed: %+v", failed)
	}
}

// TestTodo_RECOVERY_004_Mutation: budget and identity edges resolve on
// the documented side.
func TestTodo_RECOVERY_004_Mutation(t *testing.T) {
	scenario := mustGameDayScenario(t, LostAdmin)
	// Exactly at budget passes; one nanosecond over fences.
	at := mustExecuteGameDay(t, GameDayInput{
		Scenario: scenario, ObservedRPO: time.Minute, ObservedRTO: 5 * time.Minute,
		Evidence: seedGameDayEvidence(),
	})
	if at.Status != GameDayPass {
		t.Fatalf("at-budget: %+v", at)
	}
	over := mustExecuteGameDay(t, GameDayInput{
		Scenario: scenario, ObservedRPO: time.Minute, ObservedRTO: 5*time.Minute + time.Nanosecond,
		Evidence: seedGameDayEvidence(),
	})
	if over.Status != GameDayFailedGate {
		t.Fatalf("over-budget: %+v", over)
	}
	padded := scenario
	padded.DrillID = " gameday-admin-1"
	_, err := ExecuteGameDay(GameDayInput{Scenario: padded, Evidence: seedGameDayEvidence()})
	requireGameDayError(t, err, "drill id")
}

// TestTodo_RECOVERY_004_Race: concurrent drill filings keep one report
// per drill ID.
func TestTodo_RECOVERY_004_Race(t *testing.T) {
	cell := NewGameDayCell()
	const racers = 16
	var wg sync.WaitGroup
	reports := make([]GameDayReport, racers)
	errs := make([]error, racers)
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			reports[i], errs[i] = cell.Record(GameDayInput{
				Scenario:    mustGameDayScenario(t, LostProvider),
				ObservedRPO: 30 * time.Second, ObservedRTO: 4 * time.Minute,
				Evidence: seedGameDayEvidence(),
			})
		}(i)
	}
	wg.Wait()
	for i := range racers {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		if reports[i].Digest != reports[0].Digest {
			t.Fatalf("racer %d digest drift", i)
		}
	}
}
