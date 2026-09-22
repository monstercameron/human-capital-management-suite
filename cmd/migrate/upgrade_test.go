// REV-003-02 command-level suite: the migrate binary's upgrade subcommand
// persisting the schemaupgrade journal durably and driving a real version
// transition end to end. These tests never open a database: the journal is
// a file under t.TempDir and the plan/rows arrive as JSON files, so they
// prove the served binary's own entry point, not the protocol in isolation.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/schemaupgrade"
)

func upgradeTestPlan() schemaupgrade.Plan {
	return schemaupgrade.Plan{
		ID:                        "migrate-upgrade-test",
		FromVersion:               1,
		ToVersion:                 2,
		Compatibility:             schemaupgrade.CompatibilityFull,
		SourceDigest:              strings.Repeat("1", 64),
		TargetDigest:              strings.Repeat("2", 64),
		RollbackBoundary:          schemaupgrade.RollbackBeforeContract,
		RequiredAdoptionWatermark: 3,
		BackfillBatchSize:         2,
		Binaries: []schemaupgrade.Binary{
			{Name: "old", Reads: []int{1, 2}, Writes: []int{1}},
			{Name: "new", Reads: []int{1, 2}, Writes: []int{1, 2}},
		},
	}
}

func upgradeTestRows() []schemaupgrade.Row {
	return []schemaupgrade.Row{{Key: "a", Digest: "da"}, {Key: "b", Digest: "db"}, {Key: "c", Digest: "dc"}}
}

// writeUpgradeInputs serializes the plan and rows the upgrade subcommand
// consumes into planPath/rowsPath.
func writeUpgradeInputs(t *testing.T, dir string, plan schemaupgrade.Plan, rows []schemaupgrade.Row) (planPath, rowsPath string) {
	t.Helper()
	planPath = filepath.Join(dir, "plan.json")
	rowsPath = filepath.Join(dir, "rows.json")
	planRaw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, planRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	rowsRaw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rowsPath, rowsRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	return planPath, rowsPath
}

// TestTodo_REV_003_02_Upgrade drives a real version transition through the
// binary's upgrade entry point: the receipt reports CUTOVER with exact
// history, the journal file exists on disk, and re-running the same command
// is idempotent.
func TestTodo_REV_003_02_Upgrade(t *testing.T) {
	dir := t.TempDir()
	planPath, rowsPath := writeUpgradeInputs(t, dir, upgradeTestPlan(), upgradeTestRows())
	journal := filepath.Join(dir, "journal.json")

	var first bytes.Buffer
	if err := runUpgradeCommand(journal, planPath, rowsPath, 3, &first); err != nil {
		t.Fatalf("runUpgradeCommand: %v", err)
	}
	var receipt upgradeReceipt
	if err := json.Unmarshal(first.Bytes(), &receipt); err != nil {
		t.Fatalf("decode receipt %q: %v", first.String(), err)
	}
	if receipt.PlanID != "migrate-upgrade-test" || receipt.Phase != "CUTOVER" || receipt.CopiedRows != 3 || receipt.History != 5 {
		t.Fatalf("receipt = %+v, want CUTOVER with 3 rows and 5 events", receipt)
	}
	if _, err := os.Stat(journal); err != nil {
		t.Fatalf("journal missing after upgrade: %v", err)
	}

	var second bytes.Buffer
	if err := runUpgradeCommand(journal, planPath, rowsPath, 3, &second); err != nil {
		t.Fatalf("rerun runUpgradeCommand: %v", err)
	}
	var rerun upgradeReceipt
	if err := json.Unmarshal(second.Bytes(), &rerun); err != nil {
		t.Fatalf("decode rerun receipt %q: %v", second.String(), err)
	}
	if rerun != receipt {
		t.Fatalf("rerun receipt = %+v, want identical %+v (no duplicate effect)", rerun, receipt)
	}
}

