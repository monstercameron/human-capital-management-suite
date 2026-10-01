package timecardservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/list"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// WorkerSelfResolver maps an authenticated human subject to the worker
// record in the subject's tenant. The caller never supplies this mapping.
type WorkerSelfResolver interface {
	ResolveOwnWorker(ctx context.Context, tenant, subject string) (string, error)
}

// WorkerRecord is the typed, caller-visible projection of one time record.
// RawPayload and sensitive evidence are deliberately absent from this type.
type WorkerRecord struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	Source        string    `json:"source"`
	ReceiptRef    string    `json:"receipt_ref,omitempty"`
	Confidence    string    `json:"confidence,omitempty"`
	EffectiveDate time.Time `json:"effective_date"`
	EvaluatedDate time.Time `json:"evaluated_date"`
}

// WorkerEvidence is the read-only adapter contract for time observations,
// timecards, attestations, and supporting device evidence. Implementations
// must scope their query by both tenant and worker and return immutable rows.
type WorkerEvidence struct {
	ID, Tenant, WorkerRef, Kind, Source, ReceiptRef, Confidence string
	EffectiveDate, EvaluatedDate                                time.Time
	RawPayload                                                  json.RawMessage
	PhotoEvidence, BiometricEvidence, LocationEvidence          json.RawMessage
	DevicePayload                                               json.RawMessage
}

// WorkerRecordReader lists the evidence used to build a worker's daily view.
// Cursor is an opaque token previously returned by this service.
type WorkerRecordReader interface {
	ListWorkerEvidence(ctx context.Context, tenant, worker string, from, to time.Time, cursor string, limit int) ([]WorkerEvidence, string, error)
}

// WorkerRecordQuery selects a bounded daily-record window. WorkerRef is
// accepted only as a consistency check; identity is resolved from the
// authenticated principal and cannot be claimed by the request.
type WorkerRecordQuery struct {
	WorkerRef string
	From, To  time.Time
	Cursor    string
	Limit     int
}

