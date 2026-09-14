package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// Executor is the minimal database capability this package needs. A
// [dbport.Tx] and a [dbport.Conn] both satisfy it.
//
// Every Record* call issues two statements -- the instance-version
// compare-and-set and the row write -- and this package never opens a
// transaction of its own, so pass a [dbport.Tx] the caller began and will
// finish. On a bare connection those two statements autocommit independently:
// the compare-and-set would stand even if the row write failed, and the next
// writer would be told its version is stale over a transition that never
// happened. Reads are single statements and need no transaction.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// Port is the surface a durable driver calls. It is deliberately small and
// deliberately passive: Create, Record, Load.
//
// There is no Start and no Advance. Deciding what runs next is the scheduler's
// job, and WF-RUN-000 gates the scheduler behind the P1B re-evaluation; this
// package stores the state a driver decided on so that a process restart does
// not lose it.
type Port interface {
	CreateInstance(ctx context.Context, ex Executor, inst Instance) (Instance, error)
	LoadInstance(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) (Instance, error)
	RecordInstanceState(ctx context.Context, ex Executor, t InstanceTransition) (Instance, error)

	RecordNodeExecution(ctx context.Context, ex Executor, n NodeExecution, expectedInstanceVersion int64) (NodeExecution, int64, error)
	RecordNodeTransition(ctx context.Context, ex Executor, t NodeTransition) (NodeExecution, int64, error)
	LoadNodeExecution(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, nodeID string, attempt int) (NodeExecution, error)
	LoadNodeExecutions(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]NodeExecution, error)
}

// Store implements [Port] over migrations/00016_workflow_runtime.sql. It holds
// no state; every method takes its [Executor] explicitly.
type Store struct{}

var _ Port = Store{}

const instanceColumns = `tenant_id, instance_id, cell_id, workflow_id, workflow_version,
	compiled_plan_hash, business_subject_refs, business_transaction_id, execution_mode,
	runtime_status, completion_dimensions, input_ref, variable_revision_head, current_node_ids,
	effective_context_ref, last_checkpoint_ref, instance_version, correlation_id,
	created_at, started_at, completed_at`

const nodeColumns = `tenant_id, node_execution_id, instance_id, node_id, attempt, step_type, status,
	input_snapshot_ref, output_artifact_ref, capability_execution_id, authorization_decision_id,
	decision_id, human_task_id, agent_execution_id, proposal_ref, baseline_ref, policy_ref,
	repair_ref, effect_refs, retry_policy_ref, error_class, trace_id,
	started_at, completed_at, recorded_at`

