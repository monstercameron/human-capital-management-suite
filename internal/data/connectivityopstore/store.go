// Package connectivityopstore persists the tenant-scoped connector operation
// queue, transition journal, and reference-only credential binding evidence.
// It deliberately never accepts or writes credential bytes.
package connectivityopstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

var (
	ErrInvalid     = errors.New("connectivityopstore: invalid request")
	ErrNotFound    = errors.New("connectivityopstore: operation record not found")
	ErrNotReady    = errors.New("connectivityopstore: operation is not ready")
	ErrLeaseFenced = errors.New("connectivityopstore: lease is fenced")
)

// Store is a transaction-owning adapter for the three connector operation
// tables added by migrations 00235-00237.
type Store struct{ db dbport.Beginner }

// New constructs a store over a database handle. Each method opens and owns
// one transaction so tenant session scope cannot leak between calls.
func New(db dbport.Beginner) *Store { return &Store{db: db} }

// QueueItem identifies one durable queue membership row.
type QueueItem struct {
	QueueID     uuid.UUID
	OperationID uuid.UUID
	AvailableAt time.Time
}

// CredentialBinding is evidence about a reference-only machine lease. It has
// no field for a secret or a secret payload by design.
type CredentialBinding struct {
	BindingID          uuid.UUID
	OperationID        uuid.UUID
	AttemptID          uuid.UUID
	CredentialLeaseRef string
	WorkloadRef        string
	DestinationRef     string
	Purpose            string
	CustodyOperation   string
	LeaseExpiresAt     time.Time
	RevocationEpoch    uint64
	Outcome            string
	RefusalField       string
	BoundAt            time.Time
}

