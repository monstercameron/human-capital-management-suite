package jobs

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// The sentinels this package's stores classify failures with, mirroring
// internal/data/runtimestate's own vocabulary so a caller composing both
// stores in one transaction (a future JOB-002/JOB-003 dispatcher, or a test)
// distinguishes a race from a contradiction from an absence the same way in
// either package.
var (
	// ErrInvalid reports a row that is not internally consistent. It is
	// returned before any statement runs.
	ErrInvalid = errors.New("jobs: invalid row")

	// ErrDuplicate reports a row whose identity already exists: a second
	// publish of the same job version, a second run under an already-used run
	// id, a second partition for a run's already-used partition key, or a
	// repeated checkpoint sequence.
	ErrDuplicate = errors.New("jobs: duplicate row")

	// ErrNotFound reports a row that does not exist.
	ErrNotFound = errors.New("jobs: not found")

	// ErrVersionConflict reports a compare-and-swap whose expected version is
	// not the stored one. Nothing was written. This is also how ClaimPartition
	// reports a losing claimant: the loser's expected version is no longer
	// current the instant the winner's claim commits.
	ErrVersionConflict = errors.New("jobs: version conflict")

	// ErrIllegalTransition reports a state transition the lifecycle does not
	// allow, refused before the statement runs.
	ErrIllegalTransition = errors.New("jobs: illegal transition")
)

// Executor is the minimal database capability every store in this package
// needs. A [dbport.Tx] and a [dbport.Conn] both satisfy it.
//
// Every method takes it explicitly rather than holding a handle: migration
// 00034's tables are row-level-security protected, so the caller has to have
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

// isHex64 reports whether s is a well-formed 64-character lowercase hex
// digest, the shape migration 00002's content_digest domain enforces.
func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// allows reports whether graph permits from -> to.
func allows(graph map[string][]string, from, to string) bool {
	return slices.Contains(graph[from], to)
}

// validIdentifier mirrors the semantic_key domain (migration 00002):
// blank or surrounding-whitespace-padded values are refused before any
// statement runs, so callers get [ErrInvalid] instead of a raw CHECK
// violation from the database.
func validIdentifier(v string) bool {
	return v != "" && v == strings.TrimSpace(v)
}

// ---------------------------------------------------------------------------
// Job definitions.
// ---------------------------------------------------------------------------

// JobDefinition is one job_definition row: an immutable published revision of
// a governed job.
type JobDefinition struct {
	TenantID uuid.UUID
	JobID    string
	Version  uint64

	// DefinitionDigest is the content identity of the full published
	// definition. TriggerDigest is the internal/engines/schedule
	// PublishedTrigger.Digest this definition is scheduled by -- a value
	// reference, not a foreign key, because no durable trigger table exists
	// yet (see package doc). Both are plain 64-character hex, the same
	// convention internal/data/runtimestate uses for its own digest columns.
	DefinitionDigest string
	TriggerDigest    string

	// TargetDefinitionRef/TargetDefinitionVersion is the intent this job's
	// governed operation targets, in internal/intent.Ref's (type id, version)
	// shape. Also a value reference, not a foreign key.
	TargetDefinitionRef     string
	TargetDefinitionVersion uint64

	// Body is the opaque governed job spec: partitioning plan, connector or
	// import parameters, whatever the owning capability declares.
	Body []byte

	PublishedBy string
	PublishedAt time.Time
}

// DefinitionStore publishes and reads job_definition.
type DefinitionStore struct{}

// Publish records one immutable job definition revision. A second publish of
// the same (tenant, job_id, version) is [ErrDuplicate], not an update: a
// published job version is identity, and the table's forbid_mutation trigger
// refuses the rewrite even to the owning role. This is also the RED clause's
// "unversioned run" refused at its source -- a run can only ever reference a
// version that was published here and can never change under it.
func (s DefinitionStore) Publish(ctx context.Context, ex Executor, in JobDefinition) (JobDefinition, error) {
	if in.TenantID == uuid.Nil {
		return JobDefinition{}, invalid("tenant_id", "a job definition is tenant scoped")
	}
	if !validIdentifier(in.JobID) {
		return JobDefinition{}, invalid("job_id", "a job definition names a job with an unpadded identifier")
	}
	if in.Version == 0 {
		return JobDefinition{}, invalid("version", "version starts at 1")
	}
	if !isHex64(in.DefinitionDigest) {
		return JobDefinition{}, invalid("definition_digest", "digest is not 64 hex characters")
	}
	if !isHex64(in.TriggerDigest) {
		return JobDefinition{}, invalid("trigger_digest", "digest is not 64 hex characters")
	}
	if !validIdentifier(in.TargetDefinitionRef) {
		return JobDefinition{}, invalid("target_definition_ref", "a job definition names the intent it targets with an unpadded identifier")
	}
	if in.TargetDefinitionVersion == 0 {
		return JobDefinition{}, invalid("target_definition_version", "version starts at 1")
	}
	if len(in.Body) == 0 {
		return JobDefinition{}, invalid("body", "a job definition carries a governed job spec")
	}
	if in.PublishedBy == "" {
		return JobDefinition{}, invalid("published_by", "publisher is required")
	}
	if in.PublishedAt.IsZero() {
		return JobDefinition{}, invalid("published_at", "timestamp is unset")
	}

	affected, err := ex.Exec(ctx, `
		INSERT INTO job_definition (
			tenant_id, job_id, version, definition_digest, trigger_digest,
			target_definition_ref, target_definition_version, body,
			published_by, published_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.JobID, int64(in.Version), in.DefinitionDigest, in.TriggerDigest,
		in.TargetDefinitionRef, int64(in.TargetDefinitionVersion), in.Body,
		in.PublishedBy, in.PublishedAt.UTC())
	if err != nil {
		return JobDefinition{}, fmt.Errorf("jobs: publish %s/%d: %w", in.JobID, in.Version, err)
	}
	if affected == 0 {
		return JobDefinition{}, fmt.Errorf("%w: job_definition %s/%d", ErrDuplicate, in.JobID, in.Version)
	}
	in.PublishedAt = in.PublishedAt.UTC()
	return in, nil
}

// Load returns one published job definition.
func (s DefinitionStore) Load(ctx context.Context, ex Executor, tenantID uuid.UUID, jobID string, version uint64) (JobDefinition, error) {
	var (
		out           JobDefinition
		ver           int64
		targetVersion int64
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, job_id, version, definition_digest, trigger_digest,
			target_definition_ref, target_definition_version, body,
			published_by, published_at
		FROM job_definition
		WHERE tenant_id = $1 AND job_id = $2 AND version = $3`,
		tenantID, jobID, int64(version)).Scan(
		&out.TenantID, &out.JobID, &ver, &out.DefinitionDigest, &out.TriggerDigest,
		&out.TargetDefinitionRef, &targetVersion, &out.Body,
		&out.PublishedBy, &out.PublishedAt)
	if err != nil {
		if isNoRows(err) {
			return JobDefinition{}, fmt.Errorf("%w: job_definition %s/%d", ErrNotFound, jobID, version)
		}
		return JobDefinition{}, fmt.Errorf("jobs: load definition %s/%d: %w", jobID, version, err)
	}
	out.Version, out.TargetDefinitionVersion = uint64(ver), uint64(targetVersion)
	out.PublishedAt = out.PublishedAt.UTC()
	return out, nil
}

