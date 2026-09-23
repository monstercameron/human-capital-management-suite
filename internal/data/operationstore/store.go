// Package operationstore persists owner-scoped long-running operations over
// PostgreSQL. The adapter owns transaction boundaries and establishes the RLS
// tenant setting before every query; transport remains responsible for the
// caller's owner projection.
package operationstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// DB is the transaction-opening capability owned by this adapter.
type DB interface{ dbport.Beginner }

// Record is the durable operation representation owned by this data adapter.
type Record struct {
	OperationID string
	TenantID    string
	Owner       string
	RequestType string
	State       State
	ResultBytes []byte
	ErrorBytes  []byte
	MetadataRef string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// State is the storage vocabulary for an operation lifecycle.
type State string

const (
	StatePending               State = "PENDING"
	StateRunning               State = "RUNNING"
	StateSucceeded             State = "SUCCEEDED"
	StateFailed                State = "FAILED"
	StateCancellationRequested State = "CANCELLATION_REQUESTED"
	StateCancelled             State = "CANCELLED"
)

func (s State) Terminal() bool {
	return s == StateSucceeded || s == StateFailed || s == StateCancelled
}

// Store implements the PostgreSQL operation persistence contract.
type Store struct {
	db       DB
	tenantID func(string) string
	now      func() time.Time
}

var (
	ErrInvalid        = errors.New("operationstore: invalid operation")
	ErrFenced         = errors.New("operationstore: fenced state transition")
	ErrNotFound       = errors.New("operationstore: operation not found")
	ErrNotCancellable = errors.New("operationstore: operation is not cancellable")
)

// New returns a PostgreSQL-backed operation store. The optional tenant mapper
// resolves the human-facing tenant key carried by trusted transport context to
// the UUID used by the database. Without one, tenant must already be a UUID,
// which keeps small adapter tests and migrations independent of application
// composition.
func New(db DB, tenantMapper ...func(string) string) *Store {
	var mapper func(string) string
	if len(tenantMapper) > 0 {
		mapper = tenantMapper[0]
	}
	return &Store{db: db, tenantID: mapper, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Store) resolveTenant(tenant string) (uuid.UUID, error) {
	tenant = strings.TrimSpace(tenant)
	if tenant == "" {
		return uuid.Nil, fmt.Errorf("%w: tenant id is required", ErrInvalid)
	}
	if s != nil && s.tenantID != nil {
		mapped := strings.TrimSpace(s.tenantID(tenant))
		if mapped != "" {
			id, err := uuid.Parse(mapped)
			if err == nil && id != uuid.Nil {
				return id, nil
			}
		}
	}
	id, err := uuid.Parse(tenant)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: tenant %q is not a non-nil UUID", ErrInvalid, tenant)
	}
	return id, nil
}

func (s *Store) beginTenant(ctx context.Context, tenant string) (dbport.Tx, uuid.UUID, error) {
	if s == nil || s.db == nil {
		return nil, uuid.Nil, fmt.Errorf("%w: database capability is required", ErrInvalid)
	}
	id, err := s.resolveTenant(tenant)
	if err != nil {
		return nil, uuid.Nil, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("operationstore: begin: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, id); err != nil {
		_ = tx.Rollback(ctx)
		return nil, uuid.Nil, err
	}
	return tx, id, nil
}

// Put inserts one operation. It is used by the long-running work producer;
// the operation endpoint itself only exposes Get and Cancel.
func (s *Store) Put(ctx context.Context, record Record) error {
	if strings.TrimSpace(record.OperationID) == "" || strings.TrimSpace(record.TenantID) == "" || strings.TrimSpace(record.Owner) == "" {
		return fmt.Errorf("%w: operation id, tenant id and owner are required", ErrInvalid)
	}
	if !validState(record.State) {
		return fmt.Errorf("%w: state %q is not declared", ErrInvalid, record.State)
	}
	tx, tenant, err := s.beginTenant(ctx, record.TenantID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, detail, err := marshalPayloads(record)
	if err != nil {
		return err
	}
	created := record.CreatedAt.UTC()
	if record.CreatedAt.IsZero() {
		created = s.now().UTC()
	}
	updated := record.UpdatedAt.UTC()
	if record.UpdatedAt.IsZero() {
		updated = created
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO operation
			(tenant_id, operation_id, owner_subject, request_type, state,
			 result_bytes, error_bytes, metadata_ref, created_at, updated_at, fence_token)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 1)`,
		tenant, strings.TrimSpace(record.OperationID), strings.TrimSpace(record.Owner),
		strings.TrimSpace(record.RequestType), string(record.State), result, detail,
		strings.TrimSpace(record.MetadataRef), created, updated)
	if err != nil {
		return fmt.Errorf("operationstore: insert operation: %w", err)
	}
	return tx.Commit(ctx)
}

// Get returns one operation in the named tenant. A missing row is deliberately
// indistinguishable from an operation in another tenant.
func (s *Store) Get(ctx context.Context, tenant, operationID string) (Record, error) {
	tx, tenantUUID, err := s.beginTenant(ctx, tenant)
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback(ctx)
	record, err := readRecord(ctx, tx, tenantUUID, operationID)
	if err != nil {
		return Record{}, err
	}
	record.TenantID = strings.TrimSpace(tenant)
	return record, nil
}

// Cancel records one idempotent cancellation request and atomically advances
// an active operation. The row lock plus fence predicate means a stale caller
// cannot overwrite a newer state, while operation_cancel makes a replay of
// the same request converge to the same durable result.
func (s *Store) Cancel(ctx context.Context, tenant, operationID, idempotencyKey, reasonRef string) (Record, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Record{}, fmt.Errorf("%w: idempotency key is required", ErrInvalid)
	}
	tx, tenantUUID, err := s.beginTenant(ctx, tenant)
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback(ctx)
	current, err := readRecordForUpdate(ctx, tx, tenantUUID, operationID)
	if err != nil {
		return Record{}, err
	}
	rows, err := tx.Exec(ctx, `
		INSERT INTO operation_cancel
			(tenant_id, operation_id, idempotency_key, reason_ref, requested_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, operation_id, idempotency_key) DO NOTHING`,
		tenantUUID, operationID, strings.TrimSpace(idempotencyKey), strings.TrimSpace(reasonRef), s.now().UTC())
	if err != nil {
		return Record{}, fmt.Errorf("operationstore: record cancellation: %w", err)
	}
	if rows == 0 {
		current.TenantID = strings.TrimSpace(tenant)
		if err := tx.Commit(ctx); err != nil {
			return Record{}, fmt.Errorf("operationstore: commit replay: %w", err)
		}
		return current, nil
	}
	if current.State.Terminal() {
		current.TenantID = strings.TrimSpace(tenant)
		if err := tx.Commit(ctx); err != nil {
			return Record{}, fmt.Errorf("operationstore: commit terminal cancellation: %w", err)
		}
		return current, nil
	}
	if current.State != StatePending && current.State != StateRunning && current.State != StateCancellationRequested {
		return Record{}, ErrNotCancellable
	}
	updatedAt := s.now().UTC()
	fence, err := currentFence(ctx, tx, tenantUUID, operationID)
	if err != nil {
		return Record{}, err
	}
	affected, err := tx.Exec(ctx, `
		UPDATE operation
		SET state = $5, updated_at = $6, fence_token = fence_token + 1
		WHERE tenant_id = $1 AND operation_id = $2 AND state = $3 AND fence_token = $4`,
		tenantUUID, operationID, string(current.State), fence,
		string(StateCancellationRequested), updatedAt)
	if err != nil {
		return Record{}, fmt.Errorf("operationstore: cancel operation: %w", err)
	}
	if affected != 1 {
		return Record{}, ErrFenced
	}
	updated, err := readRecord(ctx, tx, tenantUUID, operationID)
	if err != nil {
		return Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Record{}, fmt.Errorf("operationstore: commit cancellation: %w", err)
	}
	updated.TenantID = strings.TrimSpace(tenant)
	return updated, nil
}

// Transition advances an operation only when both its current state and fence
// token match. It is the worker-side CAS primitive paired with Cancel.
func (s *Store) Transition(ctx context.Context, tenant, operationID string, expectedFence uint64, from, to State) (Record, error) {
	if !validState(from) || !validState(to) {
		return Record{}, fmt.Errorf("%w: undeclared transition state", ErrInvalid)
	}
	tx, tenantUUID, err := s.beginTenant(ctx, tenant)
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback(ctx)
	affected, err := tx.Exec(ctx, `
		UPDATE operation
		SET state = $5, updated_at = $6, fence_token = fence_token + 1
		WHERE tenant_id = $1 AND operation_id = $2 AND state = $3 AND fence_token = $4`,
		tenantUUID, operationID, string(from), expectedFence, string(to), s.now().UTC())
	if err != nil {
		return Record{}, fmt.Errorf("operationstore: transition operation: %w", err)
	}
	if affected != 1 {
		return Record{}, ErrFenced
	}
	record, err := readRecord(ctx, tx, tenantUUID, operationID)
	if err != nil {
		return Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Record{}, fmt.Errorf("operationstore: commit transition: %w", err)
	}
	record.TenantID = strings.TrimSpace(tenant)
	return record, nil
}

// currentFence reads the operation's fence token inside the caller's
// transaction. A query failure is returned, never coerced to zero: a zero
// fence would make Cancel's compare-and-swap miss and misreport a backend
// failure as a concurrent modification (ErrFenced).
func currentFence(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, operationID string) (uint64, error) {
	var fence int64
	if err := tx.QueryRow(ctx, `SELECT fence_token FROM operation WHERE tenant_id = $1 AND operation_id = $2`, tenant, operationID).Scan(&fence); err != nil {
		return 0, fmt.Errorf("operationstore: read fence token: %w", err)
	}
	if fence < 0 {
		return 0, nil
	}
	return uint64(fence), nil
}

func readRecord(ctx context.Context, q dbport.Querier, tenant uuid.UUID, operationID string) (Record, error) {
	return readRecordQuery(ctx, q, `
		SELECT operation_id, owner_subject, request_type, state, result_bytes,
			error_bytes, metadata_ref, created_at, updated_at
		FROM operation WHERE tenant_id = $1 AND operation_id = $2`, tenant, operationID)
}

func readRecordForUpdate(ctx context.Context, q dbport.Querier, tenant uuid.UUID, operationID string) (Record, error) {
	return readRecordQuery(ctx, q, `
		SELECT operation_id, owner_subject, request_type, state, result_bytes,
			error_bytes, metadata_ref, created_at, updated_at
		FROM operation WHERE tenant_id = $1 AND operation_id = $2 FOR UPDATE`, tenant, operationID)
}

func readRecordQuery(ctx context.Context, q dbport.Querier, query string, tenant uuid.UUID, operationID string) (Record, error) {
	var record Record
	var state string
	var resultBytes, errorBytes []byte
	err := q.QueryRow(ctx, query, tenant, strings.TrimSpace(operationID)).Scan(
		&record.OperationID, &record.Owner, &record.RequestType, &state,
		&resultBytes, &errorBytes, &record.MetadataRef, &record.CreatedAt, &record.UpdatedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("operationstore: read operation: %w", err)
	}
	record.TenantID = tenant.String()
	record.State = State(state)
	if !validState(record.State) {
		return Record{}, fmt.Errorf("%w: stored state %q is not declared", ErrInvalid, state)
	}
	if len(resultBytes) > 0 {
		record.ResultBytes = append([]byte(nil), resultBytes...)
	}
	if len(errorBytes) > 0 {
		record.ErrorBytes = append([]byte(nil), errorBytes...)
	}
	return record, nil
}

func marshalPayloads(record Record) ([]byte, []byte, error) {
	return append([]byte(nil), record.ResultBytes...), append([]byte(nil), record.ErrorBytes...), nil
}

func validState(state State) bool {
	switch state {
	case StatePending, StateRunning, StateSucceeded, StateFailed, StateCancellationRequested, StateCancelled:
		return true
	default:
		return false
	}
}