// CreateInstance stores a new instance at version 1.
//
// The instance is stored exactly as given: this package never invents a
// status, a frontier or a clock reading. An instance whose identity already
// exists collides on the primary key rather than overwriting, because two
// drivers that both believe they created an instance is a fact the caller
// needs to hear about.
func (Store) CreateInstance(ctx context.Context, ex Executor, inst Instance) (Instance, error) {
	if err := inst.Validate(); err != nil {
		return Instance{}, err
	}
	dims, err := json.Marshal(inst.CompletionDimensions)
	if err != nil {
		return Instance{}, wrap(CodeInvalidRecord, inst.InstanceID.String(), "", err,
			"encode completion dimensions")
	}
	row := ex.QueryRow(ctx, `
		INSERT INTO workflow_instance (`+instanceColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
		RETURNING `+instanceColumns,
		inst.TenantID, inst.InstanceID, inst.CellID, inst.WorkflowID, int32(inst.WorkflowVersion),
		inst.CompiledPlanHash, textArray(inst.BusinessSubjectRefs), inst.BusinessTransactionID,
		string(inst.ExecutionMode), string(inst.RuntimeStatus), dims, inst.InputRef,
		inst.VariableRevisionHead, textArray(inst.CurrentNodeIDs),
		nullableText(inst.EffectiveContextRef), nullableText(inst.LastCheckpointRef),
		inst.InstanceVersion, inst.CorrelationID,
		inst.CreatedAt.UTC(), utcOrNil(inst.StartedAt), utcOrNil(inst.CompletedAt))

	stored, err := scanInstance(row)
	if err != nil {
		return Instance{}, wrap(CodeStorageFailed, inst.InstanceID.String(), "", err,
			"insert workflow instance")
	}
	return stored, nil
}

// LoadInstance reads one instance by identity. A missing row and a row
// belonging to another tenant are the same answer, [CodeInstanceNotFound]:
// this method never discloses that an instance it may not see exists.
func (Store) LoadInstance(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) (Instance, error) {
	row := ex.QueryRow(ctx,
		`SELECT `+instanceColumns+` FROM workflow_instance WHERE tenant_id = $1 AND instance_id = $2`,
		tenantID, instanceID)
	inst, err := scanInstance(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return Instance{}, refuse(CodeInstanceNotFound, instanceID.String(), "", "no such instance")
		}
		return Instance{}, wrap(CodeStorageFailed, instanceID.String(), "", err, "read workflow instance")
	}
	return inst, nil
}

// RecordInstanceState applies one legal transition under an optimistic version
// check, writing the status, the frontier, the variable revision head, the
// pinned context, the checkpoint and the lifecycle dimensions together.
//
// A writer whose ExpectedVersion has been overtaken is refused with
// [CodeStaleInstance] and mutates nothing -- that refusal, not a lease, is the
// whole of this package's concurrency control.
func (s Store) RecordInstanceState(ctx context.Context, ex Executor, t InstanceTransition) (ret0 Instance, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.instance_transition", t)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := t.Validate(); err != nil {
		return Instance{}, err
	}
	current, err := s.LoadInstance(ctx, ex, t.TenantID, t.InstanceID)
	if err != nil {
		return Instance{}, err
	}
	if current.InstanceVersion != t.ExpectedVersion {
		return Instance{}, staleError(t.InstanceID, "", t.ExpectedVersion, current.InstanceVersion)
	}
	if !LegalInstanceTransition(current.RuntimeStatus, t.Status) {
		return Instance{}, refuse(CodeIllegalTransition, t.InstanceID.String(), "",
			"instance may not move from %s to %s", string(current.RuntimeStatus), string(t.Status))
	}
	dims, err := json.Marshal(t.CompletionDimensions)
	if err != nil {
		return Instance{}, wrap(CodeInvalidRecord, t.InstanceID.String(), "", err,
			"encode completion dimensions")
	}

	// The version guard is repeated in the WHERE clause rather than trusted
	// from the read above: between the read and this statement another writer
	// may have committed, and it is the database that must decide who won.
	row := ex.QueryRow(ctx, `
		UPDATE workflow_instance SET
			runtime_status = $1,
			current_node_ids = $2,
			variable_revision_head = $3,
			effective_context_ref = $4,
			last_checkpoint_ref = $5,
			completion_dimensions = $6,
			started_at = COALESCE($7, started_at),
			completed_at = COALESCE($8, completed_at),
			instance_version = instance_version + 1
		WHERE tenant_id = $9 AND instance_id = $10 AND instance_version = $11
		RETURNING `+instanceColumns,
		string(t.Status), textArray(t.CurrentNodeIDs), t.VariableRevisionHead,
		nullableText(t.EffectiveContextRef), nullableText(t.LastCheckpointRef), dims,
		utcOrNil(t.StartedAt), utcOrNil(t.CompletedAt),
		t.TenantID, t.InstanceID, t.ExpectedVersion)

	updated, err := scanInstance(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return Instance{}, s.explainLostUpdate(ctx, ex, t.TenantID, t.InstanceID, "", t.ExpectedVersion)
		}
		return Instance{}, wrap(CodeStorageFailed, t.InstanceID.String(), "", err,
			"update workflow instance")
	}
	return updated, nil
}

// RecordNodeExecution stores one node execution attempt and bumps the owning
// instance's version in the same transaction, so a node write and the
// instance-version check the spec requires cannot come apart.
//
// It returns the stored record and the instance version a subsequent writer
// must now hold.
func (s Store) RecordNodeExecution(
	ctx context.Context, ex Executor, n NodeExecution, expectedInstanceVersion int64,
) (NodeExecution, int64, error) {
	if err := n.Validate(); err != nil {
		return NodeExecution{}, 0, err
	}
	if expectedInstanceVersion < 1 {
		return NodeExecution{}, 0, refuse(CodeInvalidRecord, n.InstanceID.String(), n.NodeID,
			"expected instance version must be at least 1")
	}
	version, err := s.bumpInstanceVersion(ctx, ex, n.TenantID, n.InstanceID, n.NodeID, expectedInstanceVersion)
	if err != nil {
		return NodeExecution{}, 0, err
	}
	// A zero RecordedAt binds as NULL so the column's own default (now())
	// supplies it. This package never reads a wall clock itself; the database
	// stamping its own arrival time is a different thing from Go inventing one.
	row := ex.QueryRow(ctx, `
		INSERT INTO workflow_node_execution (`+nodeColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19,
			$20, $21, $22, $23, $24, COALESCE($25, now()))
		RETURNING `+nodeColumns,
		n.TenantID, n.NodeExecutionID, n.InstanceID, n.NodeID, int32(n.Attempt),
		string(n.StepType), string(n.Status),
		nullableText(n.InputSnapshotRef), nullableText(n.OutputArtifactRef),
		nullableText(n.Refs.CapabilityExecutionID), nullableText(n.Refs.AuthorizationDecisionID),
		nullableText(n.Refs.DecisionID), nullableText(n.Refs.HumanTaskID),
		nullableText(n.Refs.AgentExecutionID), nullableText(n.Refs.ProposalRef),
		nullableText(n.Refs.BaselineRef), nullableText(n.Refs.PolicyRef),
		nullableText(n.Refs.RepairRef), textArray(n.Refs.EffectRefs),
		nullableText(n.Refs.RetryPolicyRef), nullableText(n.ErrorClass), nullableText(n.TraceID),
		utcOrNil(n.StartedAt), utcOrNil(n.CompletedAt), zeroTimeOrNil(n.RecordedAt))

	stored, err := scanNodeExecution(row)
	if err != nil {
		return NodeExecution{}, 0, wrap(CodeStorageFailed, n.InstanceID.String(), n.NodeID, err,
			"insert node execution attempt %d", n.Attempt)
	}
	return stored, version, nil
}

// RecordNodeTransition moves an already-stored attempt to a new status under
// the same optimistic instance-version check, refusing a status change the
// node state machine does not allow.
func (s Store) RecordNodeTransition(ctx context.Context, ex Executor, t NodeTransition) (ret0 NodeExecution, ret1 int64, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.node_transition", t)
	defer func() { observe.DoneWith(obsOp, retErr, ret0, ret1) }()
	if err := t.Validate(); err != nil {
		return NodeExecution{}, 0, err
	}
	current, err := s.LoadNodeExecution(ctx, ex, t.TenantID, t.InstanceID, t.NodeID, t.Attempt)
	if err != nil {
		return NodeExecution{}, 0, err
	}
	if !LegalNodeTransition(current.Status, t.Status) {
		return NodeExecution{}, 0, refuse(CodeIllegalTransition, t.InstanceID.String(), t.NodeID,
			"node attempt %d may not move from %s to %s", t.Attempt, string(current.Status), string(t.Status))
	}
	version, err := s.bumpInstanceVersion(ctx, ex, t.TenantID, t.InstanceID, t.NodeID, t.ExpectedInstanceVersion)
	if err != nil {
		return NodeExecution{}, 0, err
	}

	row := ex.QueryRow(ctx, `
		UPDATE workflow_node_execution SET
			status = $1,
			output_artifact_ref = COALESCE($2, output_artifact_ref),
			capability_execution_id = COALESCE($3, capability_execution_id),
			authorization_decision_id = COALESCE($4, authorization_decision_id),
			decision_id = COALESCE($5, decision_id),
			human_task_id = COALESCE($6, human_task_id),
			agent_execution_id = COALESCE($7, agent_execution_id),
			proposal_ref = COALESCE($8, proposal_ref),
			baseline_ref = COALESCE($9, baseline_ref),
			policy_ref = COALESCE($10, policy_ref),
			repair_ref = COALESCE($11, repair_ref),
			effect_refs = CASE WHEN cardinality($12::text[]) > 0 THEN $12::text[] ELSE effect_refs END,
			retry_policy_ref = COALESCE($13, retry_policy_ref),
			error_class = COALESCE($14, error_class),
			trace_id = COALESCE($15, trace_id),
			completed_at = COALESCE($16, completed_at)
		WHERE tenant_id = $17 AND node_execution_id = $18
		RETURNING `+nodeColumns,
		string(t.Status), nullableText(t.OutputArtifactRef),
		nullableText(t.Refs.CapabilityExecutionID), nullableText(t.Refs.AuthorizationDecisionID),
		nullableText(t.Refs.DecisionID), nullableText(t.Refs.HumanTaskID),
		nullableText(t.Refs.AgentExecutionID), nullableText(t.Refs.ProposalRef),
		nullableText(t.Refs.BaselineRef), nullableText(t.Refs.PolicyRef),
		nullableText(t.Refs.RepairRef), textArray(t.Refs.EffectRefs),
		nullableText(t.Refs.RetryPolicyRef), nullableText(t.ErrorClass), nullableText(t.TraceID),
		utcOrNil(t.CompletedAt),
		t.TenantID, current.NodeExecutionID)

	updated, err := scanNodeExecution(row)
	if err != nil {
		return NodeExecution{}, 0, wrap(CodeStorageFailed, t.InstanceID.String(), t.NodeID, err,
			"update node execution attempt %d", t.Attempt)
	}
	return updated, version, nil
}

// LoadNodeExecution reads one attempt by its defining tuple.
func (Store) LoadNodeExecution(
	ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, nodeID string, attempt int,
) (NodeExecution, error) {
	row := ex.QueryRow(ctx, `SELECT `+nodeColumns+` FROM workflow_node_execution
		WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND attempt = $4`,
		tenantID, instanceID, nodeID, int32(attempt))
	n, err := scanNodeExecution(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return NodeExecution{}, refuse(CodeNodeExecutionNotFound, instanceID.String(), nodeID,
				"no attempt %d recorded", attempt)
		}
		return NodeExecution{}, wrap(CodeStorageFailed, instanceID.String(), nodeID, err,
			"read node execution attempt %d", attempt)
	}
	return n, nil
}

