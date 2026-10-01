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
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

func TestTodo_SVC_013_PhasePlanIdentity(t *testing.T) {
	files, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := migrations.ArtifactDigest()
	if err != nil {
		t.Fatal(err)
	}
	valid := migrationPhasePlanFile{
		MigrationID:    releaseVersion(files, digest),
		ManifestDigest: digest,
	}
	if err := validateMigrationPhasePlan(valid, files, digest); err != nil {
		t.Fatalf("valid phase plan rejected: %v", err)
	}

	t.Run("manifest_drift_refused", func(t *testing.T) {
		invalid := valid
		invalid.ManifestDigest = strings.Repeat("0", len(digest))
		if !errors.Is(validateMigrationPhasePlan(invalid, files, digest), errMigrationIdentityMismatch) {
			t.Fatal("phase plan accepted a different migration manifest")
		}
	})
	t.Run("release_identity_drift_refused", func(t *testing.T) {
		invalid := valid
		invalid.MigrationID = "p1a-00001-stale"
		if !errors.Is(validateMigrationPhasePlan(invalid, files, digest), errMigrationIdentityMismatch) {
			t.Fatal("phase plan accepted a different migration identity")
		}
	})
}

func TestTodo_SVC_013_PhaseHistory(t *testing.T) {
	state, err := schemaupgrade.New(upgradeTestPlan(), time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Expand(time.Date(2026, 9, 29, 12, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := state.ResumeBackfill(upgradeTestRows(), 10, time.Date(2026, 9, 29, 12, 2, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := state.CompareShadow(upgradeTestRows(), upgradeTestRows(), time.Date(2026, 9, 29, 12, 3, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := state.Cutover(3, time.Date(2026, 9, 29, 12, 4, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := state.Contract(time.Date(2026, 9, 29, 12, 5, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := verifyMigrationPhaseHistory(state); err != nil {
		t.Fatalf("valid phase history rejected: %v", err)
	}
	state.History[3].Phase = schemaupgrade.PhaseCutover
	if !errors.Is(verifyMigrationPhaseHistory(state), schemaupgrade.ErrCheckpointMismatch) {
		t.Fatal("phase history accepted a skipped shadow gate")
	}
}

func TestTodo_SVC_013_PhaseCommandValidation(t *testing.T) {
	fields := migrateConfigFields()
	noEnv := func(string) (string, bool) { return "", false }
	files, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := migrations.ArtifactDigest()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")
	rowsPath := filepath.Join(dir, "rows.json")
	planRaw, err := json.Marshal(migrationPhasePlanFile{MigrationID: releaseVersion(files, digest), ManifestDigest: digest, Plan: upgradeTestPlan()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, planRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	rowsRaw, err := json.Marshal([]schemaupgrade.Row{{Key: "a", Digest: "da"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rowsPath, rowsRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-database-url=postgres://x", "-journal-path=" + filepath.Join(dir, "journal.json"), "-upgrade-plan=" + planPath, "-upgrade-rows=" + rowsPath}
	v, err := bootstrap.ParseConfig(args, noEnv, fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateConfig("phase")(v); err != nil {
		t.Fatalf("complete phase config rejected: %v", err)
	}

	missingDatabase, err := bootstrap.ParseConfig(args[1:], noEnv, fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateConfig("phase")(missingDatabase); err == nil {
		t.Fatal("phase accepted a missing database URL")
	}

	s := spec("phase", args[1:])
	s.Getenv = noEnv
	s.Logger = discardLogger()
	s.Stdout = &bytes.Buffer{}
	s.Stderr = &bytes.Buffer{}
	if code := bootstrap.Run(context.Background(), s); code != bootstrap.ExitConfigError {
		t.Fatalf("unreachable phase config exit code=%d, want %d", code, bootstrap.ExitConfigError)
	}
}
