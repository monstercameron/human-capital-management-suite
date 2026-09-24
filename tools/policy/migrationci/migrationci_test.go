package migrationci

import (
	"errors"
	"strings"
	"testing"
)

func testManifest() Manifest {
	return Manifest{
		ReleaseDigest: strings.Repeat("a", 64),
		Entries: []Entry{
			{Version: 177, Name: "schema_upgrade_control", Checksum: strings.Repeat("1", 64), Compatibility: "FULL", Direction: "UP"},
			{Version: 178, Name: "telemetry_backend_registry", Checksum: strings.Repeat("2", 64), Compatibility: "BACKWARD_COMPATIBLE", Direction: "UP", Requires: []int64{177}},
			{Version: 179, Name: "telemetry_copy_inventory", Checksum: strings.Repeat("3", 64), Compatibility: "BACKWARD_COMPATIBLE", Direction: "UP", Requires: []int64{178}},
		},
		Phases:            []Phase{PhaseExpand, PhaseBackfill, PhaseShadow, PhaseCutover, PhaseContract},
		AdoptionWatermark: 7,
	}
}

func passingInput() Input {
	return Input{Manifest: testManifest(), BackfillComplete: true, ShadowExact: true, MixedVersionCompatible: true}
}

func TestTodo_CICD_002(t *testing.T) {
	result, err := Rehearse(passingInput())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "PASS" || len(result.Steps) != 5 {
		t.Fatalf("result=%+v, want five-step PASS rehearsal", result)
	}
}

func TestTodo_CICD_002_Golden(t *testing.T) {
	result, err := Rehearse(passingInput())
	if err != nil {
		t.Fatal(err)
	}
	want := "migration rehearsal PASS phases=EXPAND>BACKFILL>SHADOW>CUTOVER>CONTRACT findings=0"
	if got := ExplainResult(result); got != want {
		t.Fatalf("ExplainResult=%q, want %q", got, want)
	}
}

func TestTodo_CICD_002_RejectsDirtyLockAndMixedVersion(t *testing.T) {
	input := passingInput()
	input.Dirty, input.LockHeld, input.ChecksumMismatch, input.MixedVersionCompatible = true, true, true, false
	result, err := Rehearse(input)
	if !errors.Is(err, ErrRehearsalFailed) || len(result.Findings) != 4 || result.Status != "REJECTED" {
		t.Fatalf("result=%+v err=%v, want four blocking findings", result, err)
	}
}

func TestMigrationCIFencesContractOnBackfillOrShadowFailure(t *testing.T) {
	input := passingInput()
	input.BackfillComplete = false
	result, err := Rehearse(input)
	if !errors.Is(err, ErrRehearsalFailed) || len(result.Steps) != 1 || result.Steps[0] != PhaseExpand {
		t.Fatalf("result=%+v err=%v, want expansion only", result, err)
	}
	input = passingInput()
	input.AbortRequested = true
	result, err = Rehearse(input)
	if !errors.Is(err, ErrRehearsalFailed) || result.Steps[len(result.Steps)-1] != PhaseCutover {
		t.Fatalf("abort result=%+v err=%v, want contract fenced", result, err)
	}
}

func TestMigrationCIRejectsUnreviewedCompatibility(t *testing.T) {
	input := passingInput()
	input.Manifest.Entries[0].Compatibility = "UNREVIEWED"
	result, err := Rehearse(input)
	if !errors.Is(err, ErrRehearsalFailed) || result.Status != "REJECTED" {
		t.Fatalf("unreviewed compatibility result=%+v err=%v, want rejection", result, err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Code != "COMPATIBILITY_UNREVIEWED" {
		t.Fatalf("unreviewed compatibility findings=%+v, want COMPATIBILITY_UNREVIEWED", result.Findings)
	}
}
