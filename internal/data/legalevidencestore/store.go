// Package legalevidencestore persists the immutable evidence emitted by the
// legal rule-pack review, composition and authoring pipeline.
//
// The package owns no policy decisions and opens no background work. A Store
// opens a caller-owned database transaction for each port operation, scopes it
// with tenancy.WithTenant, and commits only the append that was requested.
// Pipeline chain links are checked against the tenant's latest sequence before
// insertion and the complete chain is verified again when it is read.
package legalevidencestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	legalpipeline "github.com/monstercameron/human-capital-management-suite/internal/governance/legal/pipeline"
)

var (
	ErrInvalid     = errors.New("legalevidencestore: invalid row")
	ErrDuplicate   = errors.New("legalevidencestore: duplicate row")
	ErrChainBroken = errors.New("legalevidencestore: pipeline chain broken")
	ErrNotFound    = errors.New("legalevidencestore: not found")
)

// DB is the only capability Store needs. A pgx connection, pool adapter or a
// test connection can satisfy it without exposing a driver type here.
type DB interface{ dbport.Beginner }

// Executor is the low-level capability accepted by the explicit transaction
// helpers. The caller must scope it with tenancy.WithTenant first.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// Store implements the legal evidence ports over PostgreSQL.
type Store struct{ db DB }

var _ legal.ReviewRecordStore = (*Store)(nil)
var _ legal.CompositionReceiptStore = (*Store)(nil)
var _ legalpipeline.EventStore = (*Store)(nil)

// New returns a PostgreSQL-backed evidence store.
func New(db DB) *Store { return &Store{db: db} }

func (s *Store) withTenant(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("%w: database is required", ErrInvalid)
	}
	if tenantID == uuid.Nil {
		return fmt.Errorf("%w: tenant is required", ErrInvalid)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("legalevidencestore: begin transaction: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("legalevidencestore: commit transaction: %w", err)
	}
	return nil
}

func parseUUID(field, value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: %s must be a non-nil UUID", ErrInvalid, field)
	}
	return id, nil
}

func (s *Store) withTenantString(ctx context.Context, value string, fn func(dbport.Tx, uuid.UUID) error) error {
	tenantID, err := parseUUID("tenant_id", value)
	if err != nil {
		return err
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx) error { return fn(tx, tenantID) })
}

func jsonValue(value any) ([]byte, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal evidence JSON: %w", err)
	}
	return b, nil
}

func unmarshalJSON(raw []byte, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

func classifyWriteError(kind string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return fmt.Errorf("%w: %s", ErrDuplicate, kind)
		case "23514", "23502", "22P02":
			return fmt.Errorf("%w: %s: %v", ErrInvalid, kind, err)
		}
	}
	return fmt.Errorf("legalevidencestore: write %s: %w", kind, err)
}

// AppendReviewRecord stores one immutable review record.
func (s *Store) AppendReviewRecord(ctx context.Context, in legal.ReviewRecordEntry) (legal.ReviewRecordEntry, error) {
	var out legal.ReviewRecordEntry
	err := s.withTenantString(ctx, in.TenantID, func(tx dbport.Tx, _ uuid.UUID) error {
		if err := InsertReviewRecord(ctx, tx, in); err != nil {
			return err
		}
		out = in
		return nil
	})
	if err != nil {
		return legal.ReviewRecordEntry{}, err
	}
	return out, nil
}

// ListReviewRecords returns one pack's review history in event-sequence order.
func (s *Store) ListReviewRecords(ctx context.Context, tenantID string, packDigest string) ([]legal.ReviewRecordEntry, error) {
	var out []legal.ReviewRecordEntry
	err := s.withTenantString(ctx, tenantID, func(tx dbport.Tx, parsed uuid.UUID) error {
		var err error
		out, err = ListReviewRecords(ctx, tx, parsed, packDigest)
		return err
	})
	return out, err
}

// AppendCompositionReceipt stores one immutable composition receipt.
func (s *Store) AppendCompositionReceipt(ctx context.Context, in legal.CompositionReceiptEntry) (legal.CompositionReceiptEntry, error) {
	var out legal.CompositionReceiptEntry
	err := s.withTenantString(ctx, in.TenantID, func(tx dbport.Tx, _ uuid.UUID) error {
		if err := InsertCompositionReceipt(ctx, tx, in); err != nil {
			return err
		}
		out = in
		return nil
	})
	if err != nil {
		return legal.CompositionReceiptEntry{}, err
	}
	return out, nil
}

