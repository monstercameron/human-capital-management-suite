// Package ledger owns the authoritative append path of the transaction ledger
// (owner: data plane; phase: P1A).
//
// The ledger records the causal history of a transaction. Authority is attached
// to the assertion, never inferred from the fact that an event was recorded, so
// every append states its assertion class explicitly and an authority-bearing
// class must cite an authority assignment that covers the effective instant.
//
// Callers supply what they know: tenant, stream, expected head, assertion class,
// provenance, times and typed payload. They cannot supply the sequence, the
// digest or the recorded time - those are the ledger's own, and an AppendRequest
// has no field for them.
//
// Append runs inside a transaction the caller owns, and performs no external
// call of any kind. External effects belong after the commit, through the outbox.
package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// MaxInlinePayloadBytes matches the CHECK constraint on ledger_event. Anything
// larger must be stored as a governed artifact and referenced.
const MaxInlinePayloadBytes = 65536

// AssertionClass is the truth class of an assertion (ledger specification 8.5).
type AssertionClass string

// The five assertion classes. There is no sixth.
const (
	// TransactionFact records Human Capital Management Suite's own proposal, decision, plan, attempt
	// or transaction result.
	TransactionFact AssertionClass = "TRANSACTION_FACT"
	// DomainFact records a fact Human Capital Management Suite is the configured authority for.
	DomainFact AssertionClass = "DOMAIN_FACT"
	// ExternalObservation records what another configured authority reported.
	ExternalObservation AssertionClass = "EXTERNAL_OBSERVATION"
	// Claim records an assertion not yet promoted to domain truth.
	Claim AssertionClass = "CLAIM"
	// Correction records a governed assertion that supersedes an earlier one.
	Correction AssertionClass = "CORRECTION"
)

// Valid reports whether the class is one of the five declared classes.
func (c AssertionClass) Valid() bool {
	switch c {
	case TransactionFact, DomainFact, ExternalObservation, Claim, Correction:
		return true
	default:
		return false
	}
}

// RequiresAuthority reports whether the class may only be recorded against an
// authority assignment. An external observation cannot become a domain fact
// merely by being written down.
func (c AssertionClass) RequiresAuthority() bool {
	return c == DomainFact || c == ExternalObservation
}

// EventRef points at one recorded assertion inside a tenant.
type EventRef struct {
	StreamKey string
	Sequence  int64
}

// AppendRequest is everything a caller may state about an assertion.
//
// It deliberately has no Sequence, Digest or RecordedAt field: those are
// allocated and computed by the ledger, and a caller that could supply them
// could forge history.
type AppendRequest struct {
	// Tenant scopes the whole assertion. Nothing is written without it.
	Tenant uuid.UUID
	// StreamKey is the semantic stream identity, not a physical partition.
	StreamKey string
	// ExpectedHead is the sequence the caller believes the stream is at. Zero
	// means the caller expects an empty stream.
	ExpectedHead int64
	// AssertionClass states the kind of truth being recorded.
	AssertionClass AssertionClass
	// Authority names the authority assignment for an authority-bearing class.
	Authority string
	// SourceRef records who or what produced the assertion.
	SourceRef string
	// SchemaRef names the registered payload schema.
	SchemaRef string
	// Payload holds typed protobuf bytes, or nil when ArtifactRef is used.
	Payload []byte
	// ArtifactRef references governed bytes held outside the envelope.
	ArtifactRef string
	// OccurredAt is when the originating activity occurred.
	OccurredAt time.Time
	// EffectiveAt is when the business fact applies.
	EffectiveAt time.Time
	// CorrelationID ties the assertion to its transaction.
	CorrelationID uuid.UUID
	// CausationID names the assertion that caused this one, if any.
	CausationID uuid.UUID
	// IdempotencyKey makes an exact replay return the original receipt.
	IdempotencyKey string
	// Corrects is required for CORRECTION and forbidden otherwise.
	Corrects *EventRef
}

// AppendReceipt is the ledger's answer: what was written, where, and under which
// digest.
type AppendReceipt struct {
	EventID         uuid.UUID
	Tenant          uuid.UUID
	StreamKey       string
	Sequence        int64
	PreviousHead    int64
	Digest          string
	DigestAlgorithm string
	CanonicalLength int
	RecordedAt      time.Time
	// Replayed reports that this receipt describes an assertion that was already
	// recorded under the same idempotency key with the same bytes.
	Replayed bool
}

// Appender appends assertions to a stream. Its collaborators are injected so
// that the canonicalization profile and the trusted clock are replaceable.
type Appender struct {
	digester Digester
	now      func() time.Time
}