// LoadNodeExecutions reads every recorded attempt of an instance, ordered by
// node id then attempt so a projection over them is deterministic.
func (Store) LoadNodeExecutions(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]NodeExecution, error) {
	rows, err := ex.Query(ctx, `SELECT `+nodeColumns+` FROM workflow_node_execution
		WHERE tenant_id = $1 AND instance_id = $2 ORDER BY node_id, attempt`,
		tenantID, instanceID)
	if err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "read node executions")
	}
	defer rows.Close()

	out := []NodeExecution{}
	for rows.Next() {
		n, scanErr := scanNodeExecution(rows)
		if scanErr != nil {
			return nil, wrap(CodeStorageFailed, instanceID.String(), "", scanErr, "scan node execution")
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "iterate node executions")
	}
	return out, nil
}

// bumpInstanceVersion is the one compare-and-set every runtime write goes
// through. It returns the new version, or the typed refusal that explains why
// the write may not proceed.
func (s Store) bumpInstanceVersion(
	ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, nodeID string, expected int64,
) (int64, error) {
	var version int64
	err := ex.QueryRow(ctx, `
		UPDATE workflow_instance SET instance_version = instance_version + 1
		WHERE tenant_id = $1 AND instance_id = $2 AND instance_version = $3
		RETURNING instance_version`,
		tenantID, instanceID, expected).Scan(&version)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return 0, s.explainLostUpdate(ctx, ex, tenantID, instanceID, nodeID, expected)
		}
		return 0, wrap(CodeStorageFailed, instanceID.String(), nodeID, err, "bump instance version")
	}
	return version, nil
}

