// Package outbox owns the transactional outbox (owner: data plane; phase:
// P1A; DATA-007, DATA-008, part of the NEXT-004 slice).
//
// Enqueue writes one PENDING message inside the same transaction as the
// ledger append and projection update it accompanies (Commit, in commit.go),
// so an authoritative write and the intent to distribute it about that write
// commit or roll back together (DATA-007). Consumer then dispatches queued
// messages at least once, with a lease so a crash mid-batch never loses a
// message and a completed delivery never re-applies (DATA-008).
package outbox

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Status values for one outbox row.
const (
	StatusPending   = "PENDING"
	StatusInFlight  = "IN_FLIGHT"
	StatusDelivered = "DELIVERED"
	StatusFailed    = "FAILED"
	StatusAbandoned = "ABANDONED"
)

// Criticality is the durable priority attached to a delivery. It follows the
// platform-wide P0 (highest) through P4 (lowest) vocabulary without importing
// the operations admission package into the data plane.
const (
	CriticalityP0 = "P0"
	CriticalityP1 = "P1"
	CriticalityP2 = "P2"
	CriticalityP3 = "P3"
	CriticalityP4 = "P4"
)

var (
	// ErrIdentityConflict means an idempotency key was reused for different
	// immutable delivery content. Silently returning the first row would hide
	// a logical duplicate or a caller bug.
	ErrIdentityConflict   = errors.New("outbox: effect identity conflicts with existing message")
	ErrInvalidCriticality = errors.New("outbox: invalid criticality")
	ErrLeaseFence         = errors.New("outbox: lease fence refused")
)

// TraceLink is optional diagnostic context captured at an asynchronous
// boundary. It is never used as an idempotency, authorization, tenant, or
// business identity. Invalid links are discarded when an envelope is
// written so telemetry cannot change the business result.
type TraceLink struct {
	TraceID    string
	SpanID     string
	TraceFlags byte
	TraceState string
	ExpiresAt  time.Time
}

// CausalMetadata is the bounded, durable identity of an asynchronous
// envelope. LogicalOperationID remains stable across redelivery; AttemptID
// identifies the individual claim/attempt. TraceLink is optional.
type CausalMetadata struct {
	CorrelationID      string
	CausationID        string
	LogicalOperationID string
	AttemptID          string
	TraceLink          *TraceLink
}

// EnqueueRequest is one message to distribute after the authoritative commit.
type EnqueueRequest struct {
	Tenant uuid.UUID
	// OutboxID is the row's own identity. Callers that want a deterministic,
	// replay-stable ID (e.g. derived from the ledger event ID) may supply
	// one; the zero UUID means "generate one".
	OutboxID uuid.UUID
	// EffectIdentity is the idempotent identity of the external effect this
	// message carries. A duplicate Enqueue with the same EffectIdentity
	// under the same tenant is a no-op that returns the original row: one
	// record per idempotent effect (migrations/00006, outbox_effect_identity_unique).
	EffectIdentity string
	OrderingKey    string
	Criticality    string
	SchemaRef      string
	Payload        []byte
	Causal         *CausalMetadata
}

// Record is one outbox row.
type Record struct {
	Tenant         uuid.UUID
	OutboxID       uuid.UUID
	EffectIdentity string
	OrderingKey    string
	Criticality    string
	SchemaRef      string
	Payload        []byte
	Status         string
	Attempts       int
	AvailableAt    time.Time
	UpdatedAt      time.Time
	LeaseToken     uuid.UUID
	LeaseUntil     time.Time
	LeaseVersion   int64
	LastError      *string
	Causal         *CausalMetadata
}