// Option configures an Appender.
type Option func(*Appender)

// WithDigester replaces the canonical digest profile.
func WithDigester(d Digester) Option {
	return func(a *Appender) { a.digester = d }
}

// WithClock replaces the source of the recorded time.
func WithClock(now func() time.Time) Option {
	return func(a *Appender) { a.now = now }
}

// New builds an Appender. Without options it uses the sha256 profile and the
// host clock.
func New(opts ...Option) *Appender {
	a := &Appender{
		digester: SHA256Digester{},
		now:      func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Append appends one assertion using the default appender.
func Append(ctx context.Context, tx dbport.Tx, req AppendRequest) (AppendReceipt, error) {
	return New().Append(ctx, tx, req)
}

// EnsureStream registers a stream and its head if they do not exist yet. Stream
// identity is semantic and tenant scoped; the physical partition is replaceable.
func EnsureStream(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, streamKey, streamKind, subjectRef string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO ledger_stream (tenant_id, stream_key, stream_kind, subject_ref)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, stream_key) DO NOTHING`,
		tenant, streamKey, streamKind, subjectRef); err != nil {
		return fmt.Errorf("register stream %s: %w", streamKey, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO stream_head (tenant_id, stream_key, head_sequence)
		VALUES ($1, $2, 0)
		ON CONFLICT (tenant_id, stream_key) DO NOTHING`,
		tenant, streamKey); err != nil {
		return fmt.Errorf("register stream head %s: %w", streamKey, err)
	}
	return nil
}

// Append validates the request, locks the stream head, checks the expected head,
// allocates the next sequence, computes the canonical digest, writes the event
// and advances the head - all inside the caller's transaction.
func (a *Appender) Append(ctx context.Context, tx dbport.Tx, req AppendRequest) (AppendReceipt, error) {
	if err := validate(req); err != nil {
		return AppendReceipt{}, err
	}

	algorithm, digest, length, err := a.digester.Digest(digestInput(req), req.SchemaRef)
	if err != nil {
		return AppendReceipt{}, fmt.Errorf("canonical digest: %w", err)
	}

	// An exact replay returns the original receipt; the same key over different
	// bytes is a conflict, never a silent overwrite.
	if receipt, found, err := a.replay(ctx, tx, req, digest, algorithm, length); err != nil {
		return AppendReceipt{}, err
	} else if found {
		return receipt, nil
	}

	// Lock the head. Every appender to this stream serializes here, which is what
	// makes the compare-and-swap exact rather than optimistic.
	var head int64
	err = tx.QueryRow(ctx, `
		SELECT head_sequence FROM stream_head
		WHERE tenant_id = $1 AND stream_key = $2
		FOR UPDATE`, req.Tenant, req.StreamKey).Scan(&head)
	if errors.Is(err, dbport.ErrNoRows) {
		return AppendReceipt{}, ErrStreamNotFound{Tenant: req.Tenant, StreamKey: req.StreamKey}
	}
	if err != nil {
		return AppendReceipt{}, fmt.Errorf("lock stream head %s: %w", req.StreamKey, err)
	}
	if head != req.ExpectedHead {
		// A concurrent exact replay may have committed while this call waited
		// for the head lock. Re-check it so the loser receives the original
		// receipt instead of a stale-stream error.
		if receipt, found, err := a.replay(ctx, tx, req, digest, algorithm, length); err != nil {
			return AppendReceipt{}, err
		} else if found {
			return receipt, nil
		}
		return AppendReceipt{}, ErrStaleStream{
			Tenant:    req.Tenant,
			StreamKey: req.StreamKey,
			Expected:  req.ExpectedHead,
			Actual:    head,
		}
	}

	if req.AssertionClass.RequiresAuthority() {
		if err := a.checkAuthority(ctx, tx, req); err != nil {
			return AppendReceipt{}, err
		}
	}

	write := a.planEvent(req, head, digest, algorithm, length)
	counts, err := dbport.ExecAll(ctx, tx, write.statements[:])
	if err != nil {
		if len(counts) > 0 {
			return AppendReceipt{}, fmt.Errorf("advance stream head %s: %w", req.StreamKey, err)
		}
		if conflict, ok := idempotencyConflict(err, req, digest); ok {
			return AppendReceipt{}, conflict
		}
		return AppendReceipt{}, fmt.Errorf("append to stream %s at sequence %d: %w", req.StreamKey, write.receipt.Sequence, err)
	}
	if len(counts) != len(write.statements) {
		return AppendReceipt{}, fmt.Errorf("append to stream %s at sequence %d: batch returned %d results, want %d", req.StreamKey, write.receipt.Sequence, len(counts), len(write.statements))
	}
	if counts[1] != 1 {
		return AppendReceipt{}, fmt.Errorf("advance stream head %s: %d rows updated", req.StreamKey, counts[1])
	}

	return write.receipt, nil
}