// WorkerRecordPage is the safe page returned to a worker.
type WorkerRecordPage struct {
	WorkerRef  string         `json:"worker_ref"`
	From       time.Time      `json:"from"`
	To         time.Time      `json:"to"`
	Records    []WorkerRecord `json:"records"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

// WorkerRecordExport is the deterministic export envelope.
type WorkerRecordExport struct {
	WorkerRef string         `json:"worker_ref"`
	From      time.Time      `json:"from"`
	To        time.Time      `json:"to"`
	Records   []WorkerRecord `json:"records"`
}

// WorkerRecordService provides authenticated worker access to time records.
type WorkerRecordService struct {
	Auth      Authorizer
	Workers   WorkerSelfResolver
	Reader    WorkerRecordReader
	CursorKey string
	Now       func() time.Time
}

// CapReadOwnRecords identifies the least-privilege capability for a worker
// reading their own time evidence.
const CapReadOwnRecords Capability = "TIMECARD_READ_OWN"

// NewWorkerRecordService constructs the worker-record read service.
func NewWorkerRecordService(auth Authorizer, workers WorkerSelfResolver, reader WorkerRecordReader, cursorKey string) WorkerRecordService {
	return WorkerRecordService{Auth: auth, Workers: workers, Reader: reader, CursorKey: cursorKey}
}

func (s WorkerRecordService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s WorkerRecordService) ownWorker(ctx context.Context, p *trust.Principal, requested string) (string, error) {
	if err := validPrincipal(p); err != nil || p.SubjectKind() != trust.SubjectKindHuman {
		return "", ErrInvalidPrincipal
	}
	tenant := tenantOf(p)
	if s.Workers == nil {
		return "", ErrUnavailable
	}
	worker, err := s.Workers.ResolveOwnWorker(ctx, tenant, p.Subject())
	if err != nil {
		return "", err
	}
	if requested != "" && requested != worker {
		return "", ErrForbidden
	}
	if strings.TrimSpace(worker) == "" {
		return "", ErrNotFound
	}
	if s.Auth == nil || s.Reader == nil || len(strings.TrimSpace(s.CursorKey)) < 32 {
		return "", ErrUnavailable
	}
	if err := s.Auth.Authorize(ctx, p, tenant, worker, CapReadOwnRecords); err != nil {
		return "", err
	}
	return worker, nil
}

func validateWorkerQuery(q WorkerRecordQuery) (time.Time, time.Time, int, error) {
	from, to := q.From.UTC(), q.To.UTC()
	if from.IsZero() || to.IsZero() || !to.After(from) || to.Sub(from) > 24*time.Hour {
		return time.Time{}, time.Time{}, 0, ErrInvalidRequest
	}
	limit := q.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 100 {
		return time.Time{}, time.Time{}, 0, ErrInvalidRequest
	}
	return from, to, limit, nil
}

func recordFilter(from, to time.Time) string {
	return from.Format(time.RFC3339Nano) + "|" + to.Format(time.RFC3339Nano)
}

func (s WorkerRecordService) decodeCursor(p *trust.Principal, q WorkerRecordQuery, now time.Time) (string, error) {
	if q.Cursor == "" {
		return "", nil
	}
	cp, err := list.DecodeCursor(q.Cursor, s.CursorKey, now, p.Subject(), tenantOf(p), recordFilter(q.From.UTC(), q.To.UTC()))
	if err != nil {
		return "", ErrInvalidRequest
	}
	return cp.Watermark, nil
}

func projectEvidence(e WorkerEvidence, tenant, worker string) (WorkerRecord, error) {
	if e.Tenant != tenant || e.WorkerRef != worker {
		return WorkerRecord{}, ErrNotFound
	}
	if strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.Kind) == "" || strings.TrimSpace(e.Source) == "" || e.EffectiveDate.IsZero() || e.EvaluatedDate.IsZero() {
		return WorkerRecord{}, ErrInvalidRequest
	}
	// The adapter may retain raw evidence for audit, but a worker projection
	// cannot echo it. Requiring an object also catches adapters that pass a
	// forbidden field projection as a scalar.
	if len(e.RawPayload) > 0 {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(e.RawPayload, &raw); err != nil || raw == nil {
			return WorkerRecord{}, ErrInvalidRequest
		}
		for _, forbidden := range []string{"photo", "photo_evidence", "biometric", "biometric_evidence", "location", "location_evidence", "device_payload", "raw_payload"} {
			if _, ok := raw[forbidden]; ok {
				return WorkerRecord{}, ErrForbidden
			}
		}
	}
	return WorkerRecord{ID: e.ID, Kind: e.Kind, Source: e.Source, ReceiptRef: e.ReceiptRef, Confidence: e.Confidence, EffectiveDate: e.EffectiveDate.UTC(), EvaluatedDate: e.EvaluatedDate.UTC()}, nil
}

// ReadOwnRecords returns a redacted, typed page of the authenticated
// worker's records. The tenant and worker sent to the reader come only from
// the principal and worker graph, and the cursor is bound to both.
func (s WorkerRecordService) ReadOwnRecords(ctx context.Context, p *trust.Principal, q WorkerRecordQuery) (WorkerRecordPage, error) {
	from, to, limit, err := validateWorkerQuery(q)
	if err != nil {
		return WorkerRecordPage{}, err
	}
	now := s.now()
	if err := validPrincipal(p); err != nil || p.SubjectKind() != trust.SubjectKindHuman || p.IssuedAt().After(now) || !p.ExpiresAt().After(now) {
		return WorkerRecordPage{}, ErrInvalidPrincipal
	}
	worker, err := s.ownWorker(ctx, p, q.WorkerRef)
	if err != nil {
		return WorkerRecordPage{}, err
	}
	cursor, err := s.decodeCursor(p, WorkerRecordQuery{From: from, To: to, Cursor: q.Cursor}, now)
	if err != nil {
		return WorkerRecordPage{}, err
	}
	rows, next, err := s.Reader.ListWorkerEvidence(ctx, tenantOf(p), worker, from, to, cursor, limit)
	if err != nil {
		return WorkerRecordPage{}, err
	}
	if len(rows) > limit {
		return WorkerRecordPage{}, ErrInvalidRequest
	}
	out := WorkerRecordPage{WorkerRef: worker, From: from, To: to, Records: make([]WorkerRecord, 0, len(rows))}
	for _, row := range rows {
		if row.EffectiveDate.Before(from) || !row.EffectiveDate.Before(to) || row.EvaluatedDate.Before(from) || !row.EvaluatedDate.Before(to) {
			return WorkerRecordPage{}, ErrInvalidRequest
		}
		record, err := projectEvidence(row, tenantOf(p), worker)
		if err != nil {
			return WorkerRecordPage{}, err
		}
		out.Records = append(out.Records, record)
	}
	sort.Slice(out.Records, func(i, j int) bool { return out.Records[i].ID < out.Records[j].ID })
	if next != "" {
		out.NextCursor, err = list.EncodeCursor(list.CursorPayload{Principal: p.Subject(), Tenant: tenantOf(p), Filter: recordFilter(from, to), Watermark: next, Version: 1, ExpiresAt: now.Add(list.CursorTTL).Unix(), Nonce: now.Format(time.RFC3339Nano)}, s.CursorKey)
		if err != nil {
			return WorkerRecordPage{}, fmt.Errorf("timecardservice: encode worker cursor: %w", err)
		}
	}
	return out, nil
}

// ReadOwnWorkerRecords is the descriptive alias for ReadOwnRecords.
func (s WorkerRecordService) ReadOwnWorkerRecords(ctx context.Context, p *trust.Principal, q WorkerRecordQuery) (WorkerRecordPage, error) {
	return s.ReadOwnRecords(ctx, p, q)
}

// ExportOwnRecords returns stable JSON bytes for the authenticated worker's
// complete selected window. It follows the same authorization and projection
// path as ReadOwnRecords and never exports raw or sensitive payloads.
func (s WorkerRecordService) ExportOwnRecords(ctx context.Context, p *trust.Principal, q WorkerRecordQuery) ([]byte, error) {
	q.Cursor = ""
	q.Limit = 100
	var records []WorkerRecord
	seen := make(map[string]struct{})
	for pages := 0; ; pages++ {
		if pages >= 10000 {
			return nil, ErrInvalidRequest
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := s.ReadOwnRecords(ctx, p, q)
		if err != nil {
			return nil, err
		}
		records = append(records, page.Records...)
		if page.NextCursor == "" {
			sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
			return json.Marshal(WorkerRecordExport{WorkerRef: page.WorkerRef, From: page.From, To: page.To, Records: records})
		}
		if _, ok := seen[page.NextCursor]; ok {
			return nil, ErrInvalidRequest
		}
		seen[page.NextCursor] = struct{}{}
		q.Cursor = page.NextCursor
	}
}

// ExportOwnWorkerRecords is the descriptive alias for ExportOwnRecords.
func (s WorkerRecordService) ExportOwnWorkerRecords(ctx context.Context, p *trust.Principal, q WorkerRecordQuery) ([]byte, error) {
	return s.ExportOwnRecords(ctx, p, q)
}

// IsSensitiveProjectionError reports a source payload refusal caused by
// forbidden evidence fields.
func IsSensitiveProjectionError(err error) bool { return errors.Is(err, ErrForbidden) }