// ---------------------------------------------------------------------------
// Job runs.
// ---------------------------------------------------------------------------

// Job run states.
const (
	RunDeclared  = "DECLARED"
	RunRunning   = "RUNNING"
	RunCompleted = "COMPLETED"
	RunFailed    = "FAILED"
	RunCancelled = "CANCELLED"
)

var runTransitions = map[string][]string{
	RunDeclared:  {RunRunning, RunCancelled},
	RunRunning:   {RunCompleted, RunFailed, RunCancelled},
	RunCompleted: nil,
	RunFailed:    {RunDeclared},
	RunCancelled: nil,
}

// JobRun is one job_run row: one run of a published job definition.
type JobRun struct {
	TenantID   uuid.UUID
	RunID      uuid.UUID
	JobID      string
	JobVersion uint64

	State   string
	Attempt int
	Version uint64

	DeclaredBy    string
	DeclaredAt    time.Time
	StartedAt     time.Time
	CompletedAt   time.Time
	FailureDetail string
	Causal        *CausalMetadata
}

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

func causalArgs(c *CausalMetadata) []any {
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
	out := *c
	out.CorrelationID = boundedIdentifier(c.CorrelationID)
	out.CausationID = boundedIdentifier(c.CausationID)
	out.LogicalOperationID = boundedIdentifier(c.LogicalOperationID)
	out.AttemptID = boundedIdentifier(c.AttemptID)
	out.TraceLink = nil
	if l := c.TraceLink; l != nil && validTraceID(l.TraceID, 16) && validTraceID(l.SpanID, 8) && validTraceState(l.TraceState) && (l.ExpiresAt.IsZero() || l.ExpiresAt.After(time.Now())) {
		copy := *l
		if !copy.ExpiresAt.IsZero() {
			copy.ExpiresAt = copy.ExpiresAt.UTC()
		}
		out.TraceLink = &copy
	}
	if out.CorrelationID == "" && out.CausationID == "" && out.LogicalOperationID == "" && out.AttemptID == "" && out.TraceLink == nil {
		return nil
	}
	return &out
}
func boundedIdentifier(v string) string {
	if strings.TrimSpace(v) == "" || len(v) > 128 {
		return ""
	}
	return v
}

// requireLinkExpiry refuses a trace link without an expiry before any
// statement runs. The schema forbids persisting one — the run/partition
// completeness CHECKs demand trace_link_expires_at IS NOT NULL whenever a
// link is present, and the checkpoint link table declares it NOT NULL —
// and reads can never return one, so accepting it would only surface a raw
// driver error or a silently dropped link.
func requireLinkExpiry(c *CausalMetadata) error {
	if c != nil && c.TraceLink != nil && c.TraceLink.ExpiresAt.IsZero() {
		return invalid("trace_link_expires_at", "a stored trace link carries an expiry")
	}
	return nil
}
func validTraceID(v string, size int) bool {
	if v != strings.ToLower(v) {
		return false
	}
	b, err := hex.DecodeString(v)
	if err != nil || len(b) != size {
		return false
	}
	for _, x := range b {
		if x != 0 {
			return true
		}
	}
	return false
}
func validTraceState(v string) bool {
	if len(v) > 256 {
		return false
	}
	if v == "" {
		return true
	}
	for _, entry := range strings.Split(v, ",") {
		if !strings.Contains(entry, "=") {
			return false
		}
	}
	return true
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
	if traceID != nil && spanID != nil && flags != nil && *flags >= 0 && *flags <= 255 {
		c.TraceLink = &TraceLinkMetadata{TraceID: *traceID, SpanID: *spanID, TraceFlags: byte(*flags), TraceState: valueString(state), ExpiresAt: valueTime(expires)}
	}
	return normalizeCausal(c)
}
func valueString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func valueTime(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return v.UTC()
}
func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

func causalValue(c *CausalMetadata, field int) any {
	if c == nil {
		return nil
	}
	values := []string{c.CorrelationID, c.CausationID, c.LogicalOperationID, c.AttemptID}
	if values[field] == "" {
		return nil
	}
	return values[field]
}

// RunStore declares, advances and reads job_run.
type RunStore struct{}

