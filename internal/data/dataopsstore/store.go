// Package dataopsstore persists immutable CSV import staging artifacts.
//
// Staging is deliberately a data-plane write separate from business facts: the
// store records source bytes and content digests so a later capability can
// profile, map, validate and simulate the exact artifact that was admitted.
package dataopsstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops/importing"
)

const MaxPayloadBytes = importing.MaxSourceCSVBytes

const (
	MaxSourceURIBytes      = 4096
	MaxIdempotencyKeyBytes = 256
)

var (
	ErrInvalid            = errors.New("dataopsstore: invalid staging request")
	ErrConflict           = errors.New("dataopsstore: idempotency key conflicts with an existing batch")
	ErrNotFound           = errors.New("dataopsstore: batch not found")
	ErrPayloadTooLarge    = errors.New("dataopsstore: payload exceeds the staging limit")
	ErrDatabaseCapability = errors.New("dataopsstore: database capability is required")
)

// DB is the only database capability this adapter needs.
type DB interface{ dbport.Beginner }

// Store is a durable, tenant-scoped staging store.
type Store struct{ db DB }

func New(db DB) *Store { return &Store{db: db} }

// Record is the durable staging receipt and source payload.
type Record struct {
	TenantID        uuid.UUID
	BatchID         uuid.UUID
	IdempotencyKey  string
	RequestDigest   string
	SourceURI       string
	SourceHash      string
	BatchDigest     string
	Header          []string
	RowCount        int
	Classification  string
	SchemaCandidate string
	Payload         []byte
	RetrievedAt     time.Time
	CreatedAt       time.Time
}

// Put stores one immutable batch. A repeat with the same tenant, idempotency
// key and request digest returns the original record; a different digest is
// refused before any new row can be created.
func (s *Store) Put(ctx context.Context, tenantID uuid.UUID, idempotencyKey, sourceHash string, batch importing.Batch, payload []byte, classification, schemaCandidate string) (Record, error) {
	if s == nil || s.db == nil {
		return Record{}, ErrDatabaseCapability
	}
	if tenantID == uuid.Nil || strings.TrimSpace(idempotencyKey) == "" || len(idempotencyKey) > MaxIdempotencyKeyBytes {
		return Record{}, ErrInvalid
	}
	if err := batch.Validate(); err != nil {
		return Record{}, fmt.Errorf("%w: batch: %v", ErrInvalid, err)
	}
	if len(payload) > MaxPayloadBytes {
		return Record{}, ErrPayloadTooLarge
	}
	if len(batch.Source().URI) > MaxSourceURIBytes || !validDigest(sourceHash) || strings.TrimSpace(classification) == "" || strings.TrimSpace(schemaCandidate) == "" {
		return Record{}, ErrInvalid
	}
	requestDigest := digestRequest(batch.Source().URI, payload)
	header, err := json.Marshal(batch.Header())
	if err != nil {
		return Record{}, fmt.Errorf("%w: header: %v", ErrInvalid, err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Record{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return Record{}, err
	}
	var existing Record
	var existingHeader []byte
	if err := tx.QueryRow(ctx, `
		SELECT tenant_id, batch_id, idempotency_key, request_digest, source_uri,
		       source_hash, batch_digest, header, row_count, classification,
		       schema_candidate, payload, retrieved_at, created_at
		FROM dataops_import_batch
		WHERE tenant_id=$1 AND idempotency_key=$2`, tenantID, idempotencyKey).
		Scan(&existing.TenantID, &existing.BatchID, &existing.IdempotencyKey,
			&existing.RequestDigest, &existing.SourceURI, &existing.SourceHash,
			&existing.BatchDigest, &existingHeader, &existing.RowCount,
			&existing.Classification, &existing.SchemaCandidate, &existing.Payload,
			&existing.RetrievedAt, &existing.CreatedAt); err == nil {
		if existing.RequestDigest != requestDigest {
			return Record{}, ErrConflict
		}
		if err := json.Unmarshal(existingHeader, &existing.Header); err != nil {
			return Record{}, fmt.Errorf("%w: stored header: %v", ErrInvalid, err)
		}
		return existing, tx.Commit(ctx)
	} else if !errors.Is(err, dbport.ErrNoRows) {
		return Record{}, err
	}
	existing = Record{
		TenantID: tenantID, BatchID: uuid.New(), IdempotencyKey: idempotencyKey,
		RequestDigest: requestDigest, SourceURI: batch.Source().URI,
		SourceHash: sourceHash, BatchDigest: batch.Digest(), Header: batch.Header(),
		RowCount: batch.RowCount(), Classification: classification,
		SchemaCandidate: schemaCandidate, Payload: append([]byte(nil), payload...),
		RetrievedAt: batch.RetrievedAt().Time(),
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO dataops_import_batch
			(tenant_id, batch_id, idempotency_key, request_digest, source_uri,
			 source_hash, batch_digest, header, row_count, classification,
			 schema_candidate, payload, retrieved_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		existing.TenantID, existing.BatchID, existing.IdempotencyKey,
		existing.RequestDigest, existing.SourceURI, existing.SourceHash,
		existing.BatchDigest, header, existing.RowCount, existing.Classification,
		existing.SchemaCandidate, existing.Payload, existing.RetrievedAt)
	if err != nil {
		return Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Record{}, err
	}
	return existing, nil
}

// Get returns a tenant-scoped staging artifact, including the original bytes.
func (s *Store) Get(ctx context.Context, tenantID, batchID uuid.UUID) (Record, error) {
	if s == nil || s.db == nil {
		return Record{}, ErrDatabaseCapability
	}
	if tenantID == uuid.Nil || batchID == uuid.Nil {
		return Record{}, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Record{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return Record{}, err
	}
	var rec Record
	var header []byte
	err = tx.QueryRow(ctx, `
		SELECT tenant_id, batch_id, idempotency_key, request_digest, source_uri,
		       source_hash, batch_digest, header, row_count, classification,
		       schema_candidate, payload, retrieved_at, created_at
		FROM dataops_import_batch WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID).
		Scan(&rec.TenantID, &rec.BatchID, &rec.IdempotencyKey, &rec.RequestDigest,
			&rec.SourceURI, &rec.SourceHash, &rec.BatchDigest, &header,
			&rec.RowCount, &rec.Classification, &rec.SchemaCandidate, &rec.Payload,
			&rec.RetrievedAt, &rec.CreatedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	if err := json.Unmarshal(header, &rec.Header); err != nil {
		return Record{}, fmt.Errorf("%w: stored header: %v", ErrInvalid, err)
	}
	return rec, tx.Commit(ctx)
}

func validDigest(v string) bool {
	if !strings.HasPrefix(v, "sha256:") || len(v) != len("sha256:")+sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(v, "sha256:"))
	return err == nil
}

func digestRequest(uri string, payload []byte) string {
	h := sha256.New()
	h.Write([]byte(uri))
	h.Write([]byte{0})
	h.Write(payload)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