// Enqueue inserts tenant-scoped queue membership for an existing operation.
func (s *Store) Enqueue(ctx context.Context, tenant, operationID uuid.UUID, item QueueItem) error {
	if s == nil || s.db == nil || tenant == uuid.Nil || operationID == uuid.Nil {
		return ErrInvalid
	}
	if item.QueueID == uuid.Nil {
		item.QueueID = uuid.New()
	}
	if item.AvailableAt.IsZero() {
		item.AvailableAt = time.Now().UTC()
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin enqueue: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO connector_operation_queue
			(tenant_id, queue_id, operation_id, available_at)
		VALUES ($1, $2, $3, $4)`, tenant, item.QueueID, operationID, item.AvailableAt.UTC())
	if err != nil {
		return fmt.Errorf("enqueue operation: %w", err)
	}
	return tx.Commit(ctx)
}

// Claim atomically obtains a fenced queue lease. An expired lease is first
// requeued, while a live lease is refused without changing durable state.
func (s *Store) Claim(ctx context.Context, tenant, operationID uuid.UUID, worker string, at time.Time, duration time.Duration) (operation.Lease, error) {
	if s == nil || s.db == nil || tenant == uuid.Nil || operationID == uuid.Nil || strings.TrimSpace(worker) == "" || duration <= 0 {
		return operation.Lease{}, ErrInvalid
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	at = at.UTC()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return operation.Lease{}, fmt.Errorf("begin claim: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return operation.Lease{}, err
	}
	// Serialize admissions by connection, then derive the default one-slot
	// reservation and rolling-window usage from durable queue leases and
	// provider attempts. The in-memory ConnectorLedger still supplies the
	// fast local fairness checks; these rows make its restart boundary safe.
	var connectionID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT connection_id FROM connector_operation
		WHERE tenant_id = $1 AND operation_id = $2`, tenant, operationID).Scan(&connectionID)
	if errors.Is(err, dbport.ErrNoRows) {
		return operation.Lease{}, ErrNotFound
	}
	if err != nil {
		return operation.Lease{}, fmt.Errorf("read operation connection: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT connection_id FROM connector_connection
		WHERE tenant_id = $1 AND connection_id = $2 FOR UPDATE`, tenant, connectionID).Scan(&connectionID); err != nil {
		return operation.Lease{}, fmt.Errorf("lock connector connection: %w", err)
	}
	if blocked, err := durableAdmissionBlocked(ctx, tx, tenant, operationID, connectionID, at); err != nil {
		return operation.Lease{}, err
	} else if blocked {
		return operation.Lease{}, ErrNotReady
	}
	var state string
	var fence int64
	var expiresEpoch float64
	err = tx.QueryRow(ctx, `
		SELECT queue_state, fence_token,
		       COALESCE(EXTRACT(EPOCH FROM expires_at), 0)
		FROM connector_operation_queue
		WHERE tenant_id = $1 AND operation_id = $2
		FOR UPDATE`, tenant, operationID).Scan(&state, &fence, &expiresEpoch)
	if errors.Is(err, dbport.ErrNoRows) {
		return operation.Lease{}, ErrNotFound
	}
	if err != nil {
		return operation.Lease{}, fmt.Errorf("read queue lease: %w", err)
	}
	if state == "LEASED" && expiresEpoch > 0 {
		if float64(at.UnixNano())/1e9 < expiresEpoch {
			return operation.Lease{}, ErrLeaseFenced
		}
		if _, err := tx.Exec(ctx, `
			UPDATE connector_operation_queue
			SET queue_state = 'QUEUED', lease_token = NULL, worker_ref = '',
				leased_at = NULL, expires_at = NULL, updated_at = $3
			WHERE tenant_id = $1 AND operation_id = $2`, tenant, operationID, at); err != nil {
			return operation.Lease{}, fmt.Errorf("requeue expired lease: %w", err)
		}
	}
	token := uuid.New()
	leaseExpires := at.Add(duration)
	var grantedFence int64
	err = tx.QueryRow(ctx, `
		UPDATE connector_operation_queue
		SET queue_state = 'LEASED', worker_ref = $3, lease_token = $4,
			fence_token = fence_token + 1, leased_at = $5, expires_at = $6,
			updated_at = $5
		WHERE tenant_id = $1 AND operation_id = $2
		  AND queue_state IN ('QUEUED', 'REQUEUE')
		  AND available_at <= $5
		RETURNING fence_token`, tenant, operationID, worker, token, at, leaseExpires).Scan(&grantedFence)
	if errors.Is(err, dbport.ErrNoRows) {
		return operation.Lease{}, ErrNotReady
	}
	if err != nil {
		return operation.Lease{}, fmt.Errorf("claim queue lease: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return operation.Lease{}, fmt.Errorf("commit claim: %w", err)
	}
	return operation.Lease{TenantID: tenant.String(), OperationID: operationID, Token: token, FenceToken: uint64(grantedFence), WorkerID: worker, ExpiresAt: leaseExpires}, nil
}

func durableAdmissionBlocked(ctx context.Context, tx dbport.Tx, tenant, operationID, connectionID uuid.UUID, at time.Time) (bool, error) {
	quota := operation.UnknownConnectorQuota
	if quota.MaxConcurrent > 0 {
		var active bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1
			FROM connector_operation_queue q
			JOIN connector_operation o ON o.tenant_id = q.tenant_id AND o.operation_id = q.operation_id
			WHERE q.tenant_id = $1 AND o.connection_id = $2 AND q.operation_id <> $3
			  AND q.queue_state = 'LEASED' AND q.expires_at > $4
			  AND o.state IN ('QUEUED', 'LEASED', 'SENDING')
		)`, tenant, connectionID, operationID, at).Scan(&active)
		if err != nil {
			return false, fmt.Errorf("check durable connector reservations: %w", err)
		}
		if active {
			return true, nil
		}
	}
	if quota.Limit > 0 && quota.Window > 0 {
		var used bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1
			FROM connector_operation_attempt a
			JOIN connector_operation o ON o.tenant_id = a.tenant_id AND o.operation_id = a.operation_id
			WHERE a.tenant_id = $1 AND o.connection_id = $2 AND a.attempted_at > $3
		)`, tenant, connectionID, at.Add(-quota.Window)).Scan(&used)
		if err != nil {
			return false, fmt.Errorf("check durable connector rate window: %w", err)
		}
		if used {
			return true, nil
		}
	}
	return false, nil
}

// AppendJournal appends one immutable transition event. The database trigger
// rejects later UPDATE and DELETE attempts; this method only issues INSERT.
func (s *Store) AppendJournal(ctx context.Context, tenant uuid.UUID, event operation.JournalEvent) error {
	if s == nil || s.db == nil || tenant == uuid.Nil || event.OperationID == uuid.Nil || event.OperationSequence == 0 || strings.TrimSpace(event.Event) == "" || event.Digest == "" {
		return ErrInvalid
	}
	if event.TenantID != "" && event.TenantID != tenant.String() {
		return ErrInvalid
	}
	boundAt := event.OccurredAt.UTC().Truncate(time.Microsecond)
	if boundAt.IsZero() {
		return ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin journal append: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	// Serialize the tenant-wide digest-chain append across independent worker
	// handles. The advisory lock is transaction-scoped, so rollback releases it.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, tenant.String()); err != nil {
		return fmt.Errorf("lock operation journal chain: %w", err)
	}
	// Rebase against the durable head while holding the lock. In-memory handles
	// may have cached stale heads, so the database is authoritative on every append.
	var globalSequence, operationSequence int64
	if err := tx.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM connector_operation_journal WHERE tenant_id = $1),
			COALESCE((SELECT max(operation_sequence) FROM connector_operation_journal WHERE tenant_id = $1 AND operation_id = $2), 0)`, tenant, event.OperationID).Scan(&globalSequence, &operationSequence); err != nil {
		return fmt.Errorf("read operation journal sequence: %w", err)
	}
	requestDigest := digestHex(event.RequestDigest)
	responseDigest := digestHex(event.ResponseDigest)
	// The requested operation sequence is the idempotency key. Check it before
	// assigning tenant-chain coordinates so a replay remains a no-op even if
	// other operations have appended since the original commit.
	var existingKind, existingFrom, existingTo string
	var existingAttempt uuid.UUID
	var existingFence int64
	var existingCredential, existingRequest, existingResponse string
	var existingAt time.Time
	existingErr := tx.QueryRow(ctx, `
		SELECT event_kind, from_state, to_state,
			COALESCE(attempt_id, '00000000-0000-0000-0000-000000000000'), fence_token,
			COALESCE(credential_lease_ref, ''), COALESCE(request_digest, ''),
			COALESCE(response_digest, ''), occurred_at
		FROM connector_operation_journal
		WHERE tenant_id = $1 AND operation_id = $2 AND operation_sequence = $3`,
		tenant, event.OperationID, event.OperationSequence).Scan(&existingKind, &existingFrom,
		&existingTo, &existingAttempt, &existingFence, &existingCredential,
		&existingRequest, &existingResponse, &existingAt)
	if existingErr == nil {
		if existingKind != event.Event || existingFrom != string(event.From) || existingTo != string(event.To) ||
			existingAttempt != event.AttemptID || uint64(existingFence) != event.FenceToken ||
			existingCredential != event.CredentialLeaseID || existingRequest != requestDigest ||
			existingResponse != responseDigest || !existingAt.Equal(boundAt) {
			return fmt.Errorf("%w: conflicting operation journal sequence %d", ErrInvalid, event.OperationSequence)
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(existingErr, dbport.ErrNoRows) {
		return fmt.Errorf("read requested operation journal sequence: %w", existingErr)
	}
	if uint64(operationSequence+1) != event.OperationSequence {
		return fmt.Errorf("%w: operation journal sequence %d is not next after %d", ErrInvalid, event.OperationSequence, operationSequence)
	}
	var previousDigest string
	if globalSequence > 0 {
		// The tenant stream is hash linked; exactly one leaf is the current head.
		var leafCount int64
		if err := tx.QueryRow(ctx, `
			SELECT count(*), COALESCE(max(current.event_digest), '')
			FROM connector_operation_journal current
			WHERE current.tenant_id = $1
			  AND NOT EXISTS (
				SELECT 1 FROM connector_operation_journal later
				WHERE later.tenant_id = current.tenant_id
				  AND later.previous_digest = current.event_digest)
			`, tenant).Scan(&leafCount, &previousDigest); err != nil {
			return fmt.Errorf("read operation journal chain head: %w", err)
		}
		if leafCount != 1 {
			return fmt.Errorf("%w: durable journal has %d chain heads for tenant %s", ErrInvalid, leafCount, tenant)
		}
	}
	event.OccurredAt = boundAt
	event = operation.ChainJournalEvent(event, uint64(globalSequence+1), uint64(operationSequence+1), prefixDigest(previousDigest))
	storedPreviousDigest := digestHexOrZero(event.PreviousDigest)
	eventDigest := digestHex(event.Digest)
	inserted, err := tx.Exec(ctx, `
		INSERT INTO connector_operation_journal
			(tenant_id, journal_id, operation_id, operation_sequence, event_kind,
			 from_state, to_state, attempt_id, fence_token, credential_lease_ref,
			 request_digest, response_digest, previous_digest, event_digest, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8::uuid, '00000000-0000-0000-0000-000000000000'::uuid),
			$9, $10, NULLIF($11, ''), NULLIF($12, ''), $13, $14, $15)
		ON CONFLICT (tenant_id, operation_id, operation_sequence) DO NOTHING`,
		tenant, uuid.New(), event.OperationID, event.OperationSequence, event.Event,
		event.From, event.To, event.AttemptID, event.FenceToken, event.CredentialLeaseID,
		requestDigest, responseDigest, storedPreviousDigest, eventDigest, boundAt)
	if err != nil {
		return fmt.Errorf("append operation journal: %w", err)
	}
	if inserted == 0 {
		// The unique operation sequence is the retry key. Accept a retry only
		// when it is byte-for-byte the same transition; a conflicting event at
		// an occupied sequence is journal corruption and must fail closed.
		var existingDigest, existingKind, existingFrom, existingTo string
		var existingAttempt uuid.UUID
		var existingFence int64
		var existingCredential, existingRequest, existingResponse, existingPrevious string
		var existingAt time.Time
		err = tx.QueryRow(ctx, `
			SELECT event_digest, event_kind, from_state, to_state,
				COALESCE(attempt_id, '00000000-0000-0000-0000-000000000000'), fence_token,
				credential_lease_ref, COALESCE(request_digest, ''), COALESCE(response_digest, ''),
				previous_digest, occurred_at
			FROM connector_operation_journal
			WHERE tenant_id = $1 AND operation_id = $2 AND operation_sequence = $3`,
			tenant, event.OperationID, event.OperationSequence).Scan(&existingDigest, &existingKind,
			&existingFrom, &existingTo, &existingAttempt, &existingFence, &existingCredential,
			&existingRequest, &existingResponse, &existingPrevious, &existingAt)
		if err != nil {
			return fmt.Errorf("read duplicate operation journal event: %w", err)
		}
		if existingDigest != eventDigest || existingKind != event.Event || existingFrom != string(event.From) || existingTo != string(event.To) ||
			existingAttempt != event.AttemptID || uint64(existingFence) != event.FenceToken || existingCredential != event.CredentialLeaseID ||
			existingRequest != requestDigest || existingResponse != responseDigest || existingPrevious != previousDigest || !existingAt.Equal(boundAt) {
			return fmt.Errorf("%w: conflicting operation journal sequence %d", ErrInvalid, event.OperationSequence)
		}
	}
	return tx.Commit(ctx)
}

// Journal reads one tenant's journal stream, or all events for that tenant
// when operationID is uuid.Nil.
func (s *Store) Journal(ctx context.Context, tenant, operationID uuid.UUID) ([]operation.JournalEvent, error) {
	if s == nil || s.db == nil || tenant == uuid.Nil {
		return nil, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin journal read: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT journal_id, operation_id, operation_sequence, event_kind,
			from_state, to_state, COALESCE(attempt_id, '00000000-0000-0000-0000-000000000000'),
			fence_token, credential_lease_ref, COALESCE(request_digest, ''),
			COALESCE(response_digest, ''), previous_digest, event_digest, occurred_at
		FROM connector_operation_journal
		WHERE tenant_id = $1 AND ($2::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR operation_id = $2::uuid)
		ORDER BY operation_id, operation_sequence`, tenant, operationID)
	if err != nil {
		return nil, fmt.Errorf("read operation journal: %w", err)
	}
	defer rows.Close()
	var out []operation.JournalEvent
	var ignoredJournalID uuid.UUID
	for rows.Next() {
		var event operation.JournalEvent
		var operationSequence, fence int64
		var attemptID uuid.UUID
		var eventKind, from, to, credentialRef, requestDigest, responseDigest, previousDigest, digest string
		if err := rows.Scan(&ignoredJournalID, &event.OperationID, &operationSequence, &eventKind, &from, &to, &attemptID, &fence, &credentialRef, &requestDigest, &responseDigest, &previousDigest, &digest, &event.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan operation journal: %w", err)
		}
		event.Sequence = uint64(len(out) + 1)
		event.OperationSequence = uint64(operationSequence)
		event.TenantID = tenant.String()
		event.Event = eventKind
		event.From = operation.State(from)
		event.To = operation.State(to)
		if attemptID != uuid.Nil {
			event.AttemptID = attemptID
		}
		event.FenceToken = uint64(fence)
		event.CredentialLeaseID = credentialRef
		event.RequestDigest = prefixDigest(requestDigest)
		event.ResponseDigest = prefixDigest(responseDigest)
		event.PreviousDigest = prefixDigest(previousDigest)
		event.Digest = prefixDigest(digest)
		event.OccurredAt = event.OccurredAt.UTC()
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read operation journal: %w", err)
	}
	return out, nil
}

// BindCredentialLease appends reference-only lease binding evidence.
func (s *Store) BindCredentialLease(ctx context.Context, tenant uuid.UUID, binding CredentialBinding) error {
	if s == nil || s.db == nil || tenant == uuid.Nil || binding.OperationID == uuid.Nil || binding.BindingID == uuid.Nil || strings.TrimSpace(binding.CredentialLeaseRef) == "" || strings.TrimSpace(binding.WorkloadRef) == "" || strings.TrimSpace(binding.DestinationRef) == "" || strings.TrimSpace(binding.Purpose) == "" || strings.TrimSpace(binding.CustodyOperation) == "" || strings.TrimSpace(binding.Outcome) == "" || binding.BoundAt.IsZero() {
		return ErrInvalid
	}
	if binding.Outcome == "BOUND" && !binding.LeaseExpiresAt.After(binding.BoundAt) {
		return ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin credential binding: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO connector_operation_credential_lease
			(tenant_id, binding_id, operation_id, attempt_id, credential_lease_ref,
			 workload_ref, destination_ref, purpose, custody_operation, lease_expires_at,
			 revocation_epoch, outcome, refusal_field, bound_at)
		VALUES ($1, $2, $3, NULLIF($4::uuid, '00000000-0000-0000-0000-000000000000'::uuid), $5,
			$6, $7, $8, $9, $10, $11, $12, $13, $14)`, tenant, binding.BindingID, binding.OperationID, binding.AttemptID,
		binding.CredentialLeaseRef, binding.WorkloadRef, binding.DestinationRef, binding.Purpose, binding.CustodyOperation,
		binding.LeaseExpiresAt.UTC(), binding.RevocationEpoch, binding.Outcome, binding.RefusalField, binding.BoundAt.UTC())
	if err != nil {
		return fmt.Errorf("bind credential lease: %w", err)
	}
	return tx.Commit(ctx)
}

// CredentialBindings returns immutable reference-only binding evidence.
func (s *Store) CredentialBindings(ctx context.Context, tenant, operationID uuid.UUID) ([]CredentialBinding, error) {
	if s == nil || s.db == nil || tenant == uuid.Nil || operationID == uuid.Nil {
		return nil, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin credential read: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT binding_id, operation_id, COALESCE(attempt_id, '00000000-0000-0000-0000-000000000000'),
			credential_lease_ref, workload_ref, destination_ref, purpose, custody_operation,
			lease_expires_at, revocation_epoch, outcome, refusal_field, bound_at
		FROM connector_operation_credential_lease
		WHERE tenant_id = $1 AND operation_id = $2
		ORDER BY bound_at, binding_id`, tenant, operationID)
	if err != nil {
		return nil, fmt.Errorf("read credential bindings: %w", err)
	}
	defer rows.Close()
	var out []CredentialBinding
	for rows.Next() {
		var b CredentialBinding
		var epoch int64
		if err := rows.Scan(&b.BindingID, &b.OperationID, &b.AttemptID, &b.CredentialLeaseRef, &b.WorkloadRef, &b.DestinationRef, &b.Purpose, &b.CustodyOperation, &b.LeaseExpiresAt, &epoch, &b.Outcome, &b.RefusalField, &b.BoundAt); err != nil {
			return nil, fmt.Errorf("scan credential binding: %w", err)
		}
		if b.AttemptID == uuid.Nil {
			b.AttemptID = uuid.Nil
		}
		b.RevocationEpoch = uint64(epoch)
		b.LeaseExpiresAt = b.LeaseExpiresAt.UTC()
		b.BoundAt = b.BoundAt.UTC()
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read credential bindings: %w", err)
	}
	return out, nil
}

func digestHex(value string) string {
	value = strings.TrimPrefix(value, "sha256:")
	if value == "" {
		return ""
	}
	return value
}

func digestHexOrZero(value string) string {
	value = digestHex(value)
	if value == "" {
		return strings.Repeat("0", 64)
	}
	return value
}

func prefixDigest(value string) string {
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}