// StartRun declares one new run against a published job definition. The
// caller supplies RunID: run identity is the caller's idempotency boundary
// the same way a redelivered request's identity is elsewhere in this
// codebase, so retrying a declare with the same RunID after a crash is
// [ErrDuplicate] rather than a second run. The row starts DECLARED at
// attempt 1; migration 00034's foreign key to job_definition is what makes
// declaring a run against an unpublished or unversioned job impossible.
func (s RunStore) StartRun(ctx context.Context, ex Executor, in JobRun) (JobRun, error) {
	if in.TenantID == uuid.Nil {
		return JobRun{}, invalid("tenant_id", "a job run is tenant scoped")
	}
	if in.RunID == uuid.Nil {
		return JobRun{}, invalid("run_id", "a run needs an identity")
	}
	if !validIdentifier(in.JobID) {
		return JobRun{}, invalid("job_id", "a run declares the job it runs with an unpadded identifier")
	}
	if in.JobVersion == 0 {
		return JobRun{}, invalid("job_version", "a run pins the exact published job version")
	}
	if in.DeclaredBy == "" {
		return JobRun{}, invalid("declared_by", "declarer is required")
	}
	if in.DeclaredAt.IsZero() {
		return JobRun{}, invalid("declared_at", "timestamp is unset")
	}
	in.Causal = normalizeCausal(in.Causal)
	if err := requireLinkExpiry(in.Causal); err != nil {
		return JobRun{}, err
	}
	if in.Causal != nil && in.Causal.LogicalOperationID != "" && in.Causal.AttemptID == "" {
		in.Causal.AttemptID = uuid.NewString()
	}
	args := []any{in.TenantID, in.RunID, in.JobID, int64(in.JobVersion), RunDeclared, in.DeclaredBy, in.DeclaredAt.UTC()}
	args = append(args, causalArgs(in.Causal)...)
	affected, err := ex.Exec(ctx, `
		INSERT INTO job_run (
			tenant_id, run_id, job_id, job_version, run_state, attempt,
			run_version, declared_by, declared_at, correlation_id, causation_id, logical_operation_id, attempt_id, trace_id, trace_span_id, trace_flags, trace_state, trace_link_expires_at)
		VALUES ($1, $2, $3, $4, $5, 1, 1, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT DO NOTHING`,
		args...)
	if err != nil {
		return JobRun{}, fmt.Errorf("jobs: declare run %s: %w", in.RunID, err)
	}
	if affected == 0 {
		return JobRun{}, fmt.Errorf("%w: job_run %s", ErrDuplicate, in.RunID)
	}
	in.State, in.Attempt, in.Version = RunDeclared, 1, 1
	in.DeclaredAt = in.DeclaredAt.UTC()
	in.StartedAt, in.CompletedAt, in.FailureDetail = time.Time{}, time.Time{}, ""
	return in, nil
}

// Begin transitions a run DECLARED -> RUNNING under compare-and-swap.
func (s RunStore) Begin(ctx context.Context, ex Executor, tenantID, runID uuid.UUID, expectedVersion uint64, at time.Time) (JobRun, error) {
	current, err := s.checkTransition(ctx, ex, tenantID, runID, RunRunning, expectedVersion)
	if err != nil {
		return JobRun{}, err
	}
	if at.IsZero() {
		return JobRun{}, invalid("started_at", "timestamp is unset")
	}
	affected, err := ex.Exec(ctx, `
		UPDATE job_run
		SET run_state = $4, run_version = run_version + 1, started_at = $5
		WHERE tenant_id = $1 AND run_id = $2 AND run_version = $3`,
		tenantID, runID, int64(expectedVersion), RunRunning, at.UTC())
	if err != nil {
		return JobRun{}, fmt.Errorf("jobs: begin run %s: %w", runID, err)
	}
	if affected == 0 {
		return JobRun{}, fmt.Errorf("%w: job_run %s expected version %d", ErrVersionConflict, runID, expectedVersion)
	}
	current.State, current.Version, current.StartedAt = RunRunning, expectedVersion+1, at.UTC()
	return current, nil
}

// Complete transitions a run RUNNING -> COMPLETED under compare-and-swap.
func (s RunStore) Complete(ctx context.Context, ex Executor, tenantID, runID uuid.UUID, expectedVersion uint64, at time.Time) (JobRun, error) {
	current, err := s.checkTransition(ctx, ex, tenantID, runID, RunCompleted, expectedVersion)
	if err != nil {
		return JobRun{}, err
	}
	if at.IsZero() {
		return JobRun{}, invalid("completed_at", "timestamp is unset")
	}
	affected, err := ex.Exec(ctx, `
		UPDATE job_run
		SET run_state = $4, run_version = run_version + 1, completed_at = $5
		WHERE tenant_id = $1 AND run_id = $2 AND run_version = $3`,
		tenantID, runID, int64(expectedVersion), RunCompleted, at.UTC())
	if err != nil {
		return JobRun{}, fmt.Errorf("jobs: complete run %s: %w", runID, err)
	}
	if affected == 0 {
		return JobRun{}, fmt.Errorf("%w: job_run %s expected version %d", ErrVersionConflict, runID, expectedVersion)
	}
	current.State, current.Version, current.CompletedAt = RunCompleted, expectedVersion+1, at.UTC()
	return current, nil
}

// Fail transitions a run RUNNING -> FAILED under compare-and-swap, recording
// detail. A retry that wants another attempt calls [RunStore.Retry]
// afterward, which is the only way out of FAILED.
func (s RunStore) Fail(ctx context.Context, ex Executor, tenantID, runID uuid.UUID, expectedVersion uint64, at time.Time, detail string) (JobRun, error) {
	current, err := s.checkTransition(ctx, ex, tenantID, runID, RunFailed, expectedVersion)
	if err != nil {
		return JobRun{}, err
	}
	if at.IsZero() {
		return JobRun{}, invalid("completed_at", "timestamp is unset")
	}
	if detail == "" {
		return JobRun{}, invalid("failure_detail", "a failed run records why")
	}
	affected, err := ex.Exec(ctx, `
		UPDATE job_run
		SET run_state = $4, run_version = run_version + 1, completed_at = $5, failure_detail = $6
		WHERE tenant_id = $1 AND run_id = $2 AND run_version = $3`,
		tenantID, runID, int64(expectedVersion), RunFailed, at.UTC(), detail)
	if err != nil {
		return JobRun{}, fmt.Errorf("jobs: fail run %s: %w", runID, err)
	}
	if affected == 0 {
		return JobRun{}, fmt.Errorf("%w: job_run %s expected version %d", ErrVersionConflict, runID, expectedVersion)
	}
	current.State, current.Version, current.CompletedAt, current.FailureDetail =
		RunFailed, expectedVersion+1, at.UTC(), detail
	return current, nil
}

