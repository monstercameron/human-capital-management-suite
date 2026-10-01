package agentpersonastore

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrRolloutSchemaUnavailable reports that the installed schema cannot durably
// bind a rollout mutation to its exact preview and approval references.
var ErrRolloutSchemaUnavailable = errors.New("agentpersonastore: rollout reference columns are unavailable")

// RolloutMutation identifies one installation transition. PreviewRef and
// ApprovalRef are intentionally required together: a mutation must be bound
// to the exact reviewed preview, never to a selector reconstructed by a
// caller. ExpectedRevision provides the installation compare-and-swap fence.
type RolloutMutation struct {
	InstallationID   string
	ExpectedRevision int64
	PreviewRef       string
	ApprovalRef      string
	Reason           string
}

// RolloutSchema reports whether the database can retain exact rollout refs.
type RolloutSchema struct {
	MissingColumns []string
}

// Ready reports whether exact preview and approval refs can be persisted.
func (s RolloutSchema) Ready() bool { return len(s.MissingColumns) == 0 }

// RequiredRolloutMigration is the exact additive migration required before
// ref-bound rollout mutations can be enabled. It is deliberately not applied
// here because migration ownership belongs to the schema lane.
const RequiredRolloutMigration = `ALTER TABLE persona_installations
    ADD COLUMN rollout_preview_ref text NOT NULL DEFAULT '',
    ADD COLUMN rollout_approval_ref text NOT NULL DEFAULT '';
ALTER TABLE persona_installations
    ADD CONSTRAINT persona_installations_rollout_refs_pair CHECK
    ((rollout_preview_ref = '') = (rollout_approval_ref = ''));`

// CheckRolloutSchema discovers the ref columns without changing the database.
// The query remains tenant-independent because information_schema is metadata,
// while all subsequent installation mutations are tenant-bound.
func (s *TenantStore) CheckRolloutSchema(ctx context.Context) (RolloutSchema, error) {
	if s == nil || ctx == nil {
		return RolloutSchema{}, fmt.Errorf("%w: context and tenant store are required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return RolloutSchema{}, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT column_name FROM information_schema.columns
		WHERE table_schema=current_schema() AND table_name='persona_installations'
		AND column_name IN ('rollout_preview_ref','rollout_approval_ref')`)
	if err != nil {
		return RolloutSchema{}, fmt.Errorf("agentpersonastore: inspect rollout schema: %w", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return RolloutSchema{}, fmt.Errorf("agentpersonastore: scan rollout schema: %w", err)
		}
		seen[column] = true
	}
	if err := rows.Err(); err != nil {
		return RolloutSchema{}, fmt.Errorf("agentpersonastore: read rollout schema: %w", err)
	}
	missing := make([]string, 0, 2)
	for _, column := range []string{"rollout_preview_ref", "rollout_approval_ref"} {
		if !seen[column] {
			missing = append(missing, column)
		}
	}
	if err := commit(ctx, tx); err != nil {
		return RolloutSchema{}, err
	}
	return RolloutSchema{MissingColumns: missing}, nil
}

func validateRolloutMutation(m RolloutMutation) error {
	if strings.TrimSpace(m.InstallationID) == "" || m.ExpectedRevision <= 0 ||
		strings.TrimSpace(m.PreviewRef) == "" || strings.TrimSpace(m.ApprovalRef) == "" ||
		strings.TrimSpace(m.Reason) == "" {
		return fmt.Errorf("%w: installation, revision, preview ref, approval ref, and reason are required", ErrInvalid)
	}
	return nil
}

// PauseInstallation durably suspends one installation using an exact revision
// fence. It refuses until the schema can retain both exact rollout refs.
func (s *TenantStore) PauseInstallation(ctx context.Context, mutation RolloutMutation) (PersonaInstallation, error) {
	return s.mutateRollout(ctx, mutation, InstallationSuspended)
}

// RemoveInstallation durably retires one installation using an exact revision
// fence. Retiring one installation cannot affect another installation.
func (s *TenantStore) RemoveInstallation(ctx context.Context, mutation RolloutMutation) (PersonaInstallation, error) {
	return s.mutateRollout(ctx, mutation, InstallationRetired)
}

func (s *TenantStore) mutateRollout(ctx context.Context, mutation RolloutMutation, state InstallationState) (PersonaInstallation, error) {
	if s == nil || ctx == nil {
		return PersonaInstallation{}, fmt.Errorf("%w: context and tenant store are required", ErrInvalid)
	}
	if err := validateRolloutMutation(mutation); err != nil {
		return PersonaInstallation{}, err
	}
	schema, err := s.CheckRolloutSchema(ctx)
	if err != nil {
		return PersonaInstallation{}, err
	}
	if !schema.Ready() {
		return PersonaInstallation{}, fmt.Errorf("%w: missing %s", ErrRolloutSchemaUnavailable, strings.Join(schema.MissingColumns, ", "))
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return PersonaInstallation{}, err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE persona_installations
		SET state=$4,suspension_reason=$5,revision=revision+1,
			revocation_epoch=revocation_epoch+1,rollout_preview_ref=$6,
			rollout_approval_ref=$7,updated_at=now()
		WHERE tenant_id=$1 AND installation_id=$2 AND revision=$3
			AND state IN ('ACTIVE','SUSPENDED','RETIRED')`,
		s.tenantID, mutation.InstallationID, mutation.ExpectedRevision, state,
		mutation.Reason, mutation.PreviewRef, mutation.ApprovalRef)
	if err != nil {
		return PersonaInstallation{}, fmt.Errorf("agentpersonastore: rollout mutation: %w", err)
	}
	if result != 1 {
		return PersonaInstallation{}, fmt.Errorf("%w: stale or missing installation", ErrConflict)
	}
	if err := commit(ctx, tx); err != nil {
		return PersonaInstallation{}, err
	}
	return s.GetInstallation(ctx, mutation.InstallationID)
}