// ListCompositionReceipts returns a tenant's receipt history in sequence order.
func (s *Store) ListCompositionReceipts(ctx context.Context, tenantID string) ([]legal.CompositionReceiptEntry, error) {
	var out []legal.CompositionReceiptEntry
	err := s.withTenantString(ctx, tenantID, func(tx dbport.Tx, parsed uuid.UUID) error {
		var err error
		out, err = ListCompositionReceipts(ctx, tx, parsed)
		return err
	})
	return out, err
}

// AppendEvent stores one immutable pipeline event after checking its sequence
// and predecessor against the tenant's current durable chain head.
func (s *Store) AppendEvent(ctx context.Context, in legalpipeline.EventEntry) (legalpipeline.EventEntry, error) {
	var out legalpipeline.EventEntry
	err := s.withTenantString(ctx, in.TenantID, func(tx dbport.Tx, _ uuid.UUID) error {
		if err := InsertPipelineEvent(ctx, tx, in); err != nil {
			return err
		}
		out = in
		return nil
	})
	if err != nil {
		return legalpipeline.EventEntry{}, err
	}
	return out, nil
}

// ListEvents returns and verifies the complete tenant pipeline chain.
func (s *Store) ListEvents(ctx context.Context, tenantID string) ([]legalpipeline.EventEntry, error) {
	var out []legalpipeline.EventEntry
	err := s.withTenantString(ctx, tenantID, func(tx dbport.Tx, parsed uuid.UUID) error {
		var err error
		out, err = ListPipelineEvents(ctx, tx, parsed)
		return err
	})
	return out, err
}

// InsertReviewRecord is the explicit-transaction form of AppendReviewRecord.
func InsertReviewRecord(ctx context.Context, ex Executor, in legal.ReviewRecordEntry) error {
	if err := in.Validate(); err != nil {
		return err
	}
	tenantID, err := parseUUID("tenant_id", in.TenantID)
	if err != nil {
		return err
	}
	rowID, err := parseUUID("row_id", in.RowID)
	if err != nil {
		return err
	}
	findings, err := jsonValue(in.Record.Findings)
	if err != nil {
		return err
	}
	_, err = ex.Exec(ctx, `
		INSERT INTO legal_review_record
			(tenant_id, row_id, pack_digest, author_id, reviewer_id, status,
			 findings, digest, event_sequence)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		tenantID, rowID, in.Record.PackDigest, in.Record.AuthorID,
		in.Record.ReviewerID, in.Record.Status.String(), findings, in.Digest,
		in.EventSequence)
	if err != nil {
		return classifyWriteError("legal_review_record", err)
	}
	return nil
}

// ListReviewRecords is the explicit-transaction read form.
func ListReviewRecords(ctx context.Context, q dbport.Querier, tenantID uuid.UUID, packDigest string) ([]legal.ReviewRecordEntry, error) {
	if tenantID == uuid.Nil || packDigest == "" {
		return nil, fmt.Errorf("%w: tenant and pack digest are required", ErrInvalid)
	}
	rows, err := q.Query(ctx, `
		SELECT row_id, event_sequence, author_id, reviewer_id, status,
			findings, digest
		FROM legal_review_record
		WHERE tenant_id=$1 AND pack_digest=$2
		ORDER BY event_sequence ASC`, tenantID, packDigest)
	if err != nil {
		return nil, fmt.Errorf("legalevidencestore: list review records: %w", err)
	}
	defer rows.Close()
	var out []legal.ReviewRecordEntry
	for rows.Next() {
		var (
			entry    legal.ReviewRecordEntry
			rowID    uuid.UUID
			findings []byte
			status   string
		)
		entry.TenantID = tenantID.String()
		entry.Record.PackDigest = packDigest
		if err := rows.Scan(&rowID, &entry.EventSequence, &entry.Record.AuthorID,
			&entry.Record.ReviewerID, &status, &findings, &entry.Digest); err != nil {
			return nil, fmt.Errorf("legalevidencestore: scan review record: %w", err)
		}
		entry.RowID = rowID.String()
		entry.Record.Status, err = legal.ParseReviewStatus(status)
		if err != nil {
			return nil, fmt.Errorf("%w: stored review status: %v", ErrInvalid, err)
		}
		if err := unmarshalJSON(findings, &entry.Record.Findings); err != nil {
			return nil, fmt.Errorf("legalevidencestore: decode review findings: %w", err)
		}
		if err := entry.Validate(); err != nil {
			return nil, fmt.Errorf("%w: stored review record %d: %v", ErrInvalid, entry.EventSequence, err)
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("legalevidencestore: list review records: %w", err)
	}
	return out, nil
}

// InsertCompositionReceipt is the explicit-transaction form of
// AppendCompositionReceipt.
func InsertCompositionReceipt(ctx context.Context, ex Executor, in legal.CompositionReceiptEntry) error {
	if err := in.Validate(); err != nil {
		return err
	}
	tenantID, err := parseUUID("tenant_id", in.TenantID)
	if err != nil {
		return err
	}
	rowID, err := parseUUID("row_id", in.RowID)
	if err != nil {
		return err
	}
	jurisdictions, err := jsonValue(in.Receipt.Jurisdictions)
	if err != nil {
		return err
	}
	inputs, err := jsonValue(in.Receipt.Inputs)
	if err != nil {
		return err
	}
	obligations, err := jsonValue(in.Receipt.Obligations)
	if err != nil {
		return err
	}
	traces, err := jsonValue(in.Receipt.Traces)
	if err != nil {
		return err
	}
	contradictions, err := jsonValue(in.Receipt.Contradictions)
	if err != nil {
		return err
	}
	_, err = ex.Exec(ctx, `
		INSERT INTO legal_composition_receipt
			(tenant_id, row_id, status, jurisdictions, inputs, obligations,
			 traces, contradictions, digest, event_sequence)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		tenantID, rowID, string(in.Receipt.Status), jurisdictions, inputs,
		obligations, traces, contradictions, in.Digest, in.EventSequence)
	if err != nil {
		return classifyWriteError("legal_composition_receipt", err)
	}
	return nil
}