// Cancel transitions a run DECLARED or RUNNING -> CANCELLED under
// compare-and-swap.
func (s RunStore) Cancel(ctx context.Context, ex Executor, tenantID, runID uuid.UUID, expectedVersion uint64, at time.Time) (JobRun, error) {
	current, err := s.checkTransition(ctx, ex, tenantID, runID, RunCancelled, expectedVersion)
	if err != nil {
		return JobRun{}, err
	}
	if at.IsZero() {
		return JobRun{}, invalid("completed_at", "timestamp is unset")
	}
	affected, err := ex.Exec(ctx, `
		UPDATE job_run
		SET run_state = $4, run_version = run_version + 1, completed_at = $5
		WHERE tenant_id = $1 AND run_id = $2 AND run_version = $3`,
		tenantID, runID, int64(expectedVersion), RunCancelled, at.UTC())
	if err != nil {
		return JobRun{}, fmt.Errorf("jobs: cancel run %s: %w", runID, err)
	}
	if affected == 0 {
		return JobRun{}, fmt.Errorf("%w: job_run %s expected version %d", ErrVersionConflict, runID, expectedVersion)
	}
	current.State, current.Version, current.CompletedAt = RunCancelled, expectedVersion+1, at.UTC()
	return current, nil
}

// Retry moves a FAILED run back to DECLARED for a new attempt, under
// compare-and-swap, incrementing attempt and clearing the prior attempt's
// started/completed/failure fields. This is where "attempt count" lives: a
// redrive is a new attempt on the same run identity, not a new run.
func (s RunStore) Retry(ctx context.Context, ex Executor, tenantID, runID uuid.UUID, expectedVersion uint64, at time.Time) (JobRun, error) {
	current, err := s.checkTransition(ctx, ex, tenantID, runID, RunDeclared, expectedVersion)
	if err != nil {
		return JobRun{}, err
	}
	if at.IsZero() {
		return JobRun{}, invalid("declared_at", "timestamp is unset")
	}
	// A retry is a new finite attempt, while the logical operation remains
	// stable. Attempt identity is diagnostic only and never an idempotency key.
	newAttemptID := uuid.NewString()
	affected, err := ex.Exec(ctx, `
		UPDATE job_run
		SET run_state = $4, run_version = run_version + 1, attempt = attempt + 1,
			declared_at = $5, started_at = NULL, completed_at = NULL, failure_detail = NULL, attempt_id = $6
		WHERE tenant_id = $1 AND run_id = $2 AND run_version = $3`,
		tenantID, runID, int64(expectedVersion), RunDeclared, at.UTC(), newAttemptID)
	if err != nil {
		return JobRun{}, fmt.Errorf("jobs: retry run %s: %w", runID, err)
	}
	if affected == 0 {
		return JobRun{}, fmt.Errorf("%w: job_run %s expected version %d", ErrVersionConflict, runID, expectedVersion)
	}
	current.State, current.Version, current.Attempt = RunDeclared, expectedVersion+1, current.Attempt+1
	current.DeclaredAt = at.UTC()
	if current.Causal != nil {
		current.Causal.AttemptID = newAttemptID
	}
	current.StartedAt, current.CompletedAt, current.FailureDetail = time.Time{}, time.Time{}, ""
	return current, nil
}

// checkTransition loads the current row and refuses a stale version before
// it refuses an illegal step, mirroring the partition CAS-first classifier:
// a caller presenting a version that is no longer current is told
// [ErrVersionConflict] however the state has moved, while a current
// version on a disallowed step is [ErrIllegalTransition].
func (s RunStore) checkTransition(ctx context.Context, ex Executor, tenantID, runID uuid.UUID, next string, expectedVersion uint64) (JobRun, error) {
	current, err := s.Load(ctx, ex, tenantID, runID)
	if err != nil {
		return JobRun{}, err
	}
	if current.Version != expectedVersion {
		return JobRun{}, fmt.Errorf("%w: job_run %s expected version %d", ErrVersionConflict, runID, expectedVersion)
	}
	if !allows(runTransitions, current.State, next) {
		return JobRun{}, fmt.Errorf("%w: job_run %s: %s -> %s", ErrIllegalTransition, runID, current.State, next)
	}
	return current, nil
}