// Enqueue inserts one PENDING message inside the caller's transaction. A
// duplicate EffectIdentity returns the row already recorded rather than a
// second logical message.
func Enqueue(ctx context.Context, tx dbport.Tx, req EnqueueRequest) (Record, error) {
	if req.Tenant == uuid.Nil {
		return Record{}, fmt.Errorf("outbox: enqueue requires a tenant")
	}
	if req.EffectIdentity == "" {
		return Record{}, fmt.Errorf("outbox: enqueue requires an effect identity")
	}
	if req.OrderingKey == "" {
		return Record{}, fmt.Errorf("outbox: enqueue requires an ordering key")
	}
	criticality := req.Criticality
	if criticality == "" {
		criticality = CriticalityP2
	}
	if !validCriticality(criticality) {
		return Record{}, fmt.Errorf("%w: %q", ErrInvalidCriticality, criticality)
	}
	if req.SchemaRef == "" {
		return Record{}, fmt.Errorf("outbox: enqueue requires a schema reference")
	}
	causal, err := normalizeCausal(req.Causal)
	if err != nil {
		return Record{}, err
	}
	id := req.OutboxID
	if id == uuid.Nil {
		id = uuid.New()
	}

	args := []any{req.Tenant, id, req.EffectIdentity, req.OrderingKey, criticality, req.SchemaRef, req.Payload}
	args = append(args, causalValues(causal)...)
	affected, err := tx.Exec(ctx, `
		INSERT INTO outbox (tenant_id, outbox_id, effect_identity, ordering_key, criticality, schema_ref, payload,
			correlation_id, causation_id, logical_operation_id, attempt_id,
			trace_id, trace_span_id, trace_flags, trace_state, trace_link_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (tenant_id, effect_identity) DO NOTHING`,
		args...)
	if err != nil {
		// An explicit OutboxID that collides with another row's primary
		// key is the same caller bug as a conflicting effect identity: a
		// reused identity for different content. Report it typed so
		// callers can distinguish "replay" from "bug" without parsing
		// driver errors.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "outbox_pkey" {
			return Record{}, fmt.Errorf("%w: %s", ErrIdentityConflict, req.EffectIdentity)
		}
		return Record{}, fmt.Errorf("outbox: enqueue %s: %w", req.EffectIdentity, err)
	}
	if affected == 1 {
		return Read(ctx, tx, req.Tenant, id)
	}

	// Already enqueued under this effect identity: return the existing row.
	existing, err := readByEffectIdentity(ctx, tx, req.Tenant, req.EffectIdentity)
	if err != nil {
		return Record{}, err
	}
	if !sameImmutableMessage(existing, req, criticality) {
		return Record{}, fmt.Errorf("%w: %s", ErrIdentityConflict, req.EffectIdentity)
	}
	return existing, nil
}

const selectRecordColumns = `tenant_id, outbox_id, effect_identity, ordering_key, criticality, schema_ref, payload, status, attempts, available_at, updated_at, lease_token, lease_until, lease_version, last_error,
	correlation_id, causation_id, logical_operation_id, attempt_id, trace_id, trace_span_id, trace_flags, trace_state, trace_link_expires_at`

func scanRecord(row interface{ Scan(dest ...any) error }) (Record, error) {
	var (
		rec                                                       Record
		leaseToken                                                *uuid.UUID
		leaseUntil                                                *time.Time
		correlationID, causationID, logicalOperationID, attemptID *string
		traceID, traceSpanID, traceState                          *string
		traceFlags                                                *int16
		traceExpiresAt                                            *time.Time
	)
	if err := row.Scan(
		&rec.Tenant, &rec.OutboxID, &rec.EffectIdentity, &rec.OrderingKey, &rec.Criticality,
		&rec.SchemaRef, &rec.Payload, &rec.Status, &rec.Attempts, &rec.AvailableAt,
		&rec.UpdatedAt, &leaseToken, &leaseUntil, &rec.LeaseVersion, &rec.LastError,
		&correlationID, &causationID, &logicalOperationID, &attemptID,
		&traceID, &traceSpanID, &traceFlags, &traceState, &traceExpiresAt,
	); err != nil {
		return Record{}, err
	}
	if leaseToken != nil {
		rec.LeaseToken = *leaseToken
	}
	if leaseUntil != nil {
		rec.LeaseUntil = leaseUntil.UTC()
	}
	if correlationID != nil {
		rec.Causal = &CausalMetadata{CorrelationID: *correlationID, CausationID: valueOrEmpty(causationID), LogicalOperationID: valueOrEmpty(logicalOperationID), AttemptID: valueOrEmpty(attemptID)}
		if traceID != nil && traceSpanID != nil {
			link := &TraceLink{TraceID: *traceID, SpanID: *traceSpanID, TraceState: valueOrEmpty(traceState), ExpiresAt: timeOrZero(traceExpiresAt)}
			if traceFlags != nil && *traceFlags >= 0 && *traceFlags <= 255 {
				link.TraceFlags = byte(*traceFlags)
			}
			if validTraceLink(link) {
				rec.Causal.TraceLink = link
			}
		}
	}
	return rec, nil
}

func valueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func timeOrZero(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return v.UTC()
}

func normalizeCausal(c *CausalMetadata) (*CausalMetadata, error) {
	if c == nil {
		return nil, nil
	}
	for name, value := range map[string]string{"correlation_id": c.CorrelationID, "causation_id": c.CausationID, "logical_operation_id": c.LogicalOperationID, "attempt_id": c.AttemptID} {
		if strings.TrimSpace(value) == "" || len(value) > 128 {
			return nil, fmt.Errorf("outbox: invalid causal metadata %s", name)
		}
	}
	out := *c
	if c.TraceLink != nil && validTraceLink(c.TraceLink) {
		link := *c.TraceLink
		out.TraceLink = &link
	} else {
		out.TraceLink = nil
	}
	return &out, nil
}

func validTraceLink(link *TraceLink) bool {
	if link == nil || !validHexID(link.TraceID, 16) || !validHexID(link.SpanID, 8) || !validTraceState(link.TraceState) {
		return false
	}
	return true
}

