package runtimestate

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// The sentinels the scheduling stores classify failures with. They are this
// package's own, so a caller distinguishes a race from a contradiction from an
// absence without reading a driver error string.
var (
	// ErrInvalid reports a row that is not internally consistent. It is
	// returned before any statement runs.
	ErrInvalid = errors.New("runtimestate: invalid row")

	// ErrDuplicate reports a row whose identity already exists: a second timer
	// for the same key, a redelivered signal, a second claim on a held item, a
	// duplicate approval slot.
	ErrDuplicate = errors.New("runtimestate: duplicate row")

	// ErrNotFound reports a row that does not exist.
	ErrNotFound = errors.New("runtimestate: not found")

	// ErrVersionConflict reports a compare-and-swap whose expected version is
	// not the stored one. Nothing was written.
	ErrVersionConflict = errors.New("runtimestate: version conflict")

	// ErrIllegalTransition reports a state transition the lifecycle does not
	// allow, refused before the statement runs.
	ErrIllegalTransition = errors.New("runtimestate: illegal transition")

	// ErrLeaseHeld reports an acquire against a resource whose lease is still
	// live in someone else's hands.
	ErrLeaseHeld = errors.New("runtimestate: lease held")

	// ErrFenceStale reports a write presented with a fence token older than the
	// resource's current one: the holder was superseded while it was away.
	ErrFenceStale = errors.New("runtimestate: fence token stale")
)

// Executor is the minimal database capability the scheduling stores need. A
// [dbport.Tx] and a [dbport.Conn] both satisfy it.
//
// Every method takes it explicitly rather than holding a handle: migration
// 00026's tables are row-level-security protected, so the caller has to have
// scoped its transaction with internal/data/tenancy.WithTenant first, and a
// store that opened its own connection could not guarantee that.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

func isNoRows(err error) bool { return errors.Is(err, dbport.ErrNoRows) }

func invalid(field, detail string) error {
	return fmt.Errorf("%w: %s: %s", ErrInvalid, field, detail)
}

// requireJSONObject rejects a body that is not a JSON object, which is what
// migration 00026's jsonb_typeof checks also require.
func requireJSONObject(field string, body json.RawMessage) error {
	if len(body) == 0 {
		return invalid(field, "body is absent")
	}
	if !json.Valid(body) {
		return invalid(field, "body is not valid JSON")
	}
	for _, b := range body {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '{':
			return nil
		default:
			return invalid(field, "body is not a JSON object")
		}
	}
	return invalid(field, "body is absent")
}

// Frontier entry states.
const (
	FrontierReady   = "READY"
	FrontierRunning = "RUNNING"
	FrontierWaiting = "WAITING"
	FrontierBlocked = "BLOCKED"
	FrontierLeft    = "LEFT"
)

// FrontierEntry is one workflow_frontier_entry row: one node's membership of an
// instance's execution frontier.
type FrontierEntry struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	NodeID     string

	State             string
	Sequence          uint64
	AdmittedAtVersion uint64
	EntryVersion      uint64

	EnteredAt time.Time
	LeftAt    time.Time
}

// FrontierStore reads and advances workflow_frontier_entry.
type FrontierStore struct{}