// ListCompositionReceipts is the explicit-transaction read form.
func ListCompositionReceipts(ctx context.Context, q dbport.Querier, tenantID uuid.UUID) ([]legal.CompositionReceiptEntry, error) {
	if tenantID == uuid.Nil {
		return nil, fmt.Errorf("%w: tenant is required", ErrInvalid)
	}
	rows, err := q.Query(ctx, `
		SELECT row_id, status, jurisdictions, inputs, obligations, traces,
			contradictions, digest, event_sequence
		FROM legal_composition_receipt
		WHERE tenant_id=$1
		ORDER BY event_sequence ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("legalevidencestore: list composition receipts: %w", err)
	}
	defer rows.Close()
	var out []legal.CompositionReceiptEntry
	for rows.Next() {
		var (
			entry                                                      legal.CompositionReceiptEntry
			rowID                                                      uuid.UUID
			status                                                     string
			jurisdictions, inputs, obligations, traces, contradictions []byte
		)
		entry.TenantID = tenantID.String()
		if err := rows.Scan(&rowID, &status, &jurisdictions, &inputs,
			&obligations, &traces, &contradictions, &entry.Digest, &entry.EventSequence); err != nil {
			return nil, fmt.Errorf("legalevidencestore: scan composition receipt: %w", err)
		}
		entry.RowID = rowID.String()
		entry.Receipt.Status = legal.CompositionStatus(status)
		entry.Receipt.Digest = entry.Digest
		if err := unmarshalJSON(jurisdictions, &entry.Receipt.Jurisdictions); err != nil {
			return nil, fmt.Errorf("legalevidencestore: decode jurisdictions: %w", err)
		}
		if err := unmarshalJSON(inputs, &entry.Receipt.Inputs); err != nil {
			return nil, fmt.Errorf("legalevidencestore: decode composition inputs: %w", err)
		}
		if err := unmarshalJSON(obligations, &entry.Receipt.Obligations); err != nil {
			return nil, fmt.Errorf("legalevidencestore: decode composition obligations: %w", err)
		}
		if err := unmarshalJSON(traces, &entry.Receipt.Traces); err != nil {
			return nil, fmt.Errorf("legalevidencestore: decode composition traces: %w", err)
		}
		if err := unmarshalJSON(contradictions, &entry.Receipt.Contradictions); err != nil {
			return nil, fmt.Errorf("legalevidencestore: decode contradictions: %w", err)
		}
		if err := entry.Validate(); err != nil {
			return nil, fmt.Errorf("%w: stored composition receipt %d: %v", ErrInvalid, entry.EventSequence, err)
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("legalevidencestore: list composition receipts: %w", err)
	}
	return out, nil
}

func encodeSignature(sig legal.Signature) ([]byte, error) {
	// The schema has one bytea signature column. Store the complete signature
	// envelope so a fresh connection can verify the chain without depending on
	// an in-memory signer registry; the bytes remain detached signature data.
	return jsonValue(sig)
}

func decodeSignature(raw []byte, sig *legal.Signature) error {
	if err := unmarshalJSON(raw, sig); err != nil {
		return err
	}
	return nil
}

// InsertPipelineEvent is the explicit-transaction form of AppendEvent.
func InsertPipelineEvent(ctx context.Context, ex Executor, in legalpipeline.EventEntry) error {
	if err := in.Validate(); err != nil {
		return err
	}
	tenantID, err := parseUUID("tenant_id", in.TenantID)
	if err != nil {
		return err
	}
	rowID, err := parseUUID("row_id", in.RowID)
	if err != nil {
		return err
	}
	var previousSequence int64
	var previousDigest string
	err = ex.QueryRow(ctx, `
		SELECT event_sequence, digest
		FROM legal_pipeline_event
		WHERE tenant_id=$1
		ORDER BY event_sequence DESC
		LIMIT 1`, tenantID).Scan(&previousSequence, &previousDigest)
	if err != nil && !errors.Is(err, dbport.ErrNoRows) {
		return fmt.Errorf("legalevidencestore: read pipeline head: %w", err)
	}
	if errors.Is(err, dbport.ErrNoRows) {
		if in.EventSequence != 1 || in.Event.PrevDigest != "" {
			return fmt.Errorf("%w: pipeline genesis sequence or predecessor is stale", ErrChainBroken)
		}
	} else if in.EventSequence != previousSequence+1 || in.Event.PrevDigest != previousDigest {
		return fmt.Errorf("%w: pipeline predecessor is stale", ErrChainBroken)
	}
	signature, err := encodeSignature(in.Event.Signature)
	if err != nil {
		return err
	}
	_, err = ex.Exec(ctx, `
		INSERT INTO legal_pipeline_event
			(tenant_id, row_id, stage, principal_id, role, artifact_digest,
			 prev_digest, digest, signature, event_sequence)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		tenantID, rowID, in.Event.Stage, in.Event.PrincipalID, in.Event.Role,
		in.Event.ArtifactDigest, nullableDigest(in.Event.PrevDigest), in.Event.Digest,
		signature, in.EventSequence)
	if err != nil {
		return classifyWriteError("legal_pipeline_event", err)
	}
	return nil
}

