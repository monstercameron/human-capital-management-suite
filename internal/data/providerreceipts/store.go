// Package providerreceipts persists signed provider RESULT callbacks
// ("receipts") in the append-only integration_provider_receipt table
// (migration 00313).
//
// A receipt is keyed by (tenant, provider, event id). Recording the same event
// again with the same payload digest is an idempotent no-op reported as a
// duplicate; recording it with a different digest is a conflicting
// restatement and is refused with [ErrConflict]. Every call runs inside a
// caller-owned transaction that must already be scoped to the receipt's
// tenant (tenancy.WithTenant) for row level security to admit it.
package providerreceipts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Provider, outcome and origin vocabularies, mirroring the table's CHECKs.
const (
	ProviderPayroll = "payroll"
	ProviderIAM     = "iam"

	OutcomeApplied  = "APPLIED"
	OutcomeGranted  = "GRANTED"
	OutcomeRejected = "REJECTED"
	// OutcomeReversed (payroll) and OutcomeRevoked (IAM) record an earlier
	// APPLIED or GRANTED being undone. They are a separate kind of answer:
	// see [Store.LatestForChangeByKind].
	OutcomeReversed = "REVERSED"
	OutcomeRevoked  = "REVOKED"

	OriginWebhook           = "webhook"
	OriginDeliveryRejection = "delivery_rejection"
	// OriginStatusPoll is the platform's record of a state it read from the
	// provider's status endpoint rather than received in a callback.
	OriginStatusPoll = "status_poll"
)

// MaxSecretIndex is the largest SecretIndex the smallint column holds.
const MaxSecretIndex = 32767

// Column bounds, mirroring the table's CHECKs.
const (
	MaxEventIDLen   = 200
	MaxChangeRefLen = 200
	MaxReasonLen    = 2000
)

var (
	// ErrConflict reports an event id already recorded with a different
	// payload digest: the provider restated one event with other bytes.
	ErrConflict = errors.New("providerreceipts: event already recorded with a different payload")
	// ErrInvalid reports a receipt that fails validation before any SQL runs.
	ErrInvalid = errors.New("providerreceipts: invalid receipt")
)

// Receipt is one recorded provider result.
type Receipt struct {
	TenantID       uuid.UUID
	Provider       string
	EventID        string
	EventType      string
	ChangeRef      string
	CorrelationKey string
	Outcome        string
	ProviderRef    string
	Reason         string
	Origin         string
	// Payload is the decoded callback body as JSON. PostgreSQL stores it as
	// jsonb, so the bytes read back are normalized; PayloadDigest (of the
	// exact signed bytes) is the identity that decides duplicates.
	Payload       []byte
	PayloadDigest string
	SignalID      *uuid.UUID
	ReceivedAt    time.Time
	// SecretIndex is which of the endpoint's signing secrets verified a
	// webhook receipt (0 = current, higher = a previous one still honoured
	// during rotation). It is nil for other origins, which carry no
	// signature, and may be nil for a webhook receipt recorded without it.
	SecretIndex *int
}

// Store reads and writes receipts. It holds no state.
type Store struct{}

// Validate reports the first field that would violate the table's
// constraints, wrapped in [ErrInvalid].
func (r Receipt) Validate() error {
	bad := func(field, why string) error { return fmt.Errorf("%w: %s %s", ErrInvalid, field, why) }
	switch {
	case r.TenantID == uuid.Nil:
		return bad("tenant_id", "is required")
	case r.Provider != ProviderPayroll && r.Provider != ProviderIAM:
		return bad("provider", "must be payroll or iam")
	case strings.TrimSpace(r.EventID) == "":
		return bad("event_id", "is required")
	case utf8.RuneCountInString(r.EventID) > MaxEventIDLen:
		return bad("event_id", "is too long")
	case strings.TrimSpace(r.EventType) == "":
		return bad("event_type", "is required")
	case strings.TrimSpace(r.ChangeRef) == "":
		return bad("change_ref", "is required")
	case utf8.RuneCountInString(r.ChangeRef) > MaxChangeRefLen:
		return bad("change_ref", "is too long")
	case strings.TrimSpace(r.CorrelationKey) == "":
		return bad("correlation_key", "is required")
	case !knownOutcome(r.Outcome):
		return bad("outcome", "must be APPLIED, GRANTED, REJECTED, REVERSED or REVOKED")
	case utf8.RuneCountInString(r.Reason) > MaxReasonLen:
		return bad("reason", "is too long")
	case r.Origin != OriginWebhook && r.Origin != OriginDeliveryRejection && r.Origin != OriginStatusPoll:
		return bad("origin", "must be webhook, delivery_rejection or status_poll")
	case r.SecretIndex != nil && r.Origin != OriginWebhook:
		return bad("secret_index", "is only meaningful for a webhook receipt")
	case r.SecretIndex != nil && (*r.SecretIndex < 0 || *r.SecretIndex > MaxSecretIndex):
		return bad("secret_index", "is out of range")
	case len(r.Payload) == 0:
		return bad("payload", "is required")
	case strings.TrimSpace(r.PayloadDigest) == "":
		return bad("payload_digest", "is required")
	case r.ReceivedAt.IsZero():
		return bad("received_at", "is required")
	}
	return nil
}

func knownOutcome(o string) bool {
	switch o {
	case OutcomeApplied, OutcomeGranted, OutcomeRejected, OutcomeReversed, OutcomeRevoked:
		return true
	}
	return false
}

