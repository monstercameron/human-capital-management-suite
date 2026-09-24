// Package correction owns an append-only business correction transaction. It
// preserves the original ledger event, appends a CORRECTION successor, and
// records projection/outbox work in the same caller-owned transaction.
package correction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/lineage"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
)

// ReconciliationObligation is a durable downstream action caused by a
// correction. The outbox identity makes retries idempotent.
type ReconciliationObligation struct {
	EffectIdentity string
	OrderingKey    string
	SchemaRef      string
	Payload        []byte
}

// Request describes a governed correction. EffectiveAt is deliberately
// caller-supplied: a correction can preserve the original business chronology
// while its RecordedAt is later.
type Request struct {
	Tenant       uuid.UUID
	StreamKey    string
	ExpectedHead int64
	Target       ledger.EventRef

	Authority   string
	SourceRef   string
	SchemaRef   string
	Payload     []byte
	ArtifactRef string

	OccurredAt     time.Time
	EffectiveAt    time.Time
	CorrelationID  uuid.UUID
	IdempotencyKey string

	Reason       string
	CorrectedBy  string
	CorrectionID uuid.UUID

	ProjectionName string
	Obligations    []ReconciliationObligation

	// These fields bind a correction of a committed transaction to the
	// immutable transaction_correction evidence row. They are optional for a
	// general business-fact correction, but must be supplied together.
	CorrectsReceiptID  uuid.UUID
	CorrectingIntentID uuid.UUID
	IntentDigest       string
	ProposalDigest     string
	ControlDigest      string
}

// Result contains the immutable successor and all durable work attached to
// it. Target is returned so callers can explain the chronology without a
// second read.
type Result struct {
	Target        ledger.EventRecord
	Correction    ledger.AppendReceipt
	Effective     lineage.Node
	EffectivePath []lineage.Node
	Projection    projection.ApplyResult
	Obligations   []outbox.Record
	CorrectionID  uuid.UUID
	Replayed      bool
}

var (
	ErrInvalidRequest = errors.New("transaction correction: invalid request")
	ErrTargetMismatch = errors.New("transaction correction: target is outside the requested stream")
)