// Load returns one run.
func (s RunStore) Load(ctx context.Context, ex Executor, tenantID, runID uuid.UUID) (JobRun, error) {
	var (
		out                                                                   JobRun
		jobVersion                                                            int64
		version                                                               int64
		startedAt                                                             *time.Time
		completedAt                                                           *time.Time
		failure                                                               *string
		correlation, causation, logical, attempt, traceID, spanID, traceState *string
		traceFlags                                                            *int16
		expires                                                               *time.Time
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, run_id, job_id, job_version, run_state, attempt,
			run_version, declared_by, declared_at, started_at, completed_at, failure_detail,
			correlation_id, causation_id, logical_operation_id, attempt_id, trace_id, trace_span_id, trace_flags, trace_state, trace_link_expires_at
		FROM job_run
		WHERE tenant_id = $1 AND run_id = $2`, tenantID, runID).Scan(
		&out.TenantID, &out.RunID, &out.JobID, &jobVersion, &out.State, &out.Attempt,
		&version, &out.DeclaredBy, &out.DeclaredAt, &startedAt, &completedAt, &failure,
		&correlation, &causation, &logical, &attempt, &traceID, &spanID, &traceFlags, &traceState, &expires)
	if err != nil {
		if isNoRows(err) {
			return JobRun{}, fmt.Errorf("%w: job_run %s", ErrNotFound, runID)
		}
		return JobRun{}, fmt.Errorf("jobs: load run %s: %w", runID, err)
	}
	out.JobVersion, out.Version = uint64(jobVersion), uint64(version)
	out.DeclaredAt = out.DeclaredAt.UTC()
	if startedAt != nil {
		out.StartedAt = startedAt.UTC()
	}
	if completedAt != nil {
		out.CompletedAt = completedAt.UTC()
	}
	if failure != nil {
		out.FailureDetail = *failure
	}
	out.Causal = causalFromPointers(correlation, causation, logical, attempt, traceID, spanID, traceFlags, traceState, expires)
	return out, nil
}

// ---------------------------------------------------------------------------
// Job partitions.
// ---------------------------------------------------------------------------

// Job partition states.
const (
	PartitionPending   = "PENDING"
	PartitionClaimed   = "CLAIMED"
	PartitionCompleted = "COMPLETED"
	PartitionFailed    = "FAILED"
	PartitionCancelled = "CANCELLED"
)

// JobPartition is one job_partition row: one partition of a run, with its
// own compare-and-swap fenced state and a deterministic partition key.
type JobPartition struct {
	TenantID     uuid.UUID
	PartitionID  uuid.UUID
	RunID        uuid.UUID
	PartitionKey string

	State         string
	ClaimedBy     string
	ClaimedAt     time.Time
	FailureDetail string
	Version       uint64

	CreatedAt   time.Time
	CompletedAt time.Time
	Attempt     int
	Causal      *CausalMetadata
}

// PartitionStore creates, claims, advances and reads job_partition.
type PartitionStore struct{}

// Create records one partition of a run, PENDING and unclaimed. A repeated
// create for a partition key the run already has is [ErrDuplicate]: the same
// partitioning decision replayed after a crash names the same key, and this
// is what refuses forking a second row for it rather than resuming the one
// that exists.
func (s PartitionStore) Create(ctx context.Context, ex Executor, in JobPartition) (JobPartition, error) {
	if in.TenantID == uuid.Nil {
		return JobPartition{}, invalid("tenant_id", "a partition is tenant scoped")
	}
	if in.PartitionID == uuid.Nil {
		return JobPartition{}, invalid("partition_id", "a partition needs an identity")
	}
	if in.RunID == uuid.Nil {
		return JobPartition{}, invalid("run_id", "a partition belongs to a run")
	}
	if !validIdentifier(in.PartitionKey) {
		return JobPartition{}, invalid("partition_key", "a partition carries a deterministic unpadded key")
	}
	if in.CreatedAt.IsZero() {
		return JobPartition{}, invalid("created_at", "timestamp is unset")
	}
	in.Causal = normalizeCausal(in.Causal)
	if err := requireLinkExpiry(in.Causal); err != nil {
		return JobPartition{}, err
	}
	args := []any{in.TenantID, in.PartitionID, in.RunID, in.PartitionKey, PartitionPending, in.CreatedAt.UTC()}
	args = append(args, causalArgs(in.Causal)...)
	affected, err := ex.Exec(ctx, `
		INSERT INTO job_partition (
			tenant_id, partition_id, run_id, partition_key, partition_state,
			partition_version, created_at, attempt, correlation_id, causation_id, logical_operation_id, attempt_id, trace_id, trace_span_id, trace_flags, trace_state, trace_link_expires_at)
		VALUES ($1, $2, $3, $4, $5, 1, $6, 0, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT ON CONSTRAINT job_partition_key_unique DO NOTHING`,
		args...)
	if err != nil {
		return JobPartition{}, fmt.Errorf("jobs: create partition %s/%s: %w", in.RunID, in.PartitionKey, err)
	}
	if affected == 0 {
		return JobPartition{}, fmt.Errorf("%w: job_partition %s/%s", ErrDuplicate, in.RunID, in.PartitionKey)
	}
	in.State, in.Version = PartitionPending, 1
	in.CreatedAt = in.CreatedAt.UTC()
	return in, nil
}

// ClaimPartition takes an unclaimed partition under compare-and-swap. Exactly
// one concurrent claimant wins: the winner's UPDATE advances
// partition_version, so every other claimant's WHERE clause, presenting the
// same expectedVersion, matches zero rows and receives
// [ErrVersionConflict] rather than partially applying its claim.
func (s PartitionStore) ClaimPartition(ctx context.Context, ex Executor, tenantID, partitionID uuid.UUID, expectedVersion uint64, holder string, at time.Time) (JobPartition, error) {
	if !validIdentifier(holder) {
		return JobPartition{}, invalid("claimed_by", "a claim names its holder with an unpadded identifier")
	}
	if at.IsZero() {
		return JobPartition{}, invalid("claimed_at", "timestamp is unset")
	}
	newAttemptID := uuid.NewString()
	// The compare-and-swap runs before any state inspection: a concurrent
	// claimant that already moved the row must deterministically receive
	// ErrVersionConflict, never an illegal-transition diagnosed off a row
	// that changed under the read.
	affected, err := ex.Exec(ctx, `
		UPDATE job_partition
		SET partition_state = $4, claimed_by = $5, claimed_at = $6, attempt = attempt + 1, partition_version = partition_version + 1
		, attempt_id = $8
		WHERE tenant_id = $1 AND partition_id = $2 AND partition_version = $3 AND partition_state = $7`,
		tenantID, partitionID, int64(expectedVersion), PartitionClaimed, holder, at.UTC(), PartitionPending, newAttemptID)
	if err != nil {
		return JobPartition{}, fmt.Errorf("jobs: claim partition %s: %w", partitionID, err)
	}
	if affected == 1 {
		claimed, err := s.Load(ctx, ex, tenantID, partitionID)
		if err != nil {
			return JobPartition{}, fmt.Errorf("jobs: reload claimed partition %s: %w", partitionID, err)
		}
		return claimed, nil
	}
	return JobPartition{}, s.classifyUnmatchedTransition(ctx, ex, tenantID, partitionID, expectedVersion, PartitionClaimed)
}

// classifyUnmatchedTransition tells a lost compare-and-swap apart from a
// genuinely illegal transition: when the row moved under the caller it is
// ErrVersionConflict; when the version still matches but the state
// disallows the step it is ErrIllegalTransition.
func (s PartitionStore) classifyUnmatchedTransition(ctx context.Context, ex Executor, tenantID, partitionID uuid.UUID, expectedVersion uint64, next string) error {
	current, err := s.Load(ctx, ex, tenantID, partitionID)
	if err != nil {
		return err
	}
	if current.Version != expectedVersion {
		return fmt.Errorf("%w: job_partition %s expected version %d", ErrVersionConflict, partitionID, expectedVersion)
	}
	return fmt.Errorf("%w: job_partition %s: %s -> %s", ErrIllegalTransition, partitionID, current.State, next)
}

// Complete transitions a partition CLAIMED -> COMPLETED under
// compare-and-swap.
func (s PartitionStore) Complete(ctx context.Context, ex Executor, tenantID, partitionID uuid.UUID, expectedVersion uint64, at time.Time) (JobPartition, error) {
	if at.IsZero() {
		return JobPartition{}, invalid("completed_at", "timestamp is unset")
	}
	// Compare-and-swap first (see ClaimPartition): the state predicate keeps
	// the old checkTransition guard inside the atomic statement, so a lost
	// race reports ErrVersionConflict while a genuinely illegal step still
	// reports ErrIllegalTransition.
	affected, err := ex.Exec(ctx, `
		UPDATE job_partition
		SET partition_state = $4, partition_version = partition_version + 1, completed_at = $5
		WHERE tenant_id = $1 AND partition_id = $2 AND partition_version = $3 AND partition_state = $6`,
		tenantID, partitionID, int64(expectedVersion), PartitionCompleted, at.UTC(), PartitionClaimed)
	if err != nil {
		return JobPartition{}, fmt.Errorf("jobs: complete partition %s: %w", partitionID, err)
	}
	if affected == 1 {
		completed, err := s.Load(ctx, ex, tenantID, partitionID)
		if err != nil {
			return JobPartition{}, fmt.Errorf("jobs: reload completed partition %s: %w", partitionID, err)
		}
		return completed, nil
	}
	return JobPartition{}, s.classifyUnmatchedTransition(ctx, ex, tenantID, partitionID, expectedVersion, PartitionCompleted)
}

// Fail transitions a partition CLAIMED -> FAILED under compare-and-swap,
// recording detail.
func (s PartitionStore) Fail(ctx context.Context, ex Executor, tenantID, partitionID uuid.UUID, expectedVersion uint64, at time.Time, detail string) (JobPartition, error) {
	if at.IsZero() {
		return JobPartition{}, invalid("completed_at", "timestamp is unset")
	}
	if detail == "" {
		return JobPartition{}, invalid("failure_detail", "a failed partition records why")
	}
	// Compare-and-swap first (see ClaimPartition).
	affected, err := ex.Exec(ctx, `
		UPDATE job_partition
		SET partition_state = $4, partition_version = partition_version + 1, completed_at = $5, failure_detail = $6
		WHERE tenant_id = $1 AND partition_id = $2 AND partition_version = $3 AND partition_state = $7`,
		tenantID, partitionID, int64(expectedVersion), PartitionFailed, at.UTC(), detail, PartitionClaimed)
	if err != nil {
		return JobPartition{}, fmt.Errorf("jobs: fail partition %s: %w", partitionID, err)
	}
	if affected == 1 {
		failed, err := s.Load(ctx, ex, tenantID, partitionID)
		if err != nil {
			return JobPartition{}, fmt.Errorf("jobs: reload failed partition %s: %w", partitionID, err)
		}
		return failed, nil
	}
	return JobPartition{}, s.classifyUnmatchedTransition(ctx, ex, tenantID, partitionID, expectedVersion, PartitionFailed)
}

// Cancel transitions a partition PENDING or CLAIMED -> CANCELLED under
// compare-and-swap.
func (s PartitionStore) Cancel(ctx context.Context, ex Executor, tenantID, partitionID uuid.UUID, expectedVersion uint64, at time.Time) (JobPartition, error) {
	if at.IsZero() {
		return JobPartition{}, invalid("completed_at", "timestamp is unset")
	}
	// Compare-and-swap first (see ClaimPartition). Cancel accepts PENDING
	// or CLAIMED sources, so the predicate lists both.
	affected, err := ex.Exec(ctx, `
		UPDATE job_partition
		SET partition_state = $5, partition_version = partition_version + 1, completed_at = $6
		WHERE tenant_id = $1 AND partition_id = $2 AND partition_version = $3 AND partition_state IN ($4, $7)`,
		tenantID, partitionID, int64(expectedVersion), PartitionPending, PartitionCancelled, at.UTC(), PartitionClaimed)
	if err != nil {
		return JobPartition{}, fmt.Errorf("jobs: cancel partition %s: %w", partitionID, err)
	}
	if affected == 1 {
		cancelled, err := s.Load(ctx, ex, tenantID, partitionID)
		if err != nil {
			return JobPartition{}, fmt.Errorf("jobs: reload cancelled partition %s: %w", partitionID, err)
		}
		return cancelled, nil
	}
	return JobPartition{}, s.classifyUnmatchedTransition(ctx, ex, tenantID, partitionID, expectedVersion, PartitionCancelled)
}

// Load returns one partition.
func (s PartitionStore) Load(ctx context.Context, ex Executor, tenantID, partitionID uuid.UUID) (JobPartition, error) {
	var (
		out                                                                   JobPartition
		version                                                               int64
		claimedBy                                                             *string
		claimedAt                                                             *time.Time
		completedAt                                                           *time.Time
		failure                                                               *string
		correlation, causation, logical, attempt, traceID, spanID, traceState *string
		traceFlags                                                            *int16
		expires                                                               *time.Time
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, partition_id, run_id, partition_key, partition_state,
			claimed_by, claimed_at, failure_detail, partition_version, created_at, completed_at, attempt,
			correlation_id, causation_id, logical_operation_id, attempt_id, trace_id, trace_span_id, trace_flags, trace_state, trace_link_expires_at
		FROM job_partition
		WHERE tenant_id = $1 AND partition_id = $2`, tenantID, partitionID).Scan(
		&out.TenantID, &out.PartitionID, &out.RunID, &out.PartitionKey, &out.State,
		&claimedBy, &claimedAt, &failure, &version, &out.CreatedAt, &completedAt, &out.Attempt,
		&correlation, &causation, &logical, &attempt, &traceID, &spanID, &traceFlags, &traceState, &expires)
	if err != nil {
		if isNoRows(err) {
			return JobPartition{}, fmt.Errorf("%w: job_partition %s", ErrNotFound, partitionID)
		}
		return JobPartition{}, fmt.Errorf("jobs: load partition %s: %w", partitionID, err)
	}
	out.Version = uint64(version)
	out.CreatedAt = out.CreatedAt.UTC()
	if claimedBy != nil {
		out.ClaimedBy = *claimedBy
	}
	if claimedAt != nil {
		out.ClaimedAt = claimedAt.UTC()
	}
	if completedAt != nil {
		out.CompletedAt = completedAt.UTC()
	}
	if failure != nil {
		out.FailureDetail = *failure
	}
	out.Causal = causalFromPointers(correlation, causation, logical, attempt, traceID, spanID, traceFlags, traceState, expires)
	return out, nil
}

// ListByRun returns a run's partitions ordered by partition key, for
// verifying exact partition counts after a crash/restart.
func (s PartitionStore) ListByRun(ctx context.Context, ex Executor, tenantID, runID uuid.UUID) ([]JobPartition, error) {
	rows, err := ex.Query(ctx, `
		SELECT tenant_id, partition_id, run_id, partition_key, partition_state,
			claimed_by, claimed_at, failure_detail, partition_version, created_at, completed_at, attempt,
			correlation_id, causation_id, logical_operation_id, attempt_id, trace_id, trace_span_id, trace_flags, trace_state, trace_link_expires_at
		FROM job_partition
		WHERE tenant_id = $1 AND run_id = $2
		ORDER BY partition_key`, tenantID, runID)
	if err != nil {
		return nil, fmt.Errorf("jobs: read partitions of %s: %w", runID, err)
	}
	defer rows.Close()

	var out []JobPartition
	for rows.Next() {
		var (
			p                                                                     JobPartition
			version                                                               int64
			claimedBy                                                             *string
			claimedAt                                                             *time.Time
			completedAt                                                           *time.Time
			failure                                                               *string
			correlation, causation, logical, attempt, traceID, spanID, traceState *string
			traceFlags                                                            *int16
			expires                                                               *time.Time
		)
		if err := rows.Scan(&p.TenantID, &p.PartitionID, &p.RunID, &p.PartitionKey, &p.State,
			&claimedBy, &claimedAt, &failure, &version, &p.CreatedAt, &completedAt, &p.Attempt,
			&correlation, &causation, &logical, &attempt, &traceID, &spanID, &traceFlags, &traceState, &expires); err != nil {
			return nil, fmt.Errorf("jobs: scan partition: %w", err)
		}
		p.Version = uint64(version)
		p.CreatedAt = p.CreatedAt.UTC()
		if claimedBy != nil {
			p.ClaimedBy = *claimedBy
		}
		if claimedAt != nil {
			p.ClaimedAt = claimedAt.UTC()
		}
		if completedAt != nil {
			p.CompletedAt = completedAt.UTC()
		}
		if failure != nil {
			p.FailureDetail = *failure
		}
		p.Causal = causalFromPointers(correlation, causation, logical, attempt, traceID, spanID, traceFlags, traceState, expires)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("jobs: read partitions of %s: %w", runID, err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Job checkpoints.
// ---------------------------------------------------------------------------

// JobCheckpoint is one job_checkpoint row: an append-only, numbered
// checkpoint for one partition.
type JobCheckpoint struct {
	TenantID    uuid.UUID
	PartitionID uuid.UUID
	Sequence    uint64

	StateDigest      string
	PartitionVersion uint64

	TakenAt time.Time
	Causal  *CausalMetadata
}

// CheckpointStore appends and reads job_checkpoint.
type CheckpointStore struct{}

// PruneExpiredTraceLinks removes at most limit expired operational links for
// one tenant across runs, partitions and checkpoints. Business identifiers,
// lifecycle state, results and compare-and-swap versions are unchanged.
func (s CheckpointStore) PruneExpiredTraceLinks(ctx context.Context, ex Executor, tenantID uuid.UUID, before time.Time, limit int) (int64, error) {
	if tenantID == uuid.Nil {
		return 0, invalid("tenant_id", "trace-link retention is tenant scoped")
	}
	if before.IsZero() {
		return 0, invalid("before", "timestamp is unset")
	}
	if limit < 1 || limit > 1000 {
		return 0, invalid("limit", "must be between 1 and 1000")
	}
	var affected int64
	err := ex.QueryRow(ctx,
		`SELECT hcmnext_prune_expired_job_trace_links($1,$2,$3)`,
		tenantID, before.UTC(), limit).Scan(&affected)
	if err != nil {
		return 0, fmt.Errorf("jobs: prune expired trace links: %w", err)
	}
	return affected, nil
}

// Checkpoint records one immutable checkpoint. A repeated sequence is
// [ErrDuplicate]: a safe point that could be rewritten is not a safe point,
// and the table's forbid_mutation trigger refuses the rewrite regardless.
func (s CheckpointStore) Checkpoint(ctx context.Context, ex Executor, in JobCheckpoint) (JobCheckpoint, error) {
	if in.TenantID == uuid.Nil {
		return JobCheckpoint{}, invalid("tenant_id", "a checkpoint is tenant scoped")
	}
	if in.PartitionID == uuid.Nil {
		return JobCheckpoint{}, invalid("partition_id", "a checkpoint belongs to a partition")
	}
	if in.Sequence == 0 {
		return JobCheckpoint{}, invalid("checkpoint_sequence", "sequence starts at 1")
	}
	if !isHex64(in.StateDigest) {
		return JobCheckpoint{}, invalid("state_digest", "digest is not 64 hex characters")
	}
	if in.PartitionVersion == 0 {
		return JobCheckpoint{}, invalid("partition_version", "a checkpoint records the partition version it describes")
	}
	if in.TakenAt.IsZero() {
		return JobCheckpoint{}, invalid("taken_at", "timestamp is unset")
	}
	in.Causal = normalizeCausal(in.Causal)
	if err := requireLinkExpiry(in.Causal); err != nil {
		return JobCheckpoint{}, err
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO job_checkpoint (
			tenant_id, partition_id, checkpoint_sequence, state_digest, partition_version, taken_at, correlation_id, causation_id, logical_operation_id, attempt_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.PartitionID, int64(in.Sequence), in.StateDigest, int64(in.PartitionVersion), in.TakenAt.UTC(),
		causalValue(in.Causal, 0), causalValue(in.Causal, 1), causalValue(in.Causal, 2), causalValue(in.Causal, 3))
	if err != nil {
		return JobCheckpoint{}, fmt.Errorf("jobs: checkpoint %d of %s: %w", in.Sequence, in.PartitionID, err)
	}
	if affected == 0 {
		return JobCheckpoint{}, fmt.Errorf("%w: job_checkpoint %s/%d", ErrDuplicate, in.PartitionID, in.Sequence)
	}
	if in.Causal != nil && in.Causal.TraceLink != nil {
		link := in.Causal.TraceLink
		if _, err := ex.Exec(ctx, `INSERT INTO job_checkpoint_trace_link (tenant_id, partition_id, checkpoint_sequence, trace_id, trace_span_id, trace_flags, trace_state, expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, in.TenantID, in.PartitionID, int64(in.Sequence), link.TraceID, link.SpanID, link.TraceFlags, link.TraceState, link.ExpiresAt.UTC()); err != nil {
			return JobCheckpoint{}, fmt.Errorf("jobs: checkpoint trace link %d of %s: %w", in.Sequence, in.PartitionID, err)
		}
	}
	in.TakenAt = in.TakenAt.UTC()
	return in, nil
}

// Latest returns the highest-numbered checkpoint of a partition.
func (s CheckpointStore) Latest(ctx context.Context, ex Executor, tenantID, partitionID uuid.UUID) (JobCheckpoint, error) {
	var (
		out                                                                   JobCheckpoint
		sequence                                                              int64
		version                                                               int64
		correlation, causation, logical, attempt, traceID, spanID, traceState *string
		traceFlags                                                            *int16
		expires                                                               *time.Time
	)
	err := ex.QueryRow(ctx, `
		SELECT c.tenant_id, c.partition_id, c.checkpoint_sequence, c.state_digest, c.partition_version, c.taken_at,
			c.correlation_id, c.causation_id, c.logical_operation_id, c.attempt_id, l.trace_id, l.trace_span_id, l.trace_flags, l.trace_state, l.expires_at
		FROM job_checkpoint c
		LEFT JOIN job_checkpoint_trace_link l ON l.tenant_id = c.tenant_id AND l.partition_id = c.partition_id AND l.checkpoint_sequence = c.checkpoint_sequence AND l.expires_at > now()
		WHERE c.tenant_id = $1 AND c.partition_id = $2
		ORDER BY c.checkpoint_sequence DESC
		LIMIT 1`, tenantID, partitionID).Scan(
		&out.TenantID, &out.PartitionID, &sequence, &out.StateDigest, &version, &out.TakenAt,
		&correlation, &causation, &logical, &attempt, &traceID, &spanID, &traceFlags, &traceState, &expires)
	if err != nil {
		if isNoRows(err) {
			return JobCheckpoint{}, fmt.Errorf("%w: no checkpoint for partition %s", ErrNotFound, partitionID)
		}
		return JobCheckpoint{}, fmt.Errorf("jobs: read checkpoints of %s: %w", partitionID, err)
	}
	out.Sequence, out.PartitionVersion = uint64(sequence), uint64(version)
	out.TakenAt = out.TakenAt.UTC()
	out.Causal = causalFromPointers(correlation, causation, logical, attempt, traceID, spanID, traceFlags, traceState, expires)
	return out, nil
}

// List returns every checkpoint of a partition, oldest first.
func (s CheckpointStore) List(ctx context.Context, ex Executor, tenantID, partitionID uuid.UUID) ([]JobCheckpoint, error) {
	rows, err := ex.Query(ctx, `
		SELECT c.tenant_id, c.partition_id, c.checkpoint_sequence, c.state_digest, c.partition_version, c.taken_at,
			c.correlation_id, c.causation_id, c.logical_operation_id, c.attempt_id, l.trace_id, l.trace_span_id, l.trace_flags, l.trace_state, l.expires_at
		FROM job_checkpoint c
		LEFT JOIN job_checkpoint_trace_link l ON l.tenant_id = c.tenant_id AND l.partition_id = c.partition_id AND l.checkpoint_sequence = c.checkpoint_sequence AND l.expires_at > now()
		WHERE c.tenant_id = $1 AND c.partition_id = $2
		ORDER BY c.checkpoint_sequence`, tenantID, partitionID)
	if err != nil {
		return nil, fmt.Errorf("jobs: read checkpoints of %s: %w", partitionID, err)
	}
	defer rows.Close()

	var out []JobCheckpoint
	for rows.Next() {
		var (
			c                                                                     JobCheckpoint
			sequence                                                              int64
			version                                                               int64
			correlation, causation, logical, attempt, traceID, spanID, traceState *string
			traceFlags                                                            *int16
			expires                                                               *time.Time
		)
		if err := rows.Scan(&c.TenantID, &c.PartitionID, &sequence, &c.StateDigest, &version, &c.TakenAt,
			&correlation, &causation, &logical, &attempt, &traceID, &spanID, &traceFlags, &traceState, &expires); err != nil {
			return nil, fmt.Errorf("jobs: scan checkpoint: %w", err)
		}
		c.Sequence, c.PartitionVersion = uint64(sequence), uint64(version)
		c.TakenAt = c.TakenAt.UTC()
		c.Causal = causalFromPointers(correlation, causation, logical, attempt, traceID, spanID, traceFlags, traceState, expires)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("jobs: read checkpoints of %s: %w", partitionID, err)
	}
	return out, nil
}
