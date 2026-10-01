package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/schemaupgrade"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/upgradejournal"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

var errMigrationIdentityMismatch = errors.New("migration: phase plan identity does not match embedded manifest")

// migrationPhasePlanFile is the operator-supplied phase contract. Keeping the
// migration identity beside the rolling-upgrade plan prevents a valid-looking
// phase journal from being replayed against a different SQL artifact.
type migrationPhasePlanFile struct {
	MigrationID    string             `json:"migration_id"`
	ManifestDigest string             `json:"manifest_digest"`
	Plan           schemaupgrade.Plan `json:"plan"`
}

type migrationPhaseReceipt struct {
	MigrationID    string   `json:"migration_id"`
	ManifestDigest string   `json:"manifest_digest"`
	PlanID         string   `json:"plan"`
	Phase          string   `json:"phase"`
	Gates          []string `json:"gates"`
	CopiedRows     int      `json:"copied_rows"`
	History        int      `json:"history_events"`
}

func runMigrationPhaseCommand(ctx context.Context, db *sql.DB, journalPath, planPath, rowsPath string, watermark uint64, out io.Writer) error {
	return withMigrationLock(ctx, db, func() error {
		return runMigrationPhaseGates(ctx, db, journalPath, planPath, rowsPath, watermark, out)
	})
}

// runMigrationPhaseGates verifies the PostgreSQL schema release and then
// drives the explicitly identified rolling phase journal through contract.
// The database is passed separately so the phase protocol remains a thin
// command adapter; business backfills belong to governed repair/job paths.
func runMigrationPhaseGates(ctx context.Context, db *sql.DB, journalPath, planPath, rowsPath string, watermark uint64, out io.Writer) error {
	_, _, declared, source, err := loadMigrationPhaseInputs(planPath, rowsPath)
	if err != nil {
		return err
	}
	if err := verifyMigrationJournalClean(ctx, db); err != nil {
		return err
	}
	if err := runMigrateCommandLocked(ctx, "status", db, io.Discard); err != nil {
		return fmt.Errorf("verify schema readiness: %w", err)
	}

	coord, err := upgradejournal.OpenCoordinator(journalPath, nil)
	if err != nil {
		return err
	}
	if !coord.Started() {
		if _, err := coord.Start(declared.Plan); err != nil {
			return fmt.Errorf("start migration phase gates: %w", err)
		}
	} else {
		recorded, err := coord.Load()
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(recorded.Plan, declared.Plan) {
			return fmt.Errorf("%w: phase plan %q does not match journal plan %q", schemaupgrade.ErrCheckpointMismatch, declared.Plan.ID, recorded.Plan.ID)
		}
	}
	state, err := coord.RunToContract(source, watermark)
	if err != nil {
		return fmt.Errorf("drive migration phase gates: %w", err)
	}
	if err := verifyMigrationPhaseHistory(state); err != nil {
		return err
	}
	receipt := migrationPhaseReceipt{
		MigrationID:    declared.MigrationID,
		ManifestDigest: declared.ManifestDigest,
		PlanID:         state.Plan.ID,
		Phase:          string(state.Phase),
		Gates:          []string{"EXPAND", "BACKFILL", "SHADOW", "CUTOVER", "CONTRACT"},
		CopiedRows:     state.Checkpoint.CopiedRows,
		History:        len(state.History),
	}
	if err := json.NewEncoder(out).Encode(receipt); err != nil {
		return fmt.Errorf("write migration phase receipt: %w", err)
	}
	return nil
}

func loadMigrationPhaseInputs(planPath, rowsPath string) ([]migrations.File, string, migrationPhasePlanFile, []schemaupgrade.Row, error) {
	files, err := migrations.Files()
	if err != nil {
		return nil, "", migrationPhasePlanFile{}, nil, err
	}
	digest, err := migrations.ArtifactDigest()
	if err != nil {
		return nil, "", migrationPhasePlanFile{}, nil, err
	}
	planRaw, err := os.ReadFile(planPath)
	if err != nil {
		return nil, "", migrationPhasePlanFile{}, nil, fmt.Errorf("read migration phase plan %q: %w", planPath, err)
	}
	var declared migrationPhasePlanFile
	if err := json.Unmarshal(planRaw, &declared); err != nil {
		return nil, "", migrationPhasePlanFile{}, nil, fmt.Errorf("decode migration phase plan %q: %w", planPath, err)
	}
	if err := validateMigrationPhasePlan(declared, files, digest); err != nil {
		return nil, "", migrationPhasePlanFile{}, nil, err
	}
	rowsRaw, err := os.ReadFile(rowsPath)
	if err != nil {
		return nil, "", migrationPhasePlanFile{}, nil, fmt.Errorf("read migration phase rows %q: %w", rowsPath, err)
	}
	var source []schemaupgrade.Row
	if err := json.Unmarshal(rowsRaw, &source); err != nil {
		return nil, "", migrationPhasePlanFile{}, nil, fmt.Errorf("decode migration phase rows %q: %w", rowsPath, err)
	}
	return files, digest, declared, source, nil
}

func validateMigrationPhaseFiles(planPath, rowsPath string) error {
	_, _, _, _, err := loadMigrationPhaseInputs(planPath, rowsPath)
	return err
}

func verifyMigrationPhaseHistory(state schemaupgrade.State) error {
	want := []schemaupgrade.Phase{
		schemaupgrade.PhasePlanned,
		schemaupgrade.PhaseExpanded,
		schemaupgrade.PhaseBackfilled,
		schemaupgrade.PhaseShadowed,
		schemaupgrade.PhaseCutover,
		schemaupgrade.PhaseContracted,
	}
	if len(state.History) < len(want) {
		return fmt.Errorf("%w: phase journal has %d history events, want at least %d", schemaupgrade.ErrCheckpointMismatch, len(state.History), len(want))
	}
	for i, phase := range want {
		if state.History[i].Phase != phase {
			return fmt.Errorf("%w: phase history event %d is %s, want %s", schemaupgrade.ErrCheckpointMismatch, i, state.History[i].Phase, phase)
		}
	}
	if state.Phase != schemaupgrade.PhaseContracted {
		return fmt.Errorf("%w: phase journal ended at %s, want %s", schemaupgrade.ErrCheckpointMismatch, state.Phase, schemaupgrade.PhaseContracted)
	}
	return nil
}

func validateMigrationPhasePlan(declared migrationPhasePlanFile, files []migrations.File, digest string) error {
	expectedID := releaseVersion(files, digest)
	if strings.TrimSpace(declared.MigrationID) == "" || declared.MigrationID != expectedID || declared.ManifestDigest != digest {
		return fmt.Errorf("%w: declared migration_id=%q manifest_digest=%q expected migration_id=%q manifest_digest=%q", errMigrationIdentityMismatch, declared.MigrationID, declared.ManifestDigest, expectedID, digest)
	}
	return nil
}