// Append records one business correction inside tx. The caller owns the final
// commit or rollback; a failed projection or obligation rolls back the
// successor event as well.
func Append(ctx context.Context, tx dbport.Tx, req Request, now func() time.Time) (Result, error) {
	if tx == nil {
		return Result{}, fmt.Errorf("%w: transaction is required", ErrInvalidRequest)
	}
	if err := validate(req); err != nil {
		return Result{}, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	target, err := ledger.NewReader().ReadEvent(ctx, tx, req.Tenant, req.Target.StreamKey, req.Target.Sequence)
	if err != nil {
		return Result{}, fmt.Errorf("transaction correction: read target: %w", err)
	}
	if target.StreamKey != req.StreamKey || target.Tenant != req.Tenant {
		return Result{}, ErrTargetMismatch
	}

	correctionID := req.CorrectionID
	if correctionID == uuid.Nil {
		correctionID = uuid.NewSHA1(correctionNamespace, []byte(fmt.Sprintf("%s/%s/%d/%s", req.Tenant, req.StreamKey, req.Target.Sequence, req.IdempotencyKey)))
	}
	appender := ledger.New(ledger.WithClock(func() time.Time { return now().UTC() }))
	receipt, err := lineage.Append(ctx, tx, req.Tenant, ledger.AppendRequest{
		Tenant: req.Tenant, StreamKey: req.StreamKey, ExpectedHead: req.ExpectedHead,
		AssertionClass: ledger.Correction, Authority: req.Authority, SourceRef: req.SourceRef,
		SchemaRef: req.SchemaRef, Payload: req.Payload, ArtifactRef: req.ArtifactRef,
		OccurredAt: req.OccurredAt, EffectiveAt: req.EffectiveAt,
		CorrelationID: req.CorrelationID, CausationID: target.EventID,
		IdempotencyKey: req.IdempotencyKey, Corrects: &req.Target,
	}, appender)
	if err != nil {
		return Result{}, fmt.Errorf("transaction correction: append successor: %w", err)
	}
	effective, effectivePath, err := lineage.EffectiveCurrent(ctx, tx, req.Tenant, req.Target)
	if err != nil {
		return Result{}, fmt.Errorf("transaction correction: resolve effective successor: %w", err)
	}

	var applied projection.ApplyResult
	if req.ProjectionName != "" {
		applied, err = projection.Apply(ctx, tx, projection.ApplyRequest{
			Tenant: req.Tenant, ProjectionName: req.ProjectionName,
			StreamKey: req.StreamKey, Sequence: receipt.Sequence, Digest: receipt.Digest,
		})
		if err != nil {
			return Result{}, fmt.Errorf("transaction correction: advance projection: %w", err)
		}
	}

	if hasTransactionCorrection(req) {
		if err := recordTransactionCorrection(ctx, tx, req, correctionID, now()); err != nil {
			return Result{}, err
		}
	}
	obligations, err := enqueueObligations(ctx, tx, req.Tenant, receipt, req.Obligations)
	if err != nil {
		return Result{}, err
	}
	return Result{Target: target, Correction: receipt, Effective: effective, EffectivePath: effectivePath, Projection: applied,
		Obligations: obligations, CorrectionID: correctionID, Replayed: receipt.Replayed}, nil
}

func validate(req Request) error {
	switch {
	case req.Tenant == uuid.Nil:
		return fmt.Errorf("%w: tenant is required", ErrInvalidRequest)
	case req.StreamKey == "":
		return fmt.Errorf("%w: stream key is required", ErrInvalidRequest)
	case req.Target.StreamKey == "" || req.Target.Sequence < 1:
		return fmt.Errorf("%w: correction target must name a positive stream sequence", ErrInvalidRequest)
	case req.Target.StreamKey != req.StreamKey:
		return ErrTargetMismatch
	case req.ExpectedHead < 0:
		return fmt.Errorf("%w: expected head cannot be negative", ErrInvalidRequest)
	case strings.TrimSpace(req.Authority) == "":
		return fmt.Errorf("%w: authority is required", ErrInvalidRequest)
	case strings.TrimSpace(req.SourceRef) == "":
		return fmt.Errorf("%w: source reference is required", ErrInvalidRequest)
	case strings.TrimSpace(req.SchemaRef) == "":
		return fmt.Errorf("%w: schema reference is required", ErrInvalidRequest)
	case req.OccurredAt.IsZero():
		return fmt.Errorf("%w: occurred time is required", ErrInvalidRequest)
	case req.EffectiveAt.IsZero():
		return fmt.Errorf("%w: effective time is required", ErrInvalidRequest)
	case req.CorrelationID == uuid.Nil:
		return fmt.Errorf("%w: correlation id is required", ErrInvalidRequest)
	case strings.TrimSpace(req.IdempotencyKey) == "":
		return fmt.Errorf("%w: idempotency key is required", ErrInvalidRequest)
	case strings.TrimSpace(req.Reason) == "":
		return fmt.Errorf("%w: correction reason is required", ErrInvalidRequest)
	case strings.TrimSpace(req.CorrectedBy) == "":
		return fmt.Errorf("%w: corrected by is required", ErrInvalidRequest)
	case req.Payload != nil && req.ArtifactRef != "":
		return fmt.Errorf("%w: payload and artifact reference are mutually exclusive", ErrInvalidRequest)
	case req.Payload == nil && req.ArtifactRef == "":
		return fmt.Errorf("%w: payload or artifact reference is required", ErrInvalidRequest)
	}
	transactionFields := []bool{req.CorrectsReceiptID != uuid.Nil, req.CorrectingIntentID != uuid.Nil,
		req.IntentDigest != "", req.ProposalDigest != "", req.ControlDigest != ""}
	if any(transactionFields) && !all(transactionFields) {
		return fmt.Errorf("%w: transaction correction metadata must be complete", ErrInvalidRequest)
	}
	for name, digest := range map[string]string{"intent": req.IntentDigest, "proposal": req.ProposalDigest, "control": req.ControlDigest} {
		if digest != "" && !isDigest(digest) {
			return fmt.Errorf("%w: %s digest must be 64 lowercase hex characters", ErrInvalidRequest, name)
		}
	}
	for i, obligation := range req.Obligations {
		if obligation.EffectIdentity == "" || obligation.OrderingKey == "" || obligation.SchemaRef == "" {
			return fmt.Errorf("%w: obligation %d is incomplete", ErrInvalidRequest, i)
		}
	}
	return nil
}

func hasTransactionCorrection(req Request) bool {
	return req.CorrectsReceiptID != uuid.Nil
}

func recordTransactionCorrection(ctx context.Context, tx dbport.Tx, req Request, correctionID uuid.UUID, at time.Time) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO transaction_correction (
			tenant_id, correction_id, corrects_receipt_id, correcting_intent_id,
			intent_digest, proposal_digest, control_digest,
			correction_reason, corrected_by, corrected_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (tenant_id, correction_id) DO NOTHING`,
		req.Tenant, correctionID, req.CorrectsReceiptID, req.CorrectingIntentID,
		req.IntentDigest, req.ProposalDigest, req.ControlDigest, req.Reason, req.CorrectedBy, at.UTC()); err != nil {
		return fmt.Errorf("transaction correction: record transaction correction: %w", err)
	}
	return nil
}

func enqueueObligations(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, receipt ledger.AppendReceipt, obligations []ReconciliationObligation) ([]outbox.Record, error) {
	if len(obligations) == 0 {
		return nil, nil
	}
	var records []outbox.Record
	for _, obligation := range obligations {
		if _, err := tx.Exec(ctx, `
			INSERT INTO payload_schema (
				tenant_id, schema_ref, schema_id, schema_version,
				message_full_name, wire_format, canonicalization_profile)
			VALUES ($1, $2, $2, 1, $2, 'PROTOBUF', 'LEDGER_EVENT')
			ON CONFLICT (tenant_id, schema_ref) DO NOTHING`, tenant, obligation.SchemaRef); err != nil {
			return nil, fmt.Errorf("transaction correction: register obligation schema: %w", err)
		}
		effect := obligation.EffectIdentity
		if effect == "" {
			effect = fmt.Sprintf("correction:%s:%s", receipt.EventID, obligation.OrderingKey)
		}
		record, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
			Tenant:         tenant,
			OutboxID:       uuid.NewSHA1(correctionNamespace, []byte(effect)),
			EffectIdentity: effect, OrderingKey: obligation.OrderingKey,
			SchemaRef: obligation.SchemaRef, Payload: obligation.Payload,
		})
		if err != nil {
			return nil, fmt.Errorf("transaction correction: enqueue obligation %s: %w", effect, err)
		}
		records = append(records, record)
	}
	return records, nil
}

func any(values []bool) bool {
	for _, value := range values {
		if value {
			return true
		}
	}
	return false
}

func all(values []bool) bool {
	for _, value := range values {
		if !value {
			return false
		}
	}
	return true
}

func isDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}

var correctionNamespace = uuid.MustParse("4dd8dc62-c17c-4e06-8b05-01c48a3e7f50")

// Version is the package contract version used by architecture tooling.
func Version() int { return 1 }

// Explain returns a bounded description suitable for package telemetry.
func Explain() string {
	return "transaction.correction v1: append-only business correction with projection and reconciliation obligations"
}

// DigestPayload is a small helper for obligation producers that need a stable
// content digest without importing a second canonicalization package.
func DigestPayload(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