// insertEventSQL records one event row. The column list is the whole
// ledger_event row shape; it exists once so the single-stream and the
// multi-stream append cannot drift apart.
const insertEventSQL = `
		INSERT INTO ledger_event (
			tenant_id, stream_key, sequence, event_id, assertion_class, authority_ref,
			source_ref, schema_ref, payload, artifact_ref, canonical_length, digest,
			digest_algorithm, occurred_at, effective_at, recorded_at, correlation_id,
			causation_id, idempotency_key, corrects_stream_key, corrects_sequence)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
			$17, $18, $19, $20, $21)`

// advanceHeadSQL moves a stream head to the sequence and digest just
// recorded. Exactly one row must change.
const advanceHeadSQL = `
		UPDATE stream_head
		SET head_sequence = $3, head_digest = $4, head_digest_algorithm = $5, updated_at = $6
		WHERE tenant_id = $1 AND stream_key = $2`

// eventWrite is one event's insert and head advance together with the
// receipt they produce once executed.
type eventWrite struct {
	receipt    AppendReceipt
	statements [2]dbport.Statement
}

// planEvent builds the two statements that record req at head+1. It does no
// I/O: the single-stream append executes them on their own and the
// multi-stream append collects them into one batch.
func (a *Appender) planEvent(req AppendRequest, head int64, digest, algorithm string, length int) eventWrite {
	sequence := head + 1
	recordedAt := a.now()
	eventID := uuid.New()

	var payload any
	if req.Payload != nil {
		payload = req.Payload
	}
	var artifactRef any
	if req.ArtifactRef != "" {
		artifactRef = req.ArtifactRef
	}
	var authority any
	if req.Authority != "" {
		authority = req.Authority
	}
	var causation any
	if req.CausationID != uuid.Nil {
		causation = req.CausationID
	}
	var correctsStream, correctsSequence any
	if req.Corrects != nil {
		correctsStream = req.Corrects.StreamKey
		correctsSequence = req.Corrects.Sequence
	}

	return eventWrite{
		receipt: AppendReceipt{
			EventID:         eventID,
			Tenant:          req.Tenant,
			StreamKey:       req.StreamKey,
			Sequence:        sequence,
			PreviousHead:    head,
			Digest:          digest,
			DigestAlgorithm: algorithm,
			CanonicalLength: length,
			RecordedAt:      recordedAt,
		},
		statements: [2]dbport.Statement{
			{SQL: insertEventSQL, Args: []any{
				req.Tenant, req.StreamKey, sequence, eventID, string(req.AssertionClass), authority,
				req.SourceRef, req.SchemaRef, payload, artifactRef, length, digest,
				algorithm, req.OccurredAt, req.EffectiveAt, recordedAt, req.CorrelationID,
				causation, req.IdempotencyKey, correctsStream, correctsSequence,
			}},
			{SQL: advanceHeadSQL, Args: []any{
				req.Tenant, req.StreamKey, sequence, digest, algorithm, recordedAt,
			}},
		},
	}
}

// idempotencyConflict maps the unique violation on the idempotency index to
// the typed conflict error the ledger contract promises.
func idempotencyConflict(err error, req AppendRequest, digest string) (ErrIdempotencyConflict, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "ledger_event_idempotency_unique" {
		return ErrIdempotencyConflict{
			StreamKey:      req.StreamKey,
			IdempotencyKey: req.IdempotencyKey,
			RequestDigest:  digest,
		}, true
	}
	return ErrIdempotencyConflict{}, false
}

// digestInput is the byte string the canonical digest covers: the inline payload
// when there is one, otherwise the governed artifact reference that stands in
// for it. Either way the schema reference is bound in by the digester.
func digestInput(req AppendRequest) []byte {
	if req.Payload != nil {
		return req.Payload
	}
	return []byte(req.ArtifactRef)
}