func nullableDigest(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// ListPipelineEvents is the explicit-transaction read form and the durable
// chain verification boundary.
func ListPipelineEvents(ctx context.Context, q dbport.Querier, tenantID uuid.UUID) ([]legalpipeline.EventEntry, error) {
	if tenantID == uuid.Nil {
		return nil, fmt.Errorf("%w: tenant is required", ErrInvalid)
	}
	rows, err := q.Query(ctx, `
		SELECT row_id, stage, principal_id, role, artifact_digest, prev_digest,
			digest, signature, event_sequence
		FROM legal_pipeline_event
		WHERE tenant_id=$1
		ORDER BY event_sequence ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("legalevidencestore: list pipeline events: %w", err)
	}
	defer rows.Close()
	var out []legalpipeline.EventEntry
	for rows.Next() {
		var (
			entry                      legalpipeline.EventEntry
			rowID                      uuid.UUID
			prevDigest, signatureBytes []byte
		)
		entry.TenantID = tenantID.String()
		if err := rows.Scan(&rowID, &entry.Event.Stage, &entry.Event.PrincipalID,
			&entry.Event.Role, &entry.Event.ArtifactDigest, &prevDigest, &entry.Event.Digest,
			&signatureBytes, &entry.EventSequence); err != nil {
			return nil, fmt.Errorf("legalevidencestore: scan pipeline event: %w", err)
		}
		entry.RowID = rowID.String()
		if len(prevDigest) != 0 {
			entry.Event.PrevDigest = string(prevDigest)
		}
		if err := decodeSignature(signatureBytes, &entry.Event.Signature); err != nil {
			return nil, fmt.Errorf("legalevidencestore: decode pipeline signature: %w", err)
		}
		if err := entry.Validate(); err != nil {
			return nil, fmt.Errorf("%w: stored pipeline event %d: %v", ErrInvalid, entry.EventSequence, err)
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("legalevidencestore: list pipeline events: %w", err)
	}
	// Only the event rows are persisted, so the durable read verifies the
	// event-level hash chain; authoring evidence is not stored here.
	events := make([]legalpipeline.Event, 0, len(out))
	for _, entry := range out {
		events = append(events, entry.Event)
	}
	if err := legalpipeline.VerifyEvents(events); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrChainBroken, err)
	}
	return out, nil
}
