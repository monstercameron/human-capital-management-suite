// Package dataopsimport is the application boundary for CSV staging. It
// derives tenant identity from the authenticated principal and delegates the
// immutable artifact write to dataopsstore; it does not write business facts.
package dataopsimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	dataopsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dataopsstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops/importing"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrStoreUnavailable = errors.New("dataopsimport: staging store is unavailable")
	ErrTenantMapper     = errors.New("dataopsimport: tenant mapper is unavailable")
	ErrRequestInvalid   = errors.New("dataopsimport: stage request is invalid")
)

// StageRequest is the trusted application input after the transport has
// collected the client stream. Tenant is intentionally absent: it is read
// from the authenticated context by StageFromContext.
type StageRequest struct {
	SourceURI      string
	IdempotencyKey string
	Payload        []byte
}

// StageCSV is the small wire-facing adapter used by the transport cell. It
// keeps the trust and durable staging decisions in this service while
// returning the generated receipt expected by the gRPC adapter.
func (s *Service) StageCSV(ctx context.Context, sourceURI, idempotencyKey string, payload []byte) (*dataopsv1.StageCSVResponse, error) {
	record, err := s.StageFromContext(ctx, StageRequest{SourceURI: sourceURI, IdempotencyKey: idempotencyKey, Payload: payload})
	if err != nil {
		return nil, err
	}
	return &dataopsv1.StageCSVResponse{StagedImport: &dataopsv1.StagedImport{
		Status: dataopsv1.StagedImportStatus_STAGED_IMPORT_STATUS_STAGED, BatchId: record.BatchID.String(),
		RowCount: uint64(record.RowCount), SourceHash: record.SourceHash, SchemaCandidate: record.SchemaCandidate,
		Classification: record.Classification, TenantId: record.TenantID.String(), BatchDigest: record.BatchDigest,
		SourceUri: record.SourceURI, RetrievedAt: timestamppb.New(record.RetrievedAt),
	}}, nil
}

// Service owns CSV staging semantics and its durable store.
type Service struct {
	store      *dataopsstore.Store
	tenantUUID func(values.TenantId) uuid.UUID
	now        func() time.Time
}

// New constructs the application staging service. tenantUUID must be the
// composition root's stable tenant mapping (pgstore.TenantID in production).
func New(store *dataopsstore.Store, tenantUUID func(values.TenantId) uuid.UUID, now func() time.Time) *Service {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, tenantUUID: tenantUUID, now: now}
}

// StageFromContext requires the verified principal installed by the trust
// interceptor. No request field or metadata can select another tenant.
func (s *Service) StageFromContext(ctx context.Context, req StageRequest) (dataopsstore.Record, error) {
	p, err := trust.MustFromContext(ctx)
	if err != nil {
		return dataopsstore.Record{}, err
	}
	return s.Stage(ctx, p.Tenant(), req)
}

// Stage parses and durably records a CSV artifact for tenant. Callers that
// have a principal should use StageFromContext so tenant selection is tied to
// the authenticated trust context.
func (s *Service) Stage(ctx context.Context, tenant values.TenantId, req StageRequest) (dataopsstore.Record, error) {
	if s == nil || s.store == nil {
		return dataopsstore.Record{}, ErrStoreUnavailable
	}
	if s.tenantUUID == nil {
		return dataopsstore.Record{}, ErrTenantMapper
	}
	if err := tenant.Validate(); err != nil {
		return dataopsstore.Record{}, fmt.Errorf("%w: tenant: %v", ErrRequestInvalid, err)
	}
	if strings.TrimSpace(req.SourceURI) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || len(req.SourceURI) > dataopsstore.MaxSourceURIBytes || len(req.IdempotencyKey) > dataopsstore.MaxIdempotencyKeyBytes {
		return dataopsstore.Record{}, ErrRequestInvalid
	}
	if len(req.Payload) == 0 || len(req.Payload) > importing.MaxSourceCSVBytes {
		return dataopsstore.Record{}, ErrRequestInvalid
	}
	now := s.now().UTC()
	if now.IsZero() {
		return dataopsstore.Record{}, fmt.Errorf("%w: retrieval time is unset", ErrRequestInvalid)
	}
	b, err := importing.StageCSV(importing.SourceDescriptor{
		Kind: importing.SourceKindCSV, URI: req.SourceURI, Tenant: tenant,
	}, values.NewInstant(now), bytes.NewReader(req.Payload))
	if err != nil {
		return dataopsstore.Record{}, err
	}
	digest := sha256.Sum256(req.Payload)
	sourceHash := "sha256:" + hex.EncodeToString(digest[:])
	return s.store.Put(ctx, s.tenantUUID(tenant), req.IdempotencyKey, sourceHash, b, req.Payload, "UNCLASSIFIED", "CSV")
}