// Enter admits one node to an instance's frontier.
//
// A repeated admission of the same node, or a second entry taking a sequence
// another node already holds, is [ErrDuplicate]: the frontier is a set with a
// deterministic order, and both duplicates would make a replay ambiguous rather
// than merely untidy.
func (s FrontierStore) Enter(ctx context.Context, ex Executor, in FrontierEntry) error {
	if in.NodeID == "" {
		return invalid("node_id", "a frontier entry names a node")
	}
	if in.Sequence == 0 {
		return invalid("entry_sequence", "sequence starts at 1")
	}
	if in.AdmittedAtVersion == 0 {
		return invalid("admitted_at_version", "an entry records the instance version it was admitted at")
	}
	if in.EnteredAt.IsZero() {
		return invalid("entered_at", "timestamp is unset")
	}
	if in.State == "" {
		in.State = FrontierReady
	}
	if in.State == FrontierLeft {
		return invalid("entry_state", "a node cannot enter the frontier already having left it")
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO workflow_frontier_entry (
			tenant_id, instance_id, node_id, entry_state,
			entry_sequence, admitted_at_version, entry_version, entered_at)
		VALUES ($1, $2, $3, $4, $5, $6, 1, $7)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.InstanceID, in.NodeID, in.State,
		int64(in.Sequence), int64(in.AdmittedAtVersion), in.EnteredAt.UTC())
	if err != nil {
		return fmt.Errorf("runtimestate: admit %s to frontier of %s: %w", in.NodeID, in.InstanceID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: workflow_frontier_entry %s/%s", ErrDuplicate, in.InstanceID, in.NodeID)
	}
	return nil
}

// Leave moves one node out of the frontier under compare-and-swap.
func (s FrontierStore) Leave(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID,
	nodeID string, expectedVersion uint64, at time.Time,
) error {
	if expectedVersion == 0 {
		return invalid("expected_version", "a compare-and-swap needs the version it expects")
	}
	if at.IsZero() {
		return invalid("left_at", "timestamp is unset")
	}
	affected, err := ex.Exec(ctx, `
		UPDATE workflow_frontier_entry
		SET entry_state = $5, entry_version = entry_version + 1, left_at = $6
		WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND entry_version = $4
		  AND entry_state <> $5`,
		tenantID, instanceID, nodeID, int64(expectedVersion), FrontierLeft, at.UTC())
	if err != nil {
		return fmt.Errorf("runtimestate: retire %s from frontier of %s: %w", nodeID, instanceID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: workflow_frontier_entry %s/%s expected version %d",
			ErrVersionConflict, instanceID, nodeID, expectedVersion)
	}
	return nil
}

// Open returns the instance's frontier entries that have not left, in entry
// order.
func (s FrontierStore) Open(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]FrontierEntry, error) {
	rows, err := ex.Query(ctx, `
		SELECT tenant_id, instance_id, node_id, entry_state,
			entry_sequence, admitted_at_version, entry_version, entered_at, left_at
		FROM workflow_frontier_entry
		WHERE tenant_id = $1 AND instance_id = $2 AND entry_state <> $3
		ORDER BY entry_sequence`, tenantID, instanceID, FrontierLeft)
	if err != nil {
		return nil, fmt.Errorf("runtimestate: read frontier of %s: %w", instanceID, err)
	}
	defer rows.Close()

	var out []FrontierEntry
	for rows.Next() {
		var (
			e        FrontierEntry
			sequence int64
			admitted int64
			version  int64
			leftAt   *time.Time
		)
		if err := rows.Scan(&e.TenantID, &e.InstanceID, &e.NodeID, &e.State,
			&sequence, &admitted, &version, &e.EnteredAt, &leftAt); err != nil {
			return nil, fmt.Errorf("runtimestate: scan frontier entry: %w", err)
		}
		e.Sequence, e.AdmittedAtVersion, e.EntryVersion = uint64(sequence), uint64(admitted), uint64(version)
		e.EnteredAt = e.EnteredAt.UTC()
		if leftAt != nil {
			e.LeftAt = leftAt.UTC()
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runtimestate: read frontier of %s: %w", instanceID, err)
	}
	return out, nil
}

// Node output kinds.
const (
	OutputProposal      = "PROPOSAL"
	OutputSimulation    = "SIMULATION"
	OutputDecision      = "DECISION"
	OutputEffectReceipt = "EFFECT_RECEIPT"
	OutputObservation   = "OBSERVATION"
	OutputDocument      = "DOCUMENT"
)

var outputKinds = map[string]bool{
	OutputProposal: true, OutputSimulation: true, OutputDecision: true,
	OutputEffectReceipt: true, OutputObservation: true, OutputDocument: true,
}

// NodeOutput is one workflow_node_output row: the typed, immutable result a
// node execution produced.
type NodeOutput struct {
	TenantID        uuid.UUID
	NodeExecutionID uuid.UUID
	InstanceID      uuid.UUID

	Kind         string
	SchemaRef    string
	ArtifactRef  string
	OutputDigest string

	ProducedAt time.Time
}

// OutputStore writes and reads workflow_node_output.
type OutputStore struct{}

// Record inserts one immutable node output. A second output for the same node
// execution is [ErrDuplicate], not an update: a completed node's result is a
// fact, and the table's forbid_mutation trigger refuses the rewrite even to the
// owning role.
func (s OutputStore) Record(ctx context.Context, ex Executor, in NodeOutput) error {
	if !outputKinds[in.Kind] {
		return invalid("output_kind", "kind is not a declared node output kind")
	}
	if in.SchemaRef == "" {
		return invalid("schema_ref", "a typed output names the schema it claims to be")
	}
	if in.ArtifactRef == "" {
		return invalid("artifact_ref", "value is absent")
	}
	if len(in.OutputDigest) != 64 {
		return invalid("output_digest", "digest is not 64 hex characters")
	}
	if in.ProducedAt.IsZero() {
		return invalid("produced_at", "timestamp is unset")
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO workflow_node_output (
			tenant_id, node_execution_id, instance_id,
			output_kind, schema_ref, artifact_ref, output_digest, produced_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.NodeExecutionID, in.InstanceID,
		in.Kind, in.SchemaRef, in.ArtifactRef, in.OutputDigest, in.ProducedAt.UTC())
	if err != nil {
		return fmt.Errorf("runtimestate: record output of %s: %w", in.NodeExecutionID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: workflow_node_output %s", ErrDuplicate, in.NodeExecutionID)
	}
	return nil
}

// Load returns one node execution's output.
func (s OutputStore) Load(ctx context.Context, ex Executor, tenantID, nodeExecutionID uuid.UUID) (NodeOutput, error) {
	var out NodeOutput
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, node_execution_id, instance_id,
			output_kind, schema_ref, artifact_ref, output_digest, produced_at
		FROM workflow_node_output
		WHERE tenant_id = $1 AND node_execution_id = $2`, tenantID, nodeExecutionID).Scan(
		&out.TenantID, &out.NodeExecutionID, &out.InstanceID,
		&out.Kind, &out.SchemaRef, &out.ArtifactRef, &out.OutputDigest, &out.ProducedAt)
	if err != nil {
		if isNoRows(err) {
			return NodeOutput{}, fmt.Errorf("%w: workflow_node_output %s", ErrNotFound, nodeExecutionID)
		}
		return NodeOutput{}, fmt.Errorf("runtimestate: load output of %s: %w", nodeExecutionID, err)
	}
	out.ProducedAt = out.ProducedAt.UTC()
	return out, nil
}

// Ready work states.
const (
	ReadyReady      = "READY"
	ReadyDispatched = "DISPATCHED"
	ReadyDone       = "DONE"
	ReadyCancelled  = "CANCELLED"
)

var readyTransitions = map[string][]string{
	ReadyReady:      {ReadyDispatched, ReadyCancelled},
	ReadyDispatched: {ReadyDone, ReadyCancelled, ReadyReady},
	ReadyDone:       nil,
	ReadyCancelled:  nil,
}

// ReadyWork is one workflow_ready_work row: a durable record that one attempt
// of one node may be started at or after EligibleAt.
type ReadyWork struct {
	TenantID    uuid.UUID
	ReadyWorkID uuid.UUID
	InstanceID  uuid.UUID
	NodeID      string
	Attempt     int

	State      string
	Priority   int
	EligibleAt time.Time
	Version    uint64

	EnqueuedAt  time.Time
	CompletedAt time.Time
	Causal      *CausalMetadata
}

// ReadyWorkStore writes and advances workflow_ready_work.
type ReadyWorkStore struct{}

// Enqueue records one unit of ready work. A second enqueue of the same node
// attempt is [ErrDuplicate]: one attempt is one unit of work, however many
// times an advancement is replayed.
func (s ReadyWorkStore) Enqueue(ctx context.Context, ex Executor, in ReadyWork) error {
	if in.NodeID == "" {
		return invalid("node_id", "ready work names a node")
	}
	if in.Attempt < 1 {
		return invalid("attempt", "attempt starts at 1")
	}
	if in.EligibleAt.IsZero() || in.EnqueuedAt.IsZero() {
		return invalid("eligible_at", "ready work carries both an enqueue and an eligibility instant")
	}
	if in.State == "" {
		in.State = ReadyReady
	}
	if in.Priority == 0 {
		in.Priority = 100
	}
	args := []any{in.TenantID, in.ReadyWorkID, in.InstanceID, in.NodeID, in.Attempt, in.State, in.Priority, in.EligibleAt.UTC(), in.EnqueuedAt.UTC()}
	args = append(args, causalValues(in.Causal)...)
	affected, err := ex.Exec(ctx, `
		INSERT INTO workflow_ready_work (
			tenant_id, ready_work_id, instance_id, node_id, attempt,
			ready_state, priority, eligible_at, ready_version, enqueued_at,
			correlation_id, causation_id, logical_operation_id, attempt_id, trace_id, trace_span_id, trace_flags, trace_state, trace_link_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT DO NOTHING`,
		args...)
	if err != nil {
		return fmt.Errorf("runtimestate: enqueue ready work for %s/%s: %w", in.InstanceID, in.NodeID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: workflow_ready_work %s/%s/%d", ErrDuplicate, in.InstanceID, in.NodeID, in.Attempt)
	}
	return nil
}

// Transition advances one ready-work row under compare-and-swap. The terminal
// states carry a completion instant, which the schema also requires.
func (s ReadyWorkStore) Transition(ctx context.Context, ex Executor, tenantID, readyWorkID uuid.UUID,
	expectedVersion uint64, next string, at time.Time,
) error {
	if expectedVersion == 0 {
		return invalid("expected_version", "a compare-and-swap needs the version it expects")
	}
	current, err := s.Load(ctx, ex, tenantID, readyWorkID)
	if err != nil {
		return err
	}
	if !allows(readyTransitions, current.State, next) {
		return fmt.Errorf("%w: workflow_ready_work %s: %s -> %s",
			ErrIllegalTransition, readyWorkID, current.State, next)
	}
	var completed any
	if next == ReadyDone || next == ReadyCancelled {
		if at.IsZero() {
			return invalid("completed_at", "a terminal ready-work state records when it ended")
		}
		completed = at.UTC()
	}
	affected, err := ex.Exec(ctx, `
		UPDATE workflow_ready_work
		SET ready_state = $4, ready_version = ready_version + 1, completed_at = $5
		WHERE tenant_id = $1 AND ready_work_id = $2 AND ready_version = $3`,
		tenantID, readyWorkID, int64(expectedVersion), next, completed)
	if err != nil {
		return fmt.Errorf("runtimestate: transition ready work %s: %w", readyWorkID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: workflow_ready_work %s expected version %d",
			ErrVersionConflict, readyWorkID, expectedVersion)
	}
	return nil
}

// Claim moves ready work to DISPATCHED and atomically gives this delivery a
// fresh attempt identity. The logical operation and correlation identities
// remain unchanged; only the delivery attempt changes on redelivery.
func (s ReadyWorkStore) Claim(ctx context.Context, ex Executor, tenantID, readyWorkID uuid.UUID,
	expectedVersion uint64,
) (ReadyWork, error) {
	if expectedVersion == 0 {
		return ReadyWork{}, invalid("expected_version", "a compare-and-swap needs the version it expects")
	}
	current, err := s.Load(ctx, ex, tenantID, readyWorkID)
	if err != nil {
		return ReadyWork{}, err
	}
	if !allows(readyTransitions, current.State, ReadyDispatched) {
		return ReadyWork{}, fmt.Errorf("%w: workflow_ready_work %s: %s -> %s",
			ErrIllegalTransition, readyWorkID, current.State, ReadyDispatched)
	}
	attemptID := uuid.NewString()
	affected, err := ex.Exec(ctx, `
		UPDATE workflow_ready_work
		SET ready_state = $4, ready_version = ready_version + 1,
			attempt_id = CASE WHEN logical_operation_id IS NULL THEN NULL ELSE $5 END,
			completed_at = NULL
		WHERE tenant_id = $1 AND ready_work_id = $2 AND ready_version = $3`,
		tenantID, readyWorkID, int64(expectedVersion), ReadyDispatched, attemptID)
	if err != nil {
		return ReadyWork{}, fmt.Errorf("runtimestate: claim ready work %s: %w", readyWorkID, err)
	}
	if affected == 0 {
		return ReadyWork{}, fmt.Errorf("%w: workflow_ready_work %s expected version %d",
			ErrVersionConflict, readyWorkID, expectedVersion)
	}
	return s.Load(ctx, ex, tenantID, readyWorkID)
}

// Load returns one ready-work row.
func (s ReadyWorkStore) Load(ctx context.Context, ex Executor, tenantID, readyWorkID uuid.UUID) (ReadyWork, error) {
	var (
		out                                                                   ReadyWork
		version                                                               int64
		completed                                                             *time.Time
		correlation, causation, logical, attempt, traceID, spanID, traceState *string
		flags                                                                 *int16
		expires                                                               *time.Time
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, ready_work_id, instance_id, node_id, attempt,
			ready_state, priority, eligible_at, ready_version, enqueued_at, completed_at,
			correlation_id, causation_id, logical_operation_id, attempt_id, trace_id, trace_span_id, trace_flags, trace_state, trace_link_expires_at
		FROM workflow_ready_work
		WHERE tenant_id = $1 AND ready_work_id = $2`, tenantID, readyWorkID).Scan(
		&out.TenantID, &out.ReadyWorkID, &out.InstanceID, &out.NodeID, &out.Attempt,
		&out.State, &out.Priority, &out.EligibleAt, &version, &out.EnqueuedAt, &completed,
		&correlation, &causation, &logical, &attempt, &traceID, &spanID, &flags, &traceState, &expires)
	if err != nil {
		if isNoRows(err) {
			return ReadyWork{}, fmt.Errorf("%w: workflow_ready_work %s", ErrNotFound, readyWorkID)
		}
		return ReadyWork{}, fmt.Errorf("runtimestate: load ready work %s: %w", readyWorkID, err)
	}
	out.Version = uint64(version)
	out.EligibleAt = out.EligibleAt.UTC()
	out.EnqueuedAt = out.EnqueuedAt.UTC()
	if completed != nil {
		out.CompletedAt = completed.UTC()
	}
	out.Causal = causalFromPointers(correlation, causation, logical, attempt, traceID, spanID, flags, traceState, expires)
	return out, nil
}

// Variable is one workflow_variable row.
type Variable struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	Name       string

	SchemaRef         string
	Value             json.RawMessage
	WrittenAtRevision uint64
	Version           uint64

	WrittenAt time.Time
}

// VariableStore reads and writes workflow_variable.
type VariableStore struct{}

// Put writes a variable. expectedVersion is 0 for a first write and the stored
// version for an overwrite; a mismatch is [ErrVersionConflict] and writes
// nothing, which is what stops two concurrent node completions from silently
// interleaving their variable writes.
func (s VariableStore) Put(ctx context.Context, ex Executor, in Variable, expectedVersion uint64) error {
	if in.Name == "" {
		return invalid("variable_name", "a variable has a name")
	}
	if in.SchemaRef == "" {
		return invalid("schema_ref", "a typed variable names its schema")
	}
	if in.WrittenAtRevision == 0 {
		return invalid("written_at_revision", "revision starts at 1")
	}
	if in.WrittenAt.IsZero() {
		return invalid("written_at", "timestamp is unset")
	}
	if err := requireJSONObject("variable_value", in.Value); err != nil {
		return err
	}

	if expectedVersion == 0 {
		affected, err := ex.Exec(ctx, `
			INSERT INTO workflow_variable (
				tenant_id, instance_id, variable_name, schema_ref, variable_value,
				written_at_revision, variable_version, written_at)
			VALUES ($1, $2, $3, $4, $5::jsonb, $6, 1, $7)
			ON CONFLICT DO NOTHING`,
			in.TenantID, in.InstanceID, in.Name, in.SchemaRef, string(in.Value),
			int64(in.WrittenAtRevision), in.WrittenAt.UTC())
		if err != nil {
			return fmt.Errorf("runtimestate: write variable %s: %w", in.Name, err)
		}
		if affected == 0 {
			return fmt.Errorf("%w: workflow_variable %s/%s", ErrDuplicate, in.InstanceID, in.Name)
		}
		return nil
	}

	affected, err := ex.Exec(ctx, `
		UPDATE workflow_variable
		SET schema_ref = $5, variable_value = $6::jsonb, written_at_revision = $7,
			variable_version = variable_version + 1, written_at = $8
		WHERE tenant_id = $1 AND instance_id = $2 AND variable_name = $3 AND variable_version = $4`,
		in.TenantID, in.InstanceID, in.Name, int64(expectedVersion),
		in.SchemaRef, string(in.Value), int64(in.WrittenAtRevision), in.WrittenAt.UTC())
	if err != nil {
		return fmt.Errorf("runtimestate: overwrite variable %s: %w", in.Name, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: workflow_variable %s/%s expected version %d",
			ErrVersionConflict, in.InstanceID, in.Name, expectedVersion)
	}
	return nil
}

// Get returns one variable.
func (s VariableStore) Get(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, name string) (Variable, error) {
	var (
		out      Variable
		revision int64
		version  int64
		value    string
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, instance_id, variable_name, schema_ref, variable_value::text,
			written_at_revision, variable_version, written_at
		FROM workflow_variable
		WHERE tenant_id = $1 AND instance_id = $2 AND variable_name = $3`,
		tenantID, instanceID, name).Scan(
		&out.TenantID, &out.InstanceID, &out.Name, &out.SchemaRef, &value,
		&revision, &version, &out.WrittenAt)
	if err != nil {
		if isNoRows(err) {
			return Variable{}, fmt.Errorf("%w: workflow_variable %s/%s", ErrNotFound, instanceID, name)
		}
		return Variable{}, fmt.Errorf("runtimestate: load variable %s: %w", name, err)
	}
	out.Value = json.RawMessage(value)
	out.WrittenAtRevision, out.Version = uint64(revision), uint64(version)
	out.WrittenAt = out.WrittenAt.UTC()
	return out, nil
}

// Timer kinds and states.
const (
	TimerDelay        = "DELAY"
	TimerDeadline     = "DEADLINE"
	TimerHeartbeat    = "HEARTBEAT"
	TimerRetryBackoff = "RETRY_BACKOFF"

	TimerPending   = "PENDING"
	TimerFired     = "FIRED"
	TimerCancelled = "CANCELLED"
)

var timerKinds = map[string]bool{
	TimerDelay: true, TimerDeadline: true, TimerHeartbeat: true, TimerRetryBackoff: true,
}

// Timer is one workflow_timer row.
type Timer struct {
	TenantID   uuid.UUID
	TimerID    uuid.UUID
	InstanceID uuid.UUID
	NodeID     string
	Key        string

	Kind    string
	State   string
	FiresAt time.Time
	Version uint64

	CreatedAt time.Time
	Causal    *CausalMetadata
}

// CausalMetadata is optional durable context for a timer/ready-work envelope.
// TraceLink is diagnostic only and never an authority or replay key.
type CausalMetadata struct {
	CorrelationID, CausationID, LogicalOperationID, AttemptID string
	TraceLink                                                 *TraceLinkMetadata
}
type TraceLinkMetadata struct {
	TraceID, SpanID string
	TraceFlags      byte
	TraceState      string
	ExpiresAt       time.Time
}

func causalValues(c *CausalMetadata) []any {
	c = normalizeCausal(c)
	if c == nil {
		return []any{nil, nil, nil, nil, nil, nil, nil, nil, nil}
	}
	if c.TraceLink == nil {
		return []any{c.CorrelationID, c.CausationID, c.LogicalOperationID, c.AttemptID, nil, nil, nil, nil, nil}
	}
	return []any{c.CorrelationID, c.CausationID, c.LogicalOperationID, c.AttemptID, c.TraceLink.TraceID, c.TraceLink.SpanID, c.TraceLink.TraceFlags, c.TraceLink.TraceState, nullableTime(c.TraceLink.ExpiresAt)}
}

func normalizeCausal(c *CausalMetadata) *CausalMetadata {
	if c == nil {
		return nil
	}
	for _, value := range []string{c.CorrelationID, c.CausationID, c.LogicalOperationID, c.AttemptID} {
		if strings.TrimSpace(value) == "" || len(value) > 128 {
			return nil
		}
	}
	out := *c
	out.TraceLink = nil
	if c.TraceLink != nil && validCausalTraceID(c.TraceLink.TraceID, 16) && validCausalTraceID(c.TraceLink.SpanID, 8) && len(c.TraceLink.TraceState) <= 256 {
		link := *c.TraceLink
		out.TraceLink = &link
	}
	return &out
}

func validCausalTraceID(value string, size int) bool {
	if value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != size {
		return false
	}
	for _, b := range decoded {
		if b != 0 {
			return true
		}
	}
	return false
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

func causalFromPointers(correlation, causation, logical, attempt, traceID, spanID *string, flags *int16, state *string, expires *time.Time) *CausalMetadata {
	if correlation == nil && causation == nil && logical == nil && attempt == nil {
		return nil
	}
	c := &CausalMetadata{}
	if correlation != nil {
		c.CorrelationID = *correlation
	}
	if causation != nil {
		c.CausationID = *causation
	}
	if logical != nil {
		c.LogicalOperationID = *logical
	}
	if attempt != nil {
		c.AttemptID = *attempt
	}
	if traceID != nil && spanID != nil && flags != nil {
		c.TraceLink = &TraceLinkMetadata{TraceID: *traceID, SpanID: *spanID, TraceFlags: byte(*flags), TraceState: valueOf(state), ExpiresAt: timeValue(expires)}
	}
	return c
}
func valueOf(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func timeValue(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return v.UTC()
}

// TimerStore writes and advances workflow_timer. Nothing here fires a timer on
// a clock: [TimerStore.Fire] is called by whoever observed that the instant
// passed, which keeps this a state store rather than the scheduler the
// WF-RUN-000 gate blocks.
type TimerStore struct{}

// Set records one pending timer. Setting the same (instance, node, key) twice is
// [ErrDuplicate], which is what makes a replayed advancement produce one wakeup
// rather than two.
func (s TimerStore) Set(ctx context.Context, ex Executor, in Timer) error {
	if in.NodeID == "" || in.Key == "" {
		return invalid("timer_key", "a timer is named by its node and key")
	}
	if !timerKinds[in.Kind] {
		return invalid("timer_kind", "kind is not a declared timer kind")
	}
	if in.FiresAt.IsZero() || in.CreatedAt.IsZero() {
		return invalid("fires_at", "a timer carries both a creation and a firing instant")
	}
	args := []any{in.TenantID, in.TimerID, in.InstanceID, in.NodeID, in.Key, in.Kind, TimerPending, in.FiresAt.UTC(), in.CreatedAt.UTC()}
	args = append(args, causalValues(in.Causal)...)
	affected, err := ex.Exec(ctx, `
		INSERT INTO workflow_timer (
			tenant_id, timer_id, instance_id, node_id, timer_key,
			timer_kind, timer_state, fires_at, timer_version, created_at,
			correlation_id, causation_id, logical_operation_id, attempt_id, trace_id, trace_span_id, trace_flags, trace_state, trace_link_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT DO NOTHING`,
		args...)
	if err != nil {
		return fmt.Errorf("runtimestate: set timer %s: %w", in.Key, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: workflow_timer %s/%s/%s", ErrDuplicate, in.InstanceID, in.NodeID, in.Key)
	}
	return nil
}

// Fire marks a pending timer fired under compare-and-swap. A timer that already
// fired or was cancelled is [ErrIllegalTransition]: firing twice would wake a
// node twice.
func (s TimerStore) Fire(ctx context.Context, ex Executor, tenantID, timerID uuid.UUID,
	expectedVersion uint64, at time.Time,
) error {
	return s.settle(ctx, ex, tenantID, timerID, expectedVersion, TimerFired, at)
}

// Cancel marks a pending timer cancelled under compare-and-swap.
func (s TimerStore) Cancel(ctx context.Context, ex Executor, tenantID, timerID uuid.UUID,
	expectedVersion uint64, at time.Time,
) error {
	return s.settle(ctx, ex, tenantID, timerID, expectedVersion, TimerCancelled, at)
}

func (s TimerStore) settle(ctx context.Context, ex Executor, tenantID, timerID uuid.UUID,
	expectedVersion uint64, next string, at time.Time,
) error {
	if expectedVersion == 0 {
		return invalid("expected_version", "a compare-and-swap needs the version it expects")
	}
	if at.IsZero() {
		return invalid("settled_at", "timestamp is unset")
	}
	current, err := s.Load(ctx, ex, tenantID, timerID)
	if err != nil {
		return err
	}
	if current.State != TimerPending {
		return fmt.Errorf("%w: workflow_timer %s is already %s", ErrIllegalTransition, timerID, current.State)
	}
	column := "fired_at"
	if next == TimerCancelled {
		column = "cancelled_at"
	}
	affected, err := ex.Exec(ctx, `
		UPDATE workflow_timer
		SET timer_state = $4, timer_version = timer_version + 1, `+column+` = $5
		WHERE tenant_id = $1 AND timer_id = $2 AND timer_version = $3`,
		tenantID, timerID, int64(expectedVersion), next, at.UTC())
	if err != nil {
		return fmt.Errorf("runtimestate: settle timer %s as %s: %w", timerID, next, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: workflow_timer %s expected version %d", ErrVersionConflict, timerID, expectedVersion)
	}
	return nil
}

// Load returns one timer.
func (s TimerStore) Load(ctx context.Context, ex Executor, tenantID, timerID uuid.UUID) (Timer, error) {
	var (
		out                                                                   Timer
		version                                                               int64
		correlation, causation, logical, attempt, traceID, spanID, traceState *string
		flags                                                                 *int16
		expires                                                               *time.Time
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, timer_id, instance_id, node_id, timer_key,
			timer_kind, timer_state, fires_at, timer_version, created_at,
			correlation_id, causation_id, logical_operation_id, attempt_id, trace_id, trace_span_id, trace_flags, trace_state, trace_link_expires_at
		FROM workflow_timer
		WHERE tenant_id = $1 AND timer_id = $2`, tenantID, timerID).Scan(
		&out.TenantID, &out.TimerID, &out.InstanceID, &out.NodeID, &out.Key,
		&out.Kind, &out.State, &out.FiresAt, &version, &out.CreatedAt,
		&correlation, &causation, &logical, &attempt, &traceID, &spanID, &flags, &traceState, &expires)
	if err != nil {
		if isNoRows(err) {
			return Timer{}, fmt.Errorf("%w: workflow_timer %s", ErrNotFound, timerID)
		}
		return Timer{}, fmt.Errorf("runtimestate: load timer %s: %w", timerID, err)
	}
	out.Version = uint64(version)
	out.FiresAt = out.FiresAt.UTC()
	out.CreatedAt = out.CreatedAt.UTC()
	out.Causal = causalFromPointers(correlation, causation, logical, attempt, traceID, spanID, flags, traceState, expires)
	return out, nil
}

// Signal subscription states.
const (
	SubscriptionOpen      = "OPEN"
	SubscriptionSatisfied = "SATISFIED"
	SubscriptionExpired   = "EXPIRED"
	SubscriptionCancelled = "CANCELLED"
)

// Subscription is one workflow_signal_subscription row.
type Subscription struct {
	TenantID       uuid.UUID
	SubscriptionID uuid.UUID
	InstanceID     uuid.UUID
	NodeID         string
	SignalName     string
	CorrelationKey string

	State   string
	Version uint64

	CreatedAt time.Time
}

// Signal is one workflow_signal row: one delivery of a named signal.
type Signal struct {
	TenantID       uuid.UUID
	SignalID       uuid.UUID
	SignalName     string
	CorrelationKey string
	DedupeToken    string

	SchemaRef     string
	Payload       json.RawMessage
	PayloadDigest string

	DeliveredAt time.Time
}

// SignalStore writes subscriptions, deliveries and application receipts.
type SignalStore struct{}

// Subscribe opens one subscription. Subscribing the same (instance, node,
// signal) twice is [ErrDuplicate]: one wait is one subscription.
func (s SignalStore) Subscribe(ctx context.Context, ex Executor, in Subscription) error {
	if in.NodeID == "" || in.SignalName == "" || in.CorrelationKey == "" {
		return invalid("signal_name", "a subscription names its node, signal and correlation key")
	}
	if in.CreatedAt.IsZero() {
		return invalid("created_at", "timestamp is unset")
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO workflow_signal_subscription (
			tenant_id, subscription_id, instance_id, node_id, signal_name,
			correlation_key, event_type, correlation_value,
			subscription_state, subscription_version, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $5, $6, $7, 1, $8)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.SubscriptionID, in.InstanceID, in.NodeID, in.SignalName,
		in.CorrelationKey, SubscriptionOpen, in.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("runtimestate: subscribe %s to %s: %w", in.InstanceID, in.SignalName, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: workflow_signal_subscription %s/%s/%s",
			ErrDuplicate, in.InstanceID, in.NodeID, in.SignalName)
	}
	return nil
}

// Deliver records one signal arrival. A redelivery carrying the same dedupe
// token is [ErrDuplicate], which is what makes an at-least-once transport safe
// to point at this table.
func (s SignalStore) Deliver(ctx context.Context, ex Executor, in Signal) error {
	if in.SignalName == "" || in.CorrelationKey == "" || in.DedupeToken == "" {
		return invalid("dedupe_token", "a delivery names its signal, correlation key and dedupe token")
	}
	if in.SchemaRef == "" {
		return invalid("schema_ref", "a typed signal names its schema")
	}
	if len(in.PayloadDigest) != 64 {
		return invalid("payload_digest", "digest is not 64 hex characters")
	}
	if in.DeliveredAt.IsZero() {
		return invalid("delivered_at", "timestamp is unset")
	}
	if err := requireJSONObject("payload", in.Payload); err != nil {
		return err
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO workflow_signal (
			tenant_id, signal_id, signal_name, correlation_key, event_type,
			source, correlation_value, sequence_number, dedupe_token,
			schema_ref, payload, payload_digest, delivered_at, received_at)
		VALUES ($1, $2, $3, $4, $3, 'legacy/unknown', $4, 0, $5,
			$6, $7::jsonb, $8, $9, $9)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.SignalID, in.SignalName, in.CorrelationKey, in.DedupeToken,
		in.SchemaRef, string(in.Payload), in.PayloadDigest, in.DeliveredAt.UTC())
	if err != nil {
		return fmt.Errorf("runtimestate: deliver signal %s: %w", in.SignalName, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: workflow_signal %s/%s/%s",
			ErrDuplicate, in.SignalName, in.CorrelationKey, in.DedupeToken)
	}
	return nil
}

// Apply records that one signal was applied to one subscription and closes the
// subscription, in one statement pair inside the caller's transaction.
//
// The receipt's primary key is (signal, subscription), so applying the same
// delivery twice is [ErrDuplicate] and the second application changes nothing.
// That is exactly-once at the seam a redelivery actually arrives at, rather than
// a promise about the transport.
func (s SignalStore) Apply(ctx context.Context, ex Executor, tenantID, signalID, subscriptionID, instanceID uuid.UUID,
	appliedAtVersion uint64, at time.Time,
) error {
	if appliedAtVersion == 0 {
		return invalid("applied_at_version", "a receipt records the instance version it advanced to")
	}
	if at.IsZero() {
		return invalid("applied_at", "timestamp is unset")
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO workflow_signal_receipt (
			tenant_id, signal_id, subscription_id, instance_id, applied_at, applied_at_version)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT DO NOTHING`,
		tenantID, signalID, subscriptionID, instanceID, at.UTC(), int64(appliedAtVersion))
	if err != nil {
		return fmt.Errorf("runtimestate: apply signal %s: %w", signalID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: workflow_signal_receipt %s/%s", ErrDuplicate, signalID, subscriptionID)
	}
	if _, err := ex.Exec(ctx, `
		UPDATE workflow_signal_subscription
		SET subscription_state = $3, subscription_version = subscription_version + 1, closed_at = $4
		WHERE tenant_id = $1 AND subscription_id = $2 AND subscription_state = $5`,
		tenantID, subscriptionID, SubscriptionSatisfied, at.UTC(), SubscriptionOpen); err != nil {
		return fmt.Errorf("runtimestate: close subscription %s: %w", subscriptionID, err)
	}
	return nil
}

// Lease resource kinds and states.
const (
	LeaseWorkflowInstance = "WORKFLOW_INSTANCE"
	LeaseNodeExecution    = "NODE_EXECUTION"
	LeaseWorkItem         = "WORK_ITEM"
	LeaseQueue            = "QUEUE"

	LeaseHeld     = "HELD"
	LeaseReleased = "RELEASED"
	LeaseExpired  = "EXPIRED"
	LeaseRevoked  = "REVOKED"
)

var leaseKinds = map[string]bool{
	LeaseWorkflowInstance: true, LeaseNodeExecution: true, LeaseWorkItem: true, LeaseQueue: true,
}

// Lease is one workflow_lease row.
type Lease struct {
	TenantID uuid.UUID
	LeaseID  uuid.UUID

	ResourceKind string
	ResourceID   string

	State      string
	FenceToken uint64
	HolderID   string

	AcquiredAt  time.Time
	ExpiresAt   time.Time
	HeartbeatAt time.Time
	Version     uint64
}

// LeaseStore acquires, heartbeats and releases workflow_lease.
//
// It is the durable half of WF-RUN-002 and nothing more: there is no background
// reaper here, because expiring other people's leases on a timer is scheduler
// code the WF-RUN-000 gate blocks. A caller that observes an expired lease calls
// [LeaseStore.Expire] itself, in its own transaction.
type LeaseStore struct{}

// Acquire takes the lease on a resource, minting the next fence token.
//
// A live lease held by someone else is [ErrLeaseHeld]. A lease that has expired
// by the caller's own clock is takeable: the new holder's token is the previous
// token plus one, so the old holder's later write is refusable by comparison
// rather than by hoping it noticed. The read and the two writes run in the
// caller's transaction, and the partial unique index on the HELD rows is what
// makes a concurrent acquirer collide rather than co-hold.
func (s LeaseStore) Acquire(ctx context.Context, ex Executor, in Lease, now time.Time) (Lease, error) {
	if !leaseKinds[in.ResourceKind] {
		return Lease{}, invalid("resource_kind", "kind is not a declared lease resource kind")
	}
	if in.ResourceID == "" || in.HolderID == "" {
		return Lease{}, invalid("holder_id", "a lease names its resource and its holder")
	}
	if in.AcquiredAt.IsZero() || !in.ExpiresAt.After(in.AcquiredAt) {
		return Lease{}, invalid("expires_at", "a lease expires strictly after it is acquired")
	}
	if now.IsZero() {
		return Lease{}, invalid("now", "acquiring a lease needs the caller's own clock reading")
	}

	current, err := s.Current(ctx, ex, in.TenantID, in.ResourceKind, in.ResourceID)
	switch {
	case err == nil && current.ExpiresAt.After(now):
		return Lease{}, fmt.Errorf("%w: %s %s is held by %s until %s",
			ErrLeaseHeld, in.ResourceKind, in.ResourceID, current.HolderID, current.ExpiresAt)
	case err == nil:
		// The lease lapsed. Retire it explicitly so the partial unique index has
		// room for the new holder, and so the history says it expired rather
		// than vanished.
		if _, err := ex.Exec(ctx, `
			UPDATE workflow_lease
			SET lease_state = $3, lease_version = lease_version + 1, released_at = $4
			WHERE tenant_id = $1 AND lease_id = $2`,
			in.TenantID, current.LeaseID, LeaseExpired, now.UTC()); err != nil {
			return Lease{}, fmt.Errorf("runtimestate: expire lapsed lease %s: %w", current.LeaseID, err)
		}
	case !errors.Is(err, ErrNotFound):
		return Lease{}, err
	}

	next, err := s.nextFence(ctx, ex, in.TenantID, in.ResourceKind, in.ResourceID)
	if err != nil {
		return Lease{}, err
	}
	in.FenceToken = next
	in.State = LeaseHeld
	if in.HeartbeatAt.IsZero() {
		in.HeartbeatAt = in.AcquiredAt
	}
	if _, err := ex.Exec(ctx, `
		INSERT INTO workflow_lease (
			tenant_id, lease_id, resource_kind, resource_id,
			lease_state, fence_token, holder_id,
			acquired_at, expires_at, heartbeat_at, lease_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 1)`,
		in.TenantID, in.LeaseID, in.ResourceKind, in.ResourceID,
		in.State, int64(in.FenceToken), in.HolderID,
		in.AcquiredAt.UTC(), in.ExpiresAt.UTC(), in.HeartbeatAt.UTC()); err != nil {
		return Lease{}, fmt.Errorf("runtimestate: acquire %s %s: %w", in.ResourceKind, in.ResourceID, err)
	}
	in.Version = 1
	return in, nil
}

// Heartbeat extends a held lease, refusing a holder whose fence token is no
// longer the resource's current one ([ErrFenceStale]).
func (s LeaseStore) Heartbeat(ctx context.Context, ex Executor, tenantID, leaseID uuid.UUID,
	fenceToken uint64, at, newExpiry time.Time,
) error {
	if fenceToken == 0 {
		return invalid("fence_token", "a heartbeat presents the token it holds")
	}
	if at.IsZero() || !newExpiry.After(at) {
		return invalid("expires_at", "a heartbeat extends the lease past its own instant")
	}
	affected, err := ex.Exec(ctx, `
		UPDATE workflow_lease
		SET heartbeat_at = $4, expires_at = $5, lease_version = lease_version + 1
		WHERE tenant_id = $1 AND lease_id = $2 AND fence_token = $3
		  AND lease_state = $6`,
		tenantID, leaseID, int64(fenceToken), at.UTC(), newExpiry.UTC(), LeaseHeld)
	if err != nil {
		return fmt.Errorf("runtimestate: heartbeat lease %s: %w", leaseID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: lease %s no longer holds fence token %d", ErrFenceStale, leaseID, fenceToken)
	}
	return nil
}

// Release gives up a held lease.
func (s LeaseStore) Release(ctx context.Context, ex Executor, tenantID, leaseID uuid.UUID,
	fenceToken uint64, at time.Time,
) error {
	return s.settle(ctx, ex, tenantID, leaseID, fenceToken, LeaseReleased, at)
}

// Expire retires a lapsed lease. The caller supplies the observation that it
// lapsed; nothing here watches a clock.
func (s LeaseStore) Expire(ctx context.Context, ex Executor, tenantID, leaseID uuid.UUID,
	fenceToken uint64, at time.Time,
) error {
	return s.settle(ctx, ex, tenantID, leaseID, fenceToken, LeaseExpired, at)
}

func (s LeaseStore) settle(ctx context.Context, ex Executor, tenantID, leaseID uuid.UUID,
	fenceToken uint64, next string, at time.Time,
) error {
	if at.IsZero() {
		return invalid("released_at", "timestamp is unset")
	}
	affected, err := ex.Exec(ctx, `
		UPDATE workflow_lease
		SET lease_state = $4, lease_version = lease_version + 1, released_at = $5
		WHERE tenant_id = $1 AND lease_id = $2 AND fence_token = $3 AND lease_state = $6`,
		tenantID, leaseID, int64(fenceToken), next, at.UTC(), LeaseHeld)
	if err != nil {
		return fmt.Errorf("runtimestate: settle lease %s as %s: %w", leaseID, next, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: lease %s no longer holds fence token %d", ErrFenceStale, leaseID, fenceToken)
	}
	return nil
}

// Current returns the live lease on a resource, if one is held.
func (s LeaseStore) Current(ctx context.Context, ex Executor, tenantID uuid.UUID, kind, resourceID string) (Lease, error) {
	var (
		out     Lease
		fence   int64
		version int64
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, lease_id, resource_kind, resource_id,
			lease_state, fence_token, holder_id,
			acquired_at, expires_at, heartbeat_at, lease_version
		FROM workflow_lease
		WHERE tenant_id = $1 AND resource_kind = $2 AND resource_id = $3 AND lease_state = $4`,
		tenantID, kind, resourceID, LeaseHeld).Scan(
		&out.TenantID, &out.LeaseID, &out.ResourceKind, &out.ResourceID,
		&out.State, &fence, &out.HolderID,
		&out.AcquiredAt, &out.ExpiresAt, &out.HeartbeatAt, &version)
	if err != nil {
		if isNoRows(err) {
			return Lease{}, fmt.Errorf("%w: no held lease on %s %s", ErrNotFound, kind, resourceID)
		}
		return Lease{}, fmt.Errorf("runtimestate: read lease on %s %s: %w", kind, resourceID, err)
	}
	out.FenceToken, out.Version = uint64(fence), uint64(version)
	out.AcquiredAt = out.AcquiredAt.UTC()
	out.ExpiresAt = out.ExpiresAt.UTC()
	out.HeartbeatAt = out.HeartbeatAt.UTC()
	return out, nil
}

// nextFence returns one past the highest token ever issued for a resource, so
// tokens are monotonic across the whole lease history rather than only across
// the live ones.
func (s LeaseStore) nextFence(ctx context.Context, ex Executor, tenantID uuid.UUID, kind, resourceID string) (uint64, error) {
	var highest int64
	if err := ex.QueryRow(ctx, `
		SELECT coalesce(max(fence_token), 0) FROM workflow_lease
		WHERE tenant_id = $1 AND resource_kind = $2 AND resource_id = $3`,
		tenantID, kind, resourceID).Scan(&highest); err != nil {
		return 0, fmt.Errorf("runtimestate: read fence history of %s %s: %w", kind, resourceID, err)
	}
	return uint64(highest) + 1, nil
}

// Checkpoint kinds.
const (
	CheckpointSafePoint  = "SAFE_POINT"
	CheckpointPause      = "PAUSE"
	CheckpointVersionPin = "VERSION_PIN"
	CheckpointMigration  = "MIGRATION"
)

var checkpointKinds = map[string]bool{
	CheckpointSafePoint: true, CheckpointPause: true,
	CheckpointVersionPin: true, CheckpointMigration: true,
}

// Checkpoint is one workflow_checkpoint row.
type Checkpoint struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	Sequence   uint64

	Kind            string
	StateDigest     string
	FrontierDigest  string
	VariableDigest  string
	InstanceVersion uint64

	TakenAt time.Time
}

// CheckpointStore writes and reads workflow_checkpoint.
type CheckpointStore struct{}

// Take records one immutable checkpoint. A repeated sequence is [ErrDuplicate]:
// a safe point that could be rewritten is not a safe point.
func (s CheckpointStore) Take(ctx context.Context, ex Executor, in Checkpoint) error {
	if !checkpointKinds[in.Kind] {
		return invalid("checkpoint_kind", "kind is not a declared checkpoint kind")
	}
	if in.Sequence == 0 {
		return invalid("checkpoint_sequence", "sequence starts at 1")
	}
	if in.InstanceVersion == 0 {
		return invalid("instance_version", "a checkpoint records the instance version it describes")
	}
	for _, req := range []struct{ field, value string }{
		{"state_digest", in.StateDigest},
		{"frontier_digest", in.FrontierDigest},
		{"variable_digest", in.VariableDigest},
	} {
		if len(req.value) != 64 {
			return invalid(req.field, "digest is not 64 hex characters")
		}
	}
	if in.TakenAt.IsZero() {
		return invalid("taken_at", "timestamp is unset")
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO workflow_checkpoint (
			tenant_id, instance_id, checkpoint_sequence, checkpoint_kind,
			state_digest, frontier_digest, variable_digest, instance_version, taken_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.InstanceID, int64(in.Sequence), in.Kind,
		in.StateDigest, in.FrontierDigest, in.VariableDigest,
		int64(in.InstanceVersion), in.TakenAt.UTC())
	if err != nil {
		return fmt.Errorf("runtimestate: take checkpoint %d of %s: %w", in.Sequence, in.InstanceID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: workflow_checkpoint %s/%d", ErrDuplicate, in.InstanceID, in.Sequence)
	}
	return nil
}

// Latest returns the highest-numbered checkpoint of an instance.
func (s CheckpointStore) Latest(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) (Checkpoint, error) {
	var (
		out      Checkpoint
		sequence int64
		version  int64
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, instance_id, checkpoint_sequence, checkpoint_kind,
			state_digest, frontier_digest, variable_digest, instance_version, taken_at
		FROM workflow_checkpoint
		WHERE tenant_id = $1 AND instance_id = $2
		ORDER BY checkpoint_sequence DESC
		LIMIT 1`, tenantID, instanceID).Scan(
		&out.TenantID, &out.InstanceID, &sequence, &out.Kind,
		&out.StateDigest, &out.FrontierDigest, &out.VariableDigest, &version, &out.TakenAt)
	if err != nil {
		if isNoRows(err) {
			return Checkpoint{}, fmt.Errorf("%w: no checkpoint for instance %s", ErrNotFound, instanceID)
		}
		return Checkpoint{}, fmt.Errorf("runtimestate: read checkpoints of %s: %w", instanceID, err)
	}
	out.Sequence, out.InstanceVersion = uint64(sequence), uint64(version)
	out.TakenAt = out.TakenAt.UTC()
	return out, nil
}

// Child link completion modes.
const (
	ChildAwait               = "AWAIT"
	ChildDetach              = "DETACH"
	ChildCompensateOnFailure = "COMPENSATE_ON_FAILURE"
)

var childModes = map[string]bool{
	ChildAwait: true, ChildDetach: true, ChildCompensateOnFailure: true,
}

// ChildLink is one workflow_child_link row.
type ChildLink struct {
	TenantID uuid.UUID
	Parent   uuid.UUID
	Child    uuid.UUID

	ParentNodeID string
	Ordinal      int
	Mode         string
	InputDigest  string

	CreatedAt time.Time
}

// ChildLinkStore writes and reads workflow_child_link.
type ChildLinkStore struct{}

// Link records one parent/child edge. A child that already has a parent is
// [ErrDuplicate] and a self-link is [ErrInvalid]: both would make cancellation
// propagation ambiguous or non-terminating.
func (s ChildLinkStore) Link(ctx context.Context, ex Executor, in ChildLink) error {
	if in.Parent == in.Child {
		return invalid("child_instance_id", "an instance cannot be its own child")
	}
	if in.ParentNodeID == "" {
		return invalid("parent_node_id", "a child link names the node that spawned it")
	}
	if in.Ordinal < 1 {
		return invalid("ordinal", "ordinal starts at 1")
	}
	if !childModes[in.Mode] {
		return invalid("completion_mode", "mode is not a declared completion mode")
	}
	if len(in.InputDigest) != 64 {
		return invalid("child_input_digest", "digest is not 64 hex characters")
	}
	if in.CreatedAt.IsZero() {
		return invalid("created_at", "timestamp is unset")
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO workflow_child_link (
			tenant_id, parent_instance_id, child_instance_id,
			parent_node_id, ordinal, completion_mode, child_input_digest, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.Parent, in.Child,
		in.ParentNodeID, in.Ordinal, in.Mode, in.InputDigest, in.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("runtimestate: link child %s under %s: %w", in.Child, in.Parent, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: workflow_child_link child %s", ErrDuplicate, in.Child)
	}
	return nil
}

// Children returns a parent's child instances in ordinal order.
func (s ChildLinkStore) Children(ctx context.Context, ex Executor, tenantID, parent uuid.UUID) ([]uuid.UUID, error) {
	rows, err := ex.Query(ctx, `
		SELECT child_instance_id FROM workflow_child_link
		WHERE tenant_id = $1 AND parent_instance_id = $2
		ORDER BY parent_node_id, ordinal`, tenantID, parent)
	if err != nil {
		return nil, fmt.Errorf("runtimestate: read children of %s: %w", parent, err)
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var child uuid.UUID
		if err := rows.Scan(&child); err != nil {
			return nil, fmt.Errorf("runtimestate: scan child link: %w", err)
		}
		out = append(out, child)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runtimestate: read children of %s: %w", parent, err)
	}
	return out, nil
}

// allows reports whether graph permits from -> to.
func allows(graph map[string][]string, from, to string) bool {
	return slices.Contains(graph[from], to)
}

// --- Additive read helpers -------------------------------------------------
//
// The four methods below were added for WF-RUN-002 (internal/workflow/lease)
// and WF-RUN-004 (internal/workflow/timer). Every one of them is a SELECT:
// they add no table, no state transition and no clock, so the WF-RUN-000 gate
// still holds -- a caller still supplies every instant and still drives every
// settle itself.

// CurrentForUpdate is [LeaseStore.Current] taking a row lock on the live
// lease.
//
// The difference matters for fencing rather than for reading. A holder that
// verifies its fence with an unlocked read can be superseded between that
// read and the write it was fencing: the takeover's own UPDATE of the lapsed
// row commits in the gap and the stale write lands anyway. Taking FOR UPDATE
// on the HELD row makes a concurrent [LeaseStore.Acquire] block until the
// verifying transaction finishes, which is what turns "the token matched a
// moment ago" into "the token holds for this transaction".
func (s LeaseStore) CurrentForUpdate(ctx context.Context, ex Executor, tenantID uuid.UUID, kind, resourceID string) (Lease, error) {
	return s.currentQuery(ctx, ex, tenantID, kind, resourceID, " FOR UPDATE")
}

func (s LeaseStore) currentQuery(ctx context.Context, ex Executor, tenantID uuid.UUID, kind, resourceID, suffix string) (Lease, error) {
	var (
		out     Lease
		fence   int64
		version int64
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, lease_id, resource_kind, resource_id,
			lease_state, fence_token, holder_id,
			acquired_at, expires_at, heartbeat_at, lease_version
		FROM workflow_lease
		WHERE tenant_id = $1 AND resource_kind = $2 AND resource_id = $3 AND lease_state = $4`+suffix,
		tenantID, kind, resourceID, LeaseHeld).Scan(
		&out.TenantID, &out.LeaseID, &out.ResourceKind, &out.ResourceID,
		&out.State, &fence, &out.HolderID,
		&out.AcquiredAt, &out.ExpiresAt, &out.HeartbeatAt, &version)
	if err != nil {
		if isNoRows(err) {
			return Lease{}, fmt.Errorf("%w: no held lease on %s %s", ErrNotFound, kind, resourceID)
		}
		return Lease{}, fmt.Errorf("runtimestate: read lease on %s %s: %w", kind, resourceID, err)
	}
	out.FenceToken, out.Version = uint64(fence), uint64(version)
	out.AcquiredAt = out.AcquiredAt.UTC()
	out.ExpiresAt = out.ExpiresAt.UTC()
	out.HeartbeatAt = out.HeartbeatAt.UTC()
	return out, nil
}

// History returns every lease row ever written for one resource, oldest fence
// token first. Released and expired rows stay in the table on purpose (the
// one-holder uniqueness is a partial index over the HELD rows only), so this
// is the durable evidence that a fence line is monotonic and that each
// transition happened.
func (s LeaseStore) History(ctx context.Context, ex Executor, tenantID uuid.UUID, kind, resourceID string) ([]Lease, error) {
	rows, err := ex.Query(ctx, `
		SELECT tenant_id, lease_id, resource_kind, resource_id,
			lease_state, fence_token, holder_id,
			acquired_at, expires_at, heartbeat_at, released_at, lease_version
		FROM workflow_lease
		WHERE tenant_id = $1 AND resource_kind = $2 AND resource_id = $3
		ORDER BY fence_token`, tenantID, kind, resourceID)
	if err != nil {
		return nil, fmt.Errorf("runtimestate: read lease history of %s %s: %w", kind, resourceID, err)
	}
	defer rows.Close()

	var out []Lease
	for rows.Next() {
		var (
			l        Lease
			fence    int64
			version  int64
			released *time.Time
		)
		if err := rows.Scan(&l.TenantID, &l.LeaseID, &l.ResourceKind, &l.ResourceID,
			&l.State, &fence, &l.HolderID,
			&l.AcquiredAt, &l.ExpiresAt, &l.HeartbeatAt, &released, &version); err != nil {
			return nil, fmt.Errorf("runtimestate: scan lease row: %w", err)
		}
		l.FenceToken, l.Version = uint64(fence), uint64(version)
		l.AcquiredAt, l.ExpiresAt, l.HeartbeatAt = l.AcquiredAt.UTC(), l.ExpiresAt.UTC(), l.HeartbeatAt.UTC()
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runtimestate: read lease history of %s %s: %w", kind, resourceID, err)
	}
	return out, nil
}

// Due returns the pending timers whose firing instant is at or before asOf,
// soonest first, bounded by limit (a limit below one reads one page of 100).
//
// This is a read, not a scheduler: it answers "which promises are due by the
// clock reading you handed me", and the caller still decides whether and how
// to settle each one. Nothing here polls.
func (s TimerStore) Due(ctx context.Context, ex Executor, tenantID uuid.UUID, asOf time.Time, limit int) ([]Timer, error) {
	if asOf.IsZero() {
		return nil, invalid("as_of", "listing due timers needs the caller's own clock reading")
	}
	if limit < 1 {
		limit = 100
	}
	return s.list(ctx, ex, `
		WHERE tenant_id = $1 AND timer_state = $2 AND fires_at <= $3
		ORDER BY fires_at, timer_id
		LIMIT $4`, tenantID, TimerPending, asOf.UTC(), limit)
}

// PendingForInstance returns every still-pending timer of one instance,
// soonest first. It is what lets a caller cancel an instance's outstanding
// promises when the instance itself completes.
func (s TimerStore) PendingForInstance(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]Timer, error) {
	return s.list(ctx, ex, `
		WHERE tenant_id = $1 AND instance_id = $2 AND timer_state = $3
		ORDER BY fires_at, timer_id`, tenantID, instanceID, TimerPending)
}

// ForInstance returns every timer one instance ever scheduled -- pending,
// fired and cancelled -- oldest first. It is the execution inspector's read
// (WF-RUN-019): a retry backoff that already fired is as much a part of the
// instance's history as one still pending.
func (s TimerStore) ForInstance(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]Timer, error) {
	return s.list(ctx, ex, `
		WHERE tenant_id = $1 AND instance_id = $2
		ORDER BY created_at, fires_at, timer_id`, tenantID, instanceID)
}

func (s TimerStore) list(ctx context.Context, ex Executor, where string, args ...any) ([]Timer, error) {
	rows, err := ex.Query(ctx, `
		SELECT tenant_id, timer_id, instance_id, node_id, timer_key,
			timer_kind, timer_state, fires_at, timer_version, created_at,
			correlation_id, causation_id, logical_operation_id, attempt_id, trace_id, trace_span_id, trace_flags, trace_state, trace_link_expires_at
		FROM workflow_timer `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("runtimestate: list timers: %w", err)
	}
	defer rows.Close()

	var out []Timer
	for rows.Next() {
		var (
			t                                                                     Timer
			version                                                               int64
			correlation, causation, logical, attempt, traceID, spanID, traceState *string
			flags                                                                 *int16
			expires                                                               *time.Time
		)
		if err := rows.Scan(&t.TenantID, &t.TimerID, &t.InstanceID, &t.NodeID, &t.Key,
			&t.Kind, &t.State, &t.FiresAt, &version, &t.CreatedAt,
			&correlation, &causation, &logical, &attempt, &traceID, &spanID, &flags, &traceState, &expires); err != nil {
			return nil, fmt.Errorf("runtimestate: scan timer row: %w", err)
		}
		t.Version = uint64(version)
		t.FiresAt, t.CreatedAt = t.FiresAt.UTC(), t.CreatedAt.UTC()
		t.Causal = causalFromPointers(correlation, causation, logical, attempt, traceID, spanID, flags, traceState, expires)
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runtimestate: list timers: %w", err)
	}
	return out, nil
}

// --- WF-RUN-026 additions ---------------------------------------------------
//
// internal/workflow/migrate/artifacts migrates an instance's pending runtime
// artifacts onto a new workflow epoch inside the WF-RUN-018 migration
// transaction. To do that it has to be able to enumerate what an instance
// still holds and to retire a subscription it re-keyed. Three of the four
// additions below are SELECTs; the fourth is a compare-and-swap on one
// subscription's own version, in exactly the shape [SignalStore.Apply]
// already writes that column. None of them adds a table, a clock or a
// background sweep, so the WF-RUN-000 gate still holds.

// OpenForInstance returns every still-open subscription of one instance,
// ordered by node and signal name so two readers see the same sequence.
func (s SignalStore) OpenForInstance(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]Subscription, error) {
	rows, err := ex.Query(ctx, `
		SELECT tenant_id, subscription_id, instance_id, node_id, signal_name,
			correlation_key, subscription_state, subscription_version, created_at
		FROM workflow_signal_subscription
		WHERE tenant_id = $1 AND instance_id = $2 AND subscription_state = $3
		ORDER BY node_id, signal_name`, tenantID, instanceID, SubscriptionOpen)
	if err != nil {
		return nil, fmt.Errorf("runtimestate: list open subscriptions of %s: %w", instanceID, err)
	}
	defer rows.Close()

	var out []Subscription
	for rows.Next() {
		var (
			sub     Subscription
			version int64
		)
		if err := rows.Scan(&sub.TenantID, &sub.SubscriptionID, &sub.InstanceID, &sub.NodeID,
			&sub.SignalName, &sub.CorrelationKey, &sub.State, &version, &sub.CreatedAt); err != nil {
			return nil, fmt.Errorf("runtimestate: scan subscription row: %w", err)
		}
		sub.Version = uint64(version)
		sub.CreatedAt = sub.CreatedAt.UTC()
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runtimestate: list open subscriptions of %s: %w", instanceID, err)
	}
	return out, nil
}

// CloseSubscription settles one open subscription as EXPIRED or CANCELLED
// under a compare-and-swap on its own version.
//
// SATISFIED is deliberately not settleable here: a subscription is satisfied
// by a signal actually arriving, which is [SignalStore.Apply]'s job and
// records a receipt. This method exists for the two ways a subscription ends
// without one -- it lapsed, or the wait it belonged to was withdrawn.
func (s SignalStore) CloseSubscription(ctx context.Context, ex Executor, tenantID, subscriptionID uuid.UUID,
	expectedVersion uint64, next string, at time.Time,
) error {
	if next != SubscriptionExpired && next != SubscriptionCancelled {
		return invalid("subscription_state", "a subscription closes as EXPIRED or CANCELLED here; SATISFIED needs a signal")
	}
	if expectedVersion == 0 {
		return invalid("expected_version", "a compare-and-swap needs the version it expects")
	}
	if at.IsZero() {
		return invalid("closed_at", "closing a subscription records when it ended")
	}
	affected, err := ex.Exec(ctx, `
		UPDATE workflow_signal_subscription
		SET subscription_state = $4, subscription_version = subscription_version + 1, closed_at = $5
		WHERE tenant_id = $1 AND subscription_id = $2 AND subscription_version = $3
			AND subscription_state = $6`,
		tenantID, subscriptionID, int64(expectedVersion), next, at.UTC(), SubscriptionOpen)
	if err != nil {
		return fmt.Errorf("runtimestate: close subscription %s: %w", subscriptionID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: workflow_signal_subscription %s expected version %d in %s",
			ErrVersionConflict, subscriptionID, expectedVersion, SubscriptionOpen)
	}
	return nil
}

// PendingForInstance returns every ready-work row of one instance that has
// not been settled, soonest-eligible first. DISPATCHED rows are included:
// work handed to a worker is still work the instance owes.
func (s ReadyWorkStore) PendingForInstance(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]ReadyWork, error) {
	rows, err := ex.Query(ctx, `
		SELECT tenant_id, ready_work_id, instance_id, node_id, attempt,
			ready_state, priority, eligible_at, ready_version, enqueued_at, completed_at,
			correlation_id, causation_id, logical_operation_id, attempt_id, trace_id, trace_span_id, trace_flags, trace_state, trace_link_expires_at
		FROM workflow_ready_work
		WHERE tenant_id = $1 AND instance_id = $2 AND ready_state IN ($3, $4)
		ORDER BY eligible_at, node_id, attempt`,
		tenantID, instanceID, ReadyReady, ReadyDispatched)
	if err != nil {
		return nil, fmt.Errorf("runtimestate: list pending ready work of %s: %w", instanceID, err)
	}
	defer rows.Close()

	var out []ReadyWork
	for rows.Next() {
		var (
			rw                                                                    ReadyWork
			version                                                               int64
			completed                                                             *time.Time
			correlation, causation, logical, attempt, traceID, spanID, traceState *string
			flags                                                                 *int16
			expires                                                               *time.Time
		)
		if err := rows.Scan(&rw.TenantID, &rw.ReadyWorkID, &rw.InstanceID, &rw.NodeID, &rw.Attempt,
			&rw.State, &rw.Priority, &rw.EligibleAt, &version, &rw.EnqueuedAt, &completed,
			&correlation, &causation, &logical, &attempt, &traceID, &spanID, &flags, &traceState, &expires); err != nil {
			return nil, fmt.Errorf("runtimestate: scan ready work row: %w", err)
		}
		rw.Version = uint64(version)
		rw.EligibleAt, rw.EnqueuedAt = rw.EligibleAt.UTC(), rw.EnqueuedAt.UTC()
		if completed != nil {
			rw.CompletedAt = completed.UTC()
		}
		rw.Causal = causalFromPointers(correlation, causation, logical, attempt, traceID, spanID, flags, traceState, expires)
		out = append(out, rw)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runtimestate: list pending ready work of %s: %w", instanceID, err)
	}
	return out, nil
}