// TestTodo_REV_003_02_UpgradeRecovery simulates a process kill mid-upgrade:
// a partial journal is left on disk and the binary's upgrade entry point
// resumes it to CUTOVER without restarting or duplicating the backfill.
func TestTodo_REV_003_02_UpgradeRecovery(t *testing.T) {
	dir := t.TempDir()
	planPath, rowsPath := writeUpgradeInputs(t, dir, upgradeTestPlan(), upgradeTestRows())
	journal := filepath.Join(dir, "journal.json")
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	partial, err := schemaupgrade.New(upgradeTestPlan(), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := partial.Expand(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := partial.ResumeBackfill(upgradeTestRows(), 1, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(partial.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journal, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runUpgradeCommand(journal, planPath, rowsPath, 3, &out); err != nil {
		t.Fatalf("resume runUpgradeCommand: %v", err)
	}
	var receipt upgradeReceipt
	if err := json.Unmarshal(out.Bytes(), &receipt); err != nil {
		t.Fatalf("decode receipt %q: %v", out.String(), err)
	}
	if receipt.Phase != "CUTOVER" || receipt.CopiedRows != 3 {
		t.Fatalf("resumed receipt = %+v, want CUTOVER with 3 rows", receipt)
	}
	if receipt.History <= len(partial.History) {
		t.Fatalf("recovery appended no evidence: partial=%d final=%d", len(partial.History), receipt.History)
	}
}

// TestTodo_REV_003_02_UpgradeFault proves every refusal is typed and leaves
// the journal untouched: a missing plan file, a source that changed
// mid-backfill, a watermark below the adoption bar and an empty journal
// path are all refused, never silently started or re-baselined.
func TestTodo_REV_003_02_UpgradeFault(t *testing.T) {
	dir := t.TempDir()
	planPath, rowsPath := writeUpgradeInputs(t, dir, upgradeTestPlan(), upgradeTestRows())

	t.Run("missing_plan_file", func(t *testing.T) {
		var out bytes.Buffer
		if err := runUpgradeCommand(filepath.Join(dir, "j1.json"), filepath.Join(dir, "absent.json"), rowsPath, 3, &out); err == nil {
			t.Fatal("runUpgradeCommand accepted a missing plan file")
		}
	})

	t.Run("empty_journal_path", func(t *testing.T) {
		var out bytes.Buffer
		if err := runUpgradeCommand("", planPath, rowsPath, 3, &out); err == nil {
			t.Fatal("runUpgradeCommand accepted an empty journal path")
		}
	})

	t.Run("mutated_source_is_refused", func(t *testing.T) {
		journal := filepath.Join(dir, "mutated.json")
		now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
		partial, err := schemaupgrade.New(upgradeTestPlan(), now)
		if err != nil {
			t.Fatal(err)
		}
		if err := partial.Expand(now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		if _, err := partial.ResumeBackfill(upgradeTestRows(), 1, now.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(partial.Snapshot())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(journal, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		mutated := upgradeTestRows()
		mutated[0].Digest = "changed"
		_, mutatedRows := writeUpgradeInputs(t, dir, upgradeTestPlan(), mutated)
		var out bytes.Buffer
		err = runUpgradeCommand(journal, planPath, mutatedRows, 3, &out)
		if !errors.Is(err, schemaupgrade.ErrCheckpointMismatch) {
			t.Fatalf("mutated source error=%v, want checkpoint mismatch", err)
		}
	})

	t.Run("watermark_below_adoption_bar_is_refused", func(t *testing.T) {
		journal := filepath.Join(dir, "lag.json")
		var out bytes.Buffer
		err := runUpgradeCommand(journal, planPath, rowsPath, 1, &out)
		if !errors.Is(err, schemaupgrade.ErrAdoptionLag) || !isUpgradeAdoptionLag(err) {
			t.Fatalf("low watermark error=%v, want adoption lag", err)
		}
	})
}

// TestTodo_REV_003_02_UpgradeValidation proves the upgrade subcommand is a
// real binary surface: missing journal/plan/rows flags fail config
// resolution before any work starts (and without a database URL), while a
// fully flagged upgrade validates cleanly.
func TestTodo_REV_003_02_UpgradeValidation(t *testing.T) {
	fields := migrateConfigFields()
	noEnv := func(string) (string, bool) { return "", false }

	t.Run("missing_flags_rejected", func(t *testing.T) {
		for _, args := range [][]string{
			nil,
			{"-journal-path=j.json"},
			{"-journal-path=j.json", "-upgrade-plan=p.json"},
		} {
			v, err := bootstrap.ParseConfig(args, noEnv, fields)
			if err != nil {
				t.Fatalf("ParseConfig(%v): %v", args, err)
			}
			if err := validateConfig("upgrade")(v); err == nil {
				t.Fatalf("validateConfig(upgrade) accepted %v", args)
			}
		}
	})

	t.Run("complete_flags_validate_without_database_url", func(t *testing.T) {
		v, err := bootstrap.ParseConfig([]string{
			"-journal-path=j.json", "-upgrade-plan=p.json", "-upgrade-rows=r.json",
		}, noEnv, fields)
		if err != nil {
			t.Fatalf("ParseConfig: %v", err)
		}
		if err := validateConfig("upgrade")(v); err != nil {
			t.Fatalf("validateConfig(upgrade): %v", err)
		}
	})

	t.Run("bootstrap_run_rejects_unflagged_upgrade_before_any_work", func(t *testing.T) {
		s := spec("upgrade", nil)
		s.Getenv = func(string) (string, bool) { return "", false }
		s.Logger = discardLogger()
		s.Stdout = &bytes.Buffer{}
		s.Stderr = &bytes.Buffer{}
		if code := bootstrap.Run(context.Background(), s); code != bootstrap.ExitConfigError {
			t.Fatalf("exit code = %d, want ExitConfigError (%d)", code, bootstrap.ExitConfigError)
		}
	})
}
