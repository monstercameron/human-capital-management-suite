package pgstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
)

// MutateLifecycle implements app.LifecycleMutator for SubmitIntent,
// CancelIntent and SupersedeIntent (EP-INTENT-003). It is a compare-and-swap
// against the caller's own last-read instance version, built the same way
// [Store.BindOutcome] already is: the projected five lifecycle columns and
// the commit/repair references move, the row's instance_version advances by
// exactly one, and the intent's own ledger envelope (the immutable creation
// fact AppendIntent wrote) is never touched. A concurrent writer that moved
// the row first is reported as [app.ErrOutcomeProjectionConflict], the same
// sentinel BindOutcome's own CAS conflict uses, so a caller does not have to
// learn a second conflict shape for this second kind of durable write.
func (s *Store) MutateLifecycle(ctx context.Context, m app.LifecycleMutation) (app.IntentRecord, error) {
	if m.Tenant == "" {
		return app.IntentRecord{}, fmt.Errorf("pgstore: mutate lifecycle requires a tenant")
	}
	if m.ExpectedInstanceVersion == 0 {
		return app.IntentRecord{}, fmt.Errorf("pgstore: mutate lifecycle requires an instance version")
	}
	if err := m.Lifecycle.Validate(); err != nil {
		return app.IntentRecord{}, fmt.Errorf("pgstore: mutate lifecycle: %w", err)
	}
	intentID, err := uuid.Parse(m.IntentID)
	if err != nil {
		return app.IntentRecord{}, fmt.Errorf("pgstore: mutate lifecycle intent id: %w", err)
	}
	tenantID := TenantID(m.Tenant)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return app.IntentRecord{}, fmt.Errorf("pgstore: begin mutate lifecycle: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return app.IntentRecord{}, err
	}

	var version int64
	err = tx.QueryRow(ctx, `
		SELECT instance_version FROM intent_instance
		WHERE tenant_id = $1 AND intent_id = $2
		FOR UPDATE`, tenantID, intentID).Scan(&version)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return app.IntentRecord{}, fmt.Errorf("pgstore: %w", app.ErrIntentNotFound)
		}
		return app.IntentRecord{}, fmt.Errorf("pgstore: load intent lifecycle projection: %w", err)
	}
	if uint64(version) != m.ExpectedInstanceVersion {
		return app.IntentRecord{}, fmt.Errorf("%w: stored instance version %d, expected %d",
			app.ErrOutcomeProjectionConflict, version, m.ExpectedInstanceVersion)
	}

	recordedAt := m.RecordedAt.UTC()
	if err := applyInstanceLifecycle(ctx, tx, tenantID, intentID, m.ExpectedInstanceVersion+1, m.Lifecycle, recordedAt); err != nil {
		return app.IntentRecord{}, err
	}
	updated, err := tx.Exec(ctx, `
		UPDATE intent_instance
		SET commit_receipt_ref = COALESCE(NULLIF($3, ''), commit_receipt_ref),
			repair_ref = COALESCE(NULLIF($4, ''), repair_ref)
		WHERE tenant_id = $1 AND intent_id = $2 AND instance_version = $5`,
		tenantID, intentID, m.CommitReceiptRef, m.RepairRef, int64(m.ExpectedInstanceVersion+1))
	if err != nil {
		return app.IntentRecord{}, fmt.Errorf("pgstore: mutate intent lifecycle: %w", err)
	}
	if updated != 1 {
		return app.IntentRecord{}, fmt.Errorf("%w: concurrent lifecycle mutation", app.ErrOutcomeProjectionConflict)
	}
	if err := tx.Commit(ctx); err != nil {
		return app.IntentRecord{}, fmt.Errorf("pgstore: commit mutate lifecycle: %w", err)
	}
	return s.LoadIntent(ctx, m.Tenant, m.IntentID)
}