// LinksForParent returns a parent's child links in full, in the same
// (node, ordinal) order [ChildLinkStore.Children] returns bare ids in. A
// caller that has to reason about how a child's completion reaches its parent
// -- the completion mode, the node that spawned it -- needs the row, not just
// the child's identity.
func (s ChildLinkStore) LinksForParent(ctx context.Context, ex Executor, tenantID, parent uuid.UUID) ([]ChildLink, error) {
	rows, err := ex.Query(ctx, `
		SELECT tenant_id, parent_instance_id, child_instance_id,
			parent_node_id, ordinal, completion_mode, child_input_digest, created_at
		FROM workflow_child_link
		WHERE tenant_id = $1 AND parent_instance_id = $2
		ORDER BY parent_node_id, ordinal`, tenantID, parent)
	if err != nil {
		return nil, fmt.Errorf("runtimestate: read child links of %s: %w", parent, err)
	}
	defer rows.Close()

	var out []ChildLink
	for rows.Next() {
		var link ChildLink
		if err := rows.Scan(&link.TenantID, &link.Parent, &link.Child,
			&link.ParentNodeID, &link.Ordinal, &link.Mode, &link.InputDigest, &link.CreatedAt); err != nil {
			return nil, fmt.Errorf("runtimestate: scan child link row: %w", err)
		}
		link.CreatedAt = link.CreatedAt.UTC()
		out = append(out, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runtimestate: read child links of %s: %w", parent, err)
	}
	return out, nil
}
