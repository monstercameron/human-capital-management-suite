package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// DestinationKind is the closed set of places approved time can be
// delivered to (WTIME-011, WTIME-012, TCLOCK-015).
type DestinationKind string

const (
	DestinationContractorInvoice DestinationKind = "CONTRACTOR_INVOICE"
	DestinationAgencyExport      DestinationKind = "AGENCY_EXPORT"
	DestinationPayrollExport     DestinationKind = "PAYROLL_EXPORT"
)

func validDestinationKind(k DestinationKind) bool {
	switch k {
	case DestinationContractorInvoice, DestinationAgencyExport, DestinationPayrollExport:
		return true
	}
	return false
}

// DestinationStatus is the record's current mutable status.
type DestinationStatus string

const (
	DestinationDraft    DestinationStatus = "DRAFT"
	DestinationSent     DestinationStatus = "SENT"
	DestinationAccepted DestinationStatus = "ACCEPTED"
	DestinationRejected DestinationStatus = "REJECTED"
)

// DestinationRecord is one contractor invoice draft, agency/VMS export
// record or payroll export record, pinned to the approved source revision
// it was built from.
type DestinationRecord struct {
	TenantID       string
	ID             string
	Kind           DestinationKind
	SourceRef      string
	SourceRevision int64
	ReceiverRef    string
	Status         DestinationStatus
	Revision       int64
	PayloadDigest  string
	Payload        json.RawMessage
	IdempotencyKey string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// DestinationReceipt is one append-only observation of the receiver's
// response.
type DestinationReceipt struct {
	TenantID      string
	ID            string
	DestinationID string
	Status        DestinationStatus // SENT, ACCEPTED or REJECTED
	ReceiverRef   string
	Reason        string
	ReceivedAt    time.Time
}

// CreateDestinationDraft writes a new destination record idempotently: the
// same (kind, source, source revision, idempotency key) always returns the
// original row, which is what keeps a re-export after a correction from
// duplicating hours at the receiver under the same key. A caller that wants
// a new export for a corrected revision passes the new SourceRevision,
// which is a different row entirely.
func (s *Store) CreateDestinationDraft(ctx context.Context, tenant string, r DestinationRecord) (DestinationRecord, error) {
	if tenant == "" || r.TenantID != tenant || r.ID == "" || !validDestinationKind(r.Kind) || r.SourceRef == "" ||
		r.SourceRevision <= 0 || r.PayloadDigest == "" || r.IdempotencyKey == "" {
		return DestinationRecord{}, ErrInvalid
	}
	payload := r.Payload
	if payload == nil {
		payload = json.RawMessage(`{}`)
	}
	var out DestinationRecord
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var existingID, existingDigest string
		err := tx.QueryRow(ctx, `SELECT id,payload_digest FROM destination_record WHERE tenant_id=$1 AND kind=$2 AND source_ref=$3 AND source_revision=$4 AND idempotency_key=$5`,
			tenant, string(r.Kind), r.SourceRef, r.SourceRevision, r.IdempotencyKey).Scan(&existingID, &existingDigest)
		if err == nil {
			if existingDigest != r.PayloadDigest {
				return ErrIdempotencyConflict
			}
			return s.loadDestinationLocked(ctx, tx, tenant, existingID, &out)
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO destination_record(tenant_id,id,kind,source_ref,source_revision,receiver_ref,status,revision,payload_digest,payload,idempotency_key) VALUES($1,$2,$3,$4,$5,$6,'DRAFT',1,$7,$8::jsonb,$9)`,
			tenant, r.ID, string(r.Kind), r.SourceRef, r.SourceRevision, r.ReceiverRef, r.PayloadDigest, []byte(payload), r.IdempotencyKey)
		if err != nil {
			return err
		}
		return s.loadDestinationLocked(ctx, tx, tenant, r.ID, &out)
	})
	if err != nil {
		return DestinationRecord{}, err
	}
	return out, nil
}

func (s *Store) loadDestinationLocked(ctx context.Context, tx dbport.Tx, tenant, id string, out *DestinationRecord) error {
	var status string
	var payload []byte
	row := tx.QueryRow(ctx, `SELECT tenant_id,id,kind,source_ref,source_revision,receiver_ref,status,revision,payload_digest,payload,idempotency_key,created_at,updated_at FROM destination_record WHERE tenant_id=$1 AND id=$2`, tenant, id)
	var kind string
	if err := row.Scan(&out.TenantID, &out.ID, &kind, &out.SourceRef, &out.SourceRevision, &out.ReceiverRef, &status, &out.Revision, &out.PayloadDigest, &payload, &out.IdempotencyKey, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	out.Kind = DestinationKind(kind)
	out.Status = DestinationStatus(status)
	out.Payload = append(json.RawMessage(nil), payload...)
	return nil
}

// GetDestinationRecord reads the current record.
func (s *Store) GetDestinationRecord(ctx context.Context, tenant, id string) (DestinationRecord, error) {
	if tenant == "" || id == "" {
		return DestinationRecord{}, ErrInvalid
	}
	var out DestinationRecord
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return s.loadDestinationLocked(ctx, tx, tenant, id, &out)
	})
	if err != nil {
		return DestinationRecord{}, err
	}
	return out, nil
}

// legalDestinationTransition is the closed set of status transitions a
// receipt may drive: a record can only move forward, and ACCEPTED and
// REJECTED are both terminal.
func legalDestinationTransition(from, to DestinationStatus) bool {
	switch from {
	case DestinationDraft:
		return to == DestinationSent
	case DestinationSent:
		return to == DestinationAccepted || to == DestinationRejected
	default:
		return false
	}
}

// RecordDestinationReceipt appends the receiver's observed response and
// advances the record's status when the transition is legal. Recording the
// same receipt (destination, status, receiver) twice is a no-op idempotent
// return of the current record, never a duplicate append or an error.
func (s *Store) RecordDestinationReceipt(ctx context.Context, tenant string, rec DestinationReceipt) (DestinationRecord, error) {
	if tenant == "" || rec.TenantID != tenant || rec.DestinationID == "" || rec.ReceiverRef == "" ||
		(rec.Status != DestinationSent && rec.Status != DestinationAccepted && rec.Status != DestinationRejected) {
		return DestinationRecord{}, ErrInvalid
	}
	var out DestinationRecord
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var current DestinationRecord
		if err := s.loadDestinationLocked(ctx, tx, tenant, rec.DestinationID, &current); err != nil {
			return err
		}
		if current.Status == rec.Status {
			out = current
			return nil
		}
		if !legalDestinationTransition(current.Status, rec.Status) {
			return ErrInvalid
		}
		tag, err := tx.Exec(ctx, `UPDATE destination_record SET status=$1,revision=revision+1,updated_at=now() WHERE tenant_id=$2 AND id=$3 AND revision=$4`,
			string(rec.Status), tenant, rec.DestinationID, current.Revision)
		if err != nil {
			return err
		}
		if tag != 1 {
			return ErrRevisionConflict
		}
		if _, err := tx.Exec(ctx, `INSERT INTO destination_receipt(tenant_id,id,destination_id,status,receiver_ref,reason) VALUES($1,$2,$3,$4,$5,$6)`,
			tenant, uuid.NewString(), rec.DestinationID, string(rec.Status), rec.ReceiverRef, rec.Reason); err != nil {
			return err
		}
		return s.loadDestinationLocked(ctx, tx, tenant, rec.DestinationID, &out)
	})
	if err != nil {
		return DestinationRecord{}, err
	}
	return out, nil
}

// DestinationReceipts returns the append-only receipt trail for id.
func (s *Store) DestinationReceipts(ctx context.Context, tenant, id string) ([]DestinationReceipt, error) {
	if tenant == "" || id == "" {
		return nil, ErrInvalid
	}
	out := make([]DestinationReceipt, 0)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id,id,destination_id,status,receiver_ref,reason,received_at FROM destination_receipt WHERE tenant_id=$1 AND destination_id=$2 ORDER BY received_at`, tenant, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r DestinationReceipt
			var status string
			if err := rows.Scan(&r.TenantID, &r.ID, &r.DestinationID, &status, &r.ReceiverRef, &r.Reason, &r.ReceivedAt); err != nil {
				return err
			}
			r.Status = DestinationStatus(status)
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
