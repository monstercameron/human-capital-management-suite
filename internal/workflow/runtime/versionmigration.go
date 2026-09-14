package runtime

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// VersionMigration is the one additive write internal/workflow/migrate needs
// and this package does not otherwise expose: rewriting a PAUSED instance's
// pinned compiled-plan identity and frontier under the same optimistic
// version fence every other write in this package uses (WF-RUN-018).
//
// It is deliberately its own entry point rather than a new field on
// [InstanceTransition]/[Store.RecordInstanceState]: that path is WF-RUN-001's
// general instance-status transition, gated by [LegalInstanceTransition], and
// never touches workflow_version or compiled_plan_hash. A version migration
// changes exactly those two columns plus the frontier, and deliberately
// leaves the status itself unchanged -- PAUSED stays PAUSED; the caller
// resumes separately through [ResumeFromPause] once it is satisfied the
// migration committed. Deciding *whether* a migration may happen -- the
// preview classification, the approval, the safe-point check -- belongs
// entirely to internal/workflow/migrate; this method only performs the write
// once that package has decided yes.
type VersionMigration struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	// ExpectedVersion fences the write exactly like every other Record* call
	// in this package: a version already overtaken changes nothing.
	ExpectedVersion int64

	NewWorkflowVersion  uint32
	NewCompiledPlanHash string
	// NewCurrentNodeIDs replaces the instance's frontier outright. The caller
	// has already decided the new frontier (unchanged, or the bridged target
	// node) and recorded whatever node executions that frontier requires;
	// this method does not derive or validate it against any compiled plan.
	NewCurrentNodeIDs []string
}

func (m VersionMigration) validate() error {
	id := m.InstanceID.String()
	switch {
	case m.TenantID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "", "tenant id must not be the nil UUID")
	case m.InstanceID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "", "instance id must not be the nil UUID")
	case m.ExpectedVersion < 1:
		return refuse(CodeInvalidRecord, id, "", "expected instance version must be at least 1")
	case m.NewWorkflowVersion == 0:
		return refuse(CodeInvalidRecord, id, "", "new workflow version must be at least 1")
	case m.NewCompiledPlanHash == "":
		return refuse(CodeInvalidRecord, id, "", "new compiled plan hash is required")
	case len(m.NewCurrentNodeIDs) == 0:
		return refuse(CodeInvalidRecord, id, "", "a version migration must leave the instance a frontier")
	}
	for _, node := range m.NewCurrentNodeIDs {
		if node == "" {
			return refuse(CodeInvalidRecord, id, "", "new frontier carries an empty node id")
		}
	}
	return nil
}

// RecordVersionMigration atomically rewrites a PAUSED instance's pinned
// compiled-plan version, digest and frontier, and nothing else: the runtime
// status stays PAUSED, every other column (variables, context ref, checkpoint
// ref, dimensions) carries forward untouched. It refuses an instance that is
// not currently PAUSED -- WF-RUN-018 requires a reviewed safe point, and a
// running instance is never one -- and, like every other write here, a
// version the caller no longer holds.
func (s Store) RecordVersionMigration(ctx context.Context, ex Executor, m VersionMigration) (ret0 Instance, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.version_migration", m)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := m.validate(); err != nil {
		return Instance{}, err
	}
	row := ex.QueryRow(ctx, `
		UPDATE workflow_instance SET
			workflow_version = $1,
			compiled_plan_hash = $2,
			current_node_ids = $3,
			instance_version = instance_version + 1
		WHERE tenant_id = $4 AND instance_id = $5 AND instance_version = $6 AND runtime_status = 'PAUSED'
		RETURNING `+instanceColumns,
		int32(m.NewWorkflowVersion), m.NewCompiledPlanHash, textArray(m.NewCurrentNodeIDs),
		m.TenantID, m.InstanceID, m.ExpectedVersion)

	updated, err := scanInstance(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return Instance{}, s.explainVersionMigrationMiss(ctx, ex, m.TenantID, m.InstanceID, m.ExpectedVersion)
		}
		return Instance{}, wrap(CodeStorageFailed, m.InstanceID.String(), "", err, "record version migration")
	}
	return updated, nil
}

// explainVersionMigrationMiss distinguishes the three reasons
// [Store.RecordVersionMigration]'s guarded update matched no row: the
// instance does not exist (or is not this tenant's), another writer's version
// bump overtook it, or it exists at the expected version but is not PAUSED.
// [Store.explainLostUpdate] alone cannot tell the third case from the first
// two, since it does not filter on status.
func (s Store) explainVersionMigrationMiss(
	ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, expected int64,
) error {
	var (
		stored int64
		status string
	)
	err := ex.QueryRow(ctx,
		`SELECT instance_version, runtime_status FROM workflow_instance WHERE tenant_id = $1 AND instance_id = $2`,
		tenantID, instanceID).Scan(&stored, &status)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return refuse(CodeInstanceNotFound, instanceID.String(), "", "no such instance")
		}
		return wrap(CodeStorageFailed, instanceID.String(), "", err, "read instance version and status")
	}
	if stored == expected && InstanceStatus(status) != InstancePaused {
		return refuse(CodeIllegalTransition, instanceID.String(), "",
			"instance is %s, not PAUSED; a version migration requires a paused safe point", status)
	}
	return staleError(instanceID, "", expected, stored)
}