func (a *Appender) replay(ctx context.Context, tx dbport.Tx, req AppendRequest, digest, algorithm string, length int) (AppendReceipt, bool, error) {
	var (
		eventID    uuid.UUID
		sequence   int64
		recorded   string
		recordedAt time.Time
	)
	err := tx.QueryRow(ctx, `
		SELECT event_id, sequence, digest, recorded_at FROM ledger_event
		WHERE tenant_id = $1 AND stream_key = $2 AND idempotency_key = $3`,
		req.Tenant, req.StreamKey, req.IdempotencyKey).Scan(&eventID, &sequence, &recorded, &recordedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return AppendReceipt{}, false, nil
	}
	if err != nil {
		return AppendReceipt{}, false, fmt.Errorf("read idempotent replay for %s: %w", req.StreamKey, err)
	}
	if recorded != digest {
		return AppendReceipt{}, false, ErrIdempotencyConflict{
			StreamKey:      req.StreamKey,
			IdempotencyKey: req.IdempotencyKey,
			RecordedDigest: recorded,
			RequestDigest:  digest,
		}
	}
	return AppendReceipt{
		EventID:         eventID,
		Tenant:          req.Tenant,
		StreamKey:       req.StreamKey,
		Sequence:        sequence,
		PreviousHead:    sequence - 1,
		Digest:          recorded,
		DigestAlgorithm: algorithm,
		CanonicalLength: length,
		RecordedAt:      recordedAt,
		Replayed:        true,
	}, true, nil
}

// checkAuthority proves the cited authority governs the effective instant. The
// interval is half-open, so an assignment that ends exactly at effective_at does
// not cover it.
func (a *Appender) checkAuthority(ctx context.Context, tx dbport.Tx, req AppendRequest) error {
	var covered bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM authority_assignment
			WHERE tenant_id = $1 AND authority_ref = $2
			  AND effective_from <= $3
			  AND (effective_to IS NULL OR effective_to > $3)
		)`, req.Tenant, req.Authority, req.EffectiveAt).Scan(&covered)
	if err != nil {
		return fmt.Errorf("read authority assignment %s: %w", req.Authority, err)
	}
	if !covered {
		return ErrAuthorityNotAssigned{
			AssertionClass: req.AssertionClass,
			AuthorityRef:   req.Authority,
			EffectiveAt:    req.EffectiveAt,
		}
	}
	return nil
}

func validate(req AppendRequest) error {
	if req.Tenant == uuid.Nil {
		return ErrRequestInvalid{Field: "Tenant", Reason: "is required"}
	}
	if req.StreamKey == "" {
		return ErrRequestInvalid{Field: "StreamKey", Reason: "is required"}
	}
	if req.ExpectedHead < 0 {
		return ErrRequestInvalid{Field: "ExpectedHead", Reason: "cannot be negative"}
	}
	if !req.AssertionClass.Valid() {
		return ErrInvalidAssertionClass{AssertionClass: string(req.AssertionClass)}
	}
	if req.SourceRef == "" {
		return ErrRequestInvalid{Field: "SourceRef", Reason: "is required; provenance is never implicit"}
	}
	if req.SchemaRef == "" {
		return ErrRequestInvalid{Field: "SchemaRef", Reason: "is required"}
	}
	if req.IdempotencyKey == "" {
		return ErrRequestInvalid{Field: "IdempotencyKey", Reason: "is required"}
	}
	if req.CorrelationID == uuid.Nil {
		return ErrRequestInvalid{Field: "CorrelationID", Reason: "is required"}
	}
	if req.OccurredAt.IsZero() {
		return ErrRequestInvalid{Field: "OccurredAt", Reason: "is required"}
	}
	if req.EffectiveAt.IsZero() {
		return ErrRequestInvalid{Field: "EffectiveAt", Reason: "is required"}
	}

	hasPayload := req.Payload != nil
	hasArtifact := req.ArtifactRef != ""
	switch {
	case hasPayload && hasArtifact:
		return ErrPayloadReference{Reason: "an event carries a typed payload or an artifact reference, never both"}
	case !hasPayload && !hasArtifact:
		return ErrPayloadReference{Reason: "an event carries a typed payload or an artifact reference"}
	}
	if hasPayload && len(req.Payload) > MaxInlinePayloadBytes {
		return ErrPayloadTooLarge{Length: len(req.Payload), Limit: MaxInlinePayloadBytes}
	}

	if req.AssertionClass.RequiresAuthority() && req.Authority == "" {
		return ErrAuthorityNotAssigned{AssertionClass: req.AssertionClass, EffectiveAt: req.EffectiveAt}
	}
	if req.AssertionClass == Correction {
		if req.Corrects == nil {
			return ErrCorrectionTarget{AssertionClass: req.AssertionClass, Reason: "must reference the assertion it corrects"}
		}
		if req.Corrects.StreamKey == "" || req.Corrects.Sequence < 1 {
			return ErrCorrectionTarget{AssertionClass: req.AssertionClass, Reason: "must reference an exact stream and sequence"}
		}
	} else if req.Corrects != nil {
		return ErrCorrectionTarget{AssertionClass: req.AssertionClass, Reason: "cannot reference a correction target"}
	}

	return nil
}