// explainLostUpdate distinguishes the two reasons a version-guarded update
// matched nothing: the instance is gone (or not this tenant's), or another
// writer got there first.
func (s Store) explainLostUpdate(
	ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, nodeID string, expected int64,
) error {
	var stored int64
	err := ex.QueryRow(ctx,
		`SELECT instance_version FROM workflow_instance WHERE tenant_id = $1 AND instance_id = $2`,
		tenantID, instanceID).Scan(&stored)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return refuse(CodeInstanceNotFound, instanceID.String(), nodeID, "no such instance")
		}
		return wrap(CodeStorageFailed, instanceID.String(), nodeID, err, "read instance version")
	}
	return staleError(instanceID, nodeID, expected, stored)
}

func staleError(instanceID uuid.UUID, nodeID string, expected, stored int64) error {
	return refuse(CodeStaleInstance, instanceID.String(), nodeID,
		"writer holds instance version %d, stored version is %d", expected, stored)
}

func scanInstance(row dbport.Row) (Instance, error) {
	var (
		inst          Instance
		version       int32
		mode          string
		status        string
		dims          []byte
		subjects      []string
		nodes         []string
		txID          *uuid.UUID
		contextRef    *string
		checkpointRef *string
		startedAt     *time.Time
		completedAt   *time.Time
	)
	err := row.Scan(
		&inst.TenantID, &inst.InstanceID, &inst.CellID, &inst.WorkflowID, &version,
		&inst.CompiledPlanHash, &subjects, &txID, &mode,
		&status, &dims, &inst.InputRef, &inst.VariableRevisionHead, &nodes,
		&contextRef, &checkpointRef, &inst.InstanceVersion, &inst.CorrelationID,
		&inst.CreatedAt, &startedAt, &completedAt)
	if err != nil {
		return Instance{}, err
	}
	inst.WorkflowVersion = uint32(version)
	inst.ExecutionMode = workflow.ExecutionMode(mode)
	inst.RuntimeStatus = InstanceStatus(status)
	inst.BusinessSubjectRefs = subjects
	inst.BusinessTransactionID = txID
	inst.CurrentNodeIDs = nodes
	inst.EffectiveContextRef = derefText(contextRef)
	inst.LastCheckpointRef = derefText(checkpointRef)
	inst.StartedAt = utcOrNilTime(startedAt)
	inst.CompletedAt = utcOrNilTime(completedAt)
	inst.CreatedAt = inst.CreatedAt.UTC()
	if len(dims) > 0 {
		if err := json.Unmarshal(dims, &inst.CompletionDimensions); err != nil {
			return Instance{}, err
		}
	}
	return inst, nil
}