const insertSQL = `
INSERT INTO integration_provider_receipt
    (tenant_id, provider, event_id, event_type, change_ref, correlation_key, outcome,
     provider_ref, reason, origin, payload, payload_digest, signal_id, received_at, secret_index)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13, $14, $15)
ON CONFLICT (tenant_id, provider, event_id) DO NOTHING`

const selectColumns = `tenant_id, provider, event_id, event_type, change_ref, correlation_key, outcome,
    provider_ref, reason, origin, payload::text, payload_digest, signal_id, received_at, secret_index`

// Record inserts r. When the (tenant, provider, event id) row already exists
// it reports duplicate=true if the stored digest equals r.PayloadDigest and
// [ErrConflict] otherwise; the stored row is never changed.
func (Store) Record(ctx context.Context, tx dbport.Tx, r Receipt) (duplicate bool, err error) {
	if err := r.Validate(); err != nil {
		return false, err
	}
	var signal uuid.NullUUID
	if r.SignalID != nil {
		signal = uuid.NullUUID{UUID: *r.SignalID, Valid: true}
	}
	var secretIndex *int16
	if r.SecretIndex != nil {
		v := int16(*r.SecretIndex)
		secretIndex = &v
	}
	n, err := tx.Exec(ctx, insertSQL, r.TenantID, r.Provider, r.EventID, r.EventType, r.ChangeRef, r.CorrelationKey,
		r.Outcome, r.ProviderRef, r.Reason, r.Origin, string(r.Payload), r.PayloadDigest, signal, r.ReceivedAt.UTC(), secretIndex)
	if err != nil {
		return false, fmt.Errorf("providerreceipts: insert %s/%s: %w", r.Provider, r.EventID, err)
	}
	if n == 1 {
		return false, nil
	}
	var stored string
	err = tx.QueryRow(ctx, `SELECT payload_digest FROM integration_provider_receipt WHERE tenant_id = $1 AND provider = $2 AND event_id = $3`,
		r.TenantID, r.Provider, r.EventID).Scan(&stored)
	if err != nil {
		return false, fmt.Errorf("providerreceipts: read existing %s/%s: %w", r.Provider, r.EventID, err)
	}
	if stored != r.PayloadDigest {
		return false, fmt.Errorf("%w: %s/%s", ErrConflict, r.Provider, r.EventID)
	}
	return true, nil
}

// LatestForChange returns the most recently received receipt answering
// changeRef, across providers and outcomes; ties on received_at break by
// event id so the answer is deterministic. found is false when none exists.
func (Store) LatestForChange(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, changeRef string) (Receipt, bool, error) {
	if tenantID == uuid.Nil || strings.TrimSpace(changeRef) == "" {
		return Receipt{}, false, fmt.Errorf("%w: tenant_id and change_ref are required", ErrInvalid)
	}
	return scanLatest(tx.QueryRow(ctx, `SELECT `+selectColumns+` FROM integration_provider_receipt
WHERE tenant_id = $1 AND change_ref = $2
ORDER BY received_at DESC, event_id DESC
LIMIT 1`, tenantID, changeRef), changeRef)
}

// LatestForChangeByKind is LatestForChange restricted to the given
// outcomes, so a caller asks one kind of question at a time: "how did the
// apply settle" (APPLIED, GRANTED, REJECTED) separately from "was it undone"
// (REVERSED, REVOKED). A REVERSED received after an APPLIED therefore never
// hides the APPLIED from an observer asking about the apply. At least one
// outcome is required and each must be known.
func (Store) LatestForChangeByKind(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, changeRef string, outcomes ...string) (Receipt, bool, error) {
	if tenantID == uuid.Nil || strings.TrimSpace(changeRef) == "" {
		return Receipt{}, false, fmt.Errorf("%w: tenant_id and change_ref are required", ErrInvalid)
	}
	if len(outcomes) == 0 {
		return Receipt{}, false, fmt.Errorf("%w: at least one outcome is required", ErrInvalid)
	}
	for _, o := range outcomes {
		if !knownOutcome(o) {
			return Receipt{}, false, fmt.Errorf("%w: outcome %q is not known", ErrInvalid, o)
		}
	}
	return scanLatest(tx.QueryRow(ctx, `SELECT `+selectColumns+` FROM integration_provider_receipt
WHERE tenant_id = $1 AND change_ref = $2 AND outcome = ANY($3::text[])
ORDER BY received_at DESC, event_id DESC
LIMIT 1`, tenantID, changeRef, outcomes), changeRef)
}

// scanLatest reads one selectColumns row; found is false on no rows.
func scanLatest(row dbport.Row, changeRef string) (Receipt, bool, error) {
	var (
		r           Receipt
		payload     string
		signal      uuid.NullUUID
		secretIndex *int16
	)
	err := row.Scan(&r.TenantID, &r.Provider, &r.EventID, &r.EventType, &r.ChangeRef, &r.CorrelationKey, &r.Outcome,
		&r.ProviderRef, &r.Reason, &r.Origin, &payload, &r.PayloadDigest, &signal, &r.ReceivedAt, &secretIndex)
	if errors.Is(err, dbport.ErrNoRows) {
		return Receipt{}, false, nil
	}
	if err != nil {
		return Receipt{}, false, fmt.Errorf("providerreceipts: latest for %s: %w", changeRef, err)
	}
	r.Payload = []byte(payload)
	if signal.Valid {
		id := signal.UUID
		r.SignalID = &id
	}
	if secretIndex != nil {
		v := int(*secretIndex)
		r.SecretIndex = &v
	}
	r.ReceivedAt = r.ReceivedAt.UTC()
	return r, true, nil
}
