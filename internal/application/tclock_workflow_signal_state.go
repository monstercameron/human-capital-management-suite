package application

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// WorkflowInstanceVersionReader reads the current durable workflow instance
// version used to fence signal continuation. Implementations must scope the
// transaction to the supplied tenant before reading the instance row.
type WorkflowInstanceVersionReader interface {
	LoadWorkflowInstanceVersion(context.Context, uuid.UUID, uuid.UUID) (int64, error)
}

// PostgresWorkflowInstanceVersionReader is the production tenant-scoped
// implementation. It performs a read-only transaction and never accepts an
// instance from another tenant.
type PostgresWorkflowInstanceVersionReader struct{ Pool dbport.Beginner }

var _ WorkflowInstanceVersionReader = PostgresWorkflowInstanceVersionReader{}

// LoadWorkflowInstanceVersion returns the current instance version.
func (r PostgresWorkflowInstanceVersionReader) LoadWorkflowInstanceVersion(ctx context.Context, tenant, instance uuid.UUID) (int64, error) {
	if r.Pool == nil || tenant == uuid.Nil || instance == uuid.Nil {
		return 0, errors.New("workflow instance version: tenant, instance and pool are required")
	}
	tx, err := beginReadOnly(ctx, r.Pool)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return 0, err
	}
	var version int64
	err = tx.QueryRow(ctx, `SELECT instance_version FROM workflow_instance WHERE tenant_id=$1 AND instance_id=$2`, tenant, instance).Scan(&version)
	if err != nil {
		return 0, err
	}
	if version < 1 {
		return 0, errors.New("workflow instance version: stored version must be positive")
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return version, nil
}

type readonlyBeginner interface {
	BeginReadOnly(context.Context) (dbport.Tx, error)
}

func beginReadOnly(ctx context.Context, b dbport.Beginner) (dbport.Tx, error) {
	if ro, ok := b.(readonlyBeginner); ok {
		return ro.BeginReadOnly(ctx)
	}
	return b.Begin(ctx)
}