func scanNodeExecution(row dbport.Row) (NodeExecution, error) {
	var (
		n           NodeExecution
		attempt     int32
		stepType    string
		status      string
		inputRef    *string
		outputRef   *string
		capExec     *string
		authzID     *string
		decisionID  *string
		taskID      *string
		agentID     *string
		proposal    *string
		baseline    *string
		policy      *string
		repair      *string
		effectRefs  []string
		retryPolicy *string
		errorClass  *string
		traceID     *string
		startedAt   *time.Time
		completedAt *time.Time
	)
	err := row.Scan(
		&n.TenantID, &n.NodeExecutionID, &n.InstanceID, &n.NodeID, &attempt, &stepType, &status,
		&inputRef, &outputRef, &capExec, &authzID,
		&decisionID, &taskID, &agentID, &proposal, &baseline, &policy,
		&repair, &effectRefs, &retryPolicy, &errorClass, &traceID,
		&startedAt, &completedAt, &n.RecordedAt)
	if err != nil {
		return NodeExecution{}, err
	}
	n.Attempt = int(attempt)
	n.StepType = workflow.StepType(stepType)
	n.Status = NodeStatus(status)
	n.InputSnapshotRef = derefText(inputRef)
	n.OutputArtifactRef = derefText(outputRef)
	n.Refs = GovernanceRefs{
		CapabilityExecutionID:   derefText(capExec),
		AuthorizationDecisionID: derefText(authzID),
		DecisionID:              derefText(decisionID),
		HumanTaskID:             derefText(taskID),
		AgentExecutionID:        derefText(agentID),
		ProposalRef:             derefText(proposal),
		BaselineRef:             derefText(baseline),
		PolicyRef:               derefText(policy),
		RepairRef:               derefText(repair),
		EffectRefs:              effectRefs,
		RetryPolicyRef:          derefText(retryPolicy),
	}
	n.ErrorClass = derefText(errorClass)
	n.TraceID = derefText(traceID)
	n.StartedAt = utcOrNilTime(startedAt)
	n.CompletedAt = utcOrNilTime(completedAt)
	n.RecordedAt = n.RecordedAt.UTC()
	return n, nil
}

// nullableText binds "" as SQL NULL, so an unset optional reference is absent
// rather than an empty string that later reads as a real value.
func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func derefText(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// textArray binds a nil slice as an empty array rather than NULL: both array
// columns are NOT NULL with an empty default, and "no frontier" is an empty
// set, not an unknown one.
func textArray(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func utcOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

func utcOrNilTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func zeroTimeOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}