func validHexID(value string, size int) bool {
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

func validTraceState(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 256 {
		return false
	}
	seen := make(map[string]struct{})
	entries := strings.Split(value, ",")
	if len(entries) > 32 {
		return false
	}
	for _, entry := range entries {
		key, member, ok := strings.Cut(entry, "=")
		if !ok || !validTraceStateKey(key) || !validTraceStateValue(member) {
			return false
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func validTraceStateKey(key string) bool {
	if key == "" || len(key) > 256 {
		return false
	}
	parts := strings.Split(key, "@")
	if len(parts) > 2 || len(parts) == 2 && (parts[0] == "" || len(parts[0]) > 241) {
		return false
	}
	for partIndex, part := range parts {
		if part == "" {
			return false
		}
		lower := part[0] >= 'a' && part[0] <= 'z'
		digitTenant := len(parts) == 2 && partIndex == 0 && part[0] >= '0' && part[0] <= '9'
		if !lower && !digitTenant {
			return false
		}
		for _, r := range part[1:] {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || strings.ContainsRune("_-*/", r)) {
				return false
			}
		}
	}
	return true
}

func validTraceStateValue(value string) bool {
	if value == "" || value[0] == ' ' || value[len(value)-1] == ' ' {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r > 0x7e || r == ',' || r == '=' {
			return false
		}
	}
	return true
}

func causalValues(c *CausalMetadata) []any {
	if c == nil {
		return []any{nil, nil, nil, nil, nil, nil, nil, nil, nil}
	}
	var traceID, spanID, traceState any
	var flags any
	var expires any
	if c.TraceLink != nil {
		traceID, spanID, traceState, flags, expires = c.TraceLink.TraceID, c.TraceLink.SpanID, c.TraceLink.TraceState, int16(c.TraceLink.TraceFlags), c.TraceLink.ExpiresAt
	}
	return []any{c.CorrelationID, c.CausationID, c.LogicalOperationID, c.AttemptID, traceID, spanID, flags, traceState, expires}
}

func validCriticality(value string) bool {
	return value == CriticalityP0 || value == CriticalityP1 || value == CriticalityP2 || value == CriticalityP3 || value == CriticalityP4
}

func sameImmutableMessage(existing Record, req EnqueueRequest, criticality string) bool {
	return existing.EffectIdentity == req.EffectIdentity &&
		existing.OrderingKey == req.OrderingKey &&
		existing.Criticality == criticality &&
		existing.SchemaRef == req.SchemaRef &&
		bytes.Equal(existing.Payload, req.Payload) && sameCausal(existing.Causal, req.Causal)
}

func sameCausal(a, b *CausalMetadata) bool {
	na, errA := normalizeCausal(a)
	nb, errB := normalizeCausal(b)
	if errA != nil || errB != nil {
		return false
	}
	if na == nil || nb == nil {
		return na == nil && nb == nil
	}
	// AttemptID is claim metadata: it intentionally changes on every
	// redelivery. The logical envelope identity remains immutable.
	return na.CorrelationID == nb.CorrelationID && na.CausationID == nb.CausationID && na.LogicalOperationID == nb.LogicalOperationID
}

// Querier is the minimal database capability Read needs.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) dbport.Row
}

// Read returns one outbox row by its own ID.
func Read(ctx context.Context, q Querier, tenant uuid.UUID, outboxID uuid.UUID) (Record, error) {
	row := q.QueryRow(ctx, `SELECT `+selectRecordColumns+` FROM outbox WHERE tenant_id = $1 AND outbox_id = $2`, tenant, outboxID)
	rec, err := scanRecord(row)
	if errors.Is(err, dbport.ErrNoRows) {
		return Record{}, fmt.Errorf("outbox: %s not found for tenant %s", outboxID, tenant)
	}
	if err != nil {
		return Record{}, fmt.Errorf("outbox: read %s: %w", outboxID, err)
	}
	return rec, nil
}

// ReadByEffectIdentity returns the outbox row one idempotent effect enqueued,
// and whether one exists. An effect that enqueued nothing (or belongs to
// another tenant) is found=false with no error, because the workflow
// execution inspector (WF-RUN-019) asks for every effect reference a node
// recorded and most of them never touch the outbox.
func ReadByEffectIdentity(ctx context.Context, q Querier, tenant uuid.UUID, effectIdentity string) (Record, bool, error) {
	rec, err := readByEffectIdentity(ctx, q, tenant, effectIdentity)
	if errors.Is(err, dbport.ErrNoRows) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, err
	}
	return rec, true, nil
}

func readByEffectIdentity(ctx context.Context, q Querier, tenant uuid.UUID, effectIdentity string) (Record, error) {
	row := q.QueryRow(ctx, `SELECT `+selectRecordColumns+` FROM outbox WHERE tenant_id = $1 AND effect_identity = $2`, tenant, effectIdentity)
	rec, err := scanRecord(row)
	if err != nil {
		return Record{}, fmt.Errorf("outbox: read effect %s: %w", effectIdentity, err)
	}
	return rec, nil
}
