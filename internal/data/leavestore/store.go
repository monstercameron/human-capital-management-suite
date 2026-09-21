// Package leavestore materializes the Medical Leave and Return-to-Work
// domain state (DB-023): tenant-scoped typed repositories for leave
// requests and records, program eligibility, entitlement segments,
// absence links, availability revisions, balance postings, evidence
// references, work restrictions, obligations, intent links and the
// restricted medical compartment. Revision tables are append-only:
// state advances through new revision rows under compare-and-swap, and
// overlapping incompatible active leave, orphan children, stale heads,
// double posts and employment inactivation all refuse.
package leavestore

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrConflict reports a compare-and-swap or uniqueness conflict.
var ErrConflict = errors.New("leavestore: conflict")

// ErrForbidden reports a role-gated refusal.
var ErrForbidden = errors.New("leavestore: forbidden")

// Store is the stateless typed repository; every method takes its
// transaction from the caller so leave commits join the owning business
// transaction.
type Store struct{}

// New starts the store.
func New() Store { return Store{} }

// Request is one leave request revision.
type Request struct {
	RequestID      uuid.UUID
	Revision       int64
	State          string
	ProposalDigest string
	IdempotencyKey string
}

// CreateRequest inserts one request revision. Repeated idempotency keys
// return the existing row instead of a second request.
func (s Store) CreateRequest(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, request Request) (existed bool, err error) {
	var revision int64
	err = tx.QueryRow(ctx, `INSERT INTO leave_request
		(tenant_id, request_id, revision, state, employment_state, proposal_digest, idempotency_key)
		VALUES ($1,$2,$3,$4,'ACTIVE',$5,$6)
		ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
		RETURNING revision`,
		tenant, request.RequestID, request.Revision, request.State, request.ProposalDigest, request.IdempotencyKey).Scan(&revision)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return true, nil
		}
		return false, fmt.Errorf("leavestore: create request: %w", err)
	}
	return false, nil
}

// Record is one leave record revision.
type Record struct {
	RecordID        uuid.UUID
	RequestID       uuid.UUID
	RequestRevision int64
	Revision        int64
	State           string
	ProposalDigest  string
}

// AppendRecord appends one record revision under compare-and-swap: the
// revision must follow the current head exactly, or the append is stale.
func (s Store) AppendRecord(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, record Record) error {
	var head int64
	err := tx.QueryRow(ctx, `SELECT COALESCE(max(revision),0) FROM leave_record
		WHERE tenant_id=$1 AND record_id=$2`, tenant, record.RecordID).Scan(&head)
	if err != nil {
		return fmt.Errorf("leavestore: record head: %w", err)
	}
	if record.Revision != head+1 {
		return fmt.Errorf("leavestore: stale record revision: have %d want %d: %w", record.Revision, head+1, ErrConflict)
	}
	_, err = tx.Exec(ctx, `INSERT INTO leave_record
		(tenant_id, record_id, request_id, request_revision, revision, state, employment_state, proposal_digest)
		VALUES ($1,$2,$3,$4,$5,$6,'ACTIVE',$7)`,
		tenant, record.RecordID, record.RequestID, record.RequestRevision, record.Revision, record.State, record.ProposalDigest)
	if err != nil {
		return fmt.Errorf("leavestore: append record: %w", err)
	}
	return nil
}

// Eligibility is one program eligibility row.
type Eligibility struct {
	ProgramID   string
	Authority   string
	Result      string
	RuleID      string
	RuleVersion string
	Digest      string
}

// PutEligibility records one program eligibility outcome for a record revision.
func (s Store) PutEligibility(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, recordID uuid.UUID, recordRevision int64, eligibility Eligibility) error {
	_, err := tx.Exec(ctx, `INSERT INTO leave_program_eligibility
		(tenant_id, record_id, record_revision, program_id, authority, result, rule_id, rule_version, digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		tenant, recordID, recordRevision, eligibility.ProgramID, eligibility.Authority, eligibility.Result, eligibility.RuleID, eligibility.RuleVersion, eligibility.Digest)
	if err != nil {
		return fmt.Errorf("leavestore: put eligibility: %w", err)
	}
	return nil
}

// Segment is one entitlement plan segment.
type Segment struct {
	StartDay int
	EndDay   int
	Kind     string
	Hours    int
	Programs []string
}

// PutSegments records the tiling entitlement segments for a record revision.
func (s Store) PutSegments(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, recordID uuid.UUID, recordRevision int64, segments []Segment) error {
	statements := make([]dbport.Statement, 0, len(segments))
	for _, segment := range segments {
		statements = append(statements, dbport.Statement{SQL: `INSERT INTO leave_entitlement_segment
			(tenant_id, record_id, record_revision, start_day, end_day, kind, hours, programs)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			Args: []any{tenant, recordID, recordRevision, segment.StartDay, segment.EndDay, segment.Kind, segment.Hours, segment.Programs},
		})
	}
	if _, err := dbport.ExecAll(ctx, tx, statements); err != nil {
		return fmt.Errorf("leavestore: put segment: %w", err)
	}
	return nil
}

// LinkAbsence binds the active absence relationship.
func (s Store) LinkAbsence(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, recordID uuid.UUID, recordRevision int64, absenceID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO leave_absence_link
		(tenant_id, record_id, record_revision, absence_id) VALUES ($1,$2,$3,$4)`,
		tenant, recordID, recordRevision, absenceID)
	if err != nil {
		return fmt.Errorf("leavestore: link absence: %w", err)
	}
	return nil
}

// Availability is one availability successor revision.
type Availability struct {
	RevisionID     uuid.UUID
	WorkerRef      string
	PriorRevision  *uuid.UUID
	State          string
	Restored       bool
	IntentRef      string
	ProposalDigest string
	Reason         string
	LeaveEventID   string
	EffectiveStart int
	EffectiveEnd   int
	Digest         string
}

// AppendAvailability appends one availability successor: overlapping
// incompatible intervals refuse under lock, and duplicate leave events
// return the existing revision instead of a second row.
func (s Store) AppendAvailability(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, availability Availability) (existed bool, err error) {
	var clashes int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM (
			SELECT 1 FROM leave_availability_revision
			WHERE tenant_id=$1 AND worker_ref=$2 AND state<>$3
			AND effective_start<=$4 AND $5<=effective_end
			FOR UPDATE
		) clash`, tenant, availability.WorkerRef, availability.State, availability.EffectiveEnd, availability.EffectiveStart).Scan(&clashes)
	if err != nil {
		return false, fmt.Errorf("leavestore: overlap check: %w", err)
	}
	if clashes > 0 {
		return false, fmt.Errorf("leavestore: overlapping incompatible active leave: %w", ErrConflict)
	}
	var revisionID uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO leave_availability_revision
		(tenant_id, revision_id, worker_ref, prior_revision, state, restored, intent_ref, proposal_digest, reason, leave_event_id, effective_start, effective_end, digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (tenant_id, leave_event_id) DO NOTHING
		RETURNING revision_id`,
		tenant, uuid.New(), availability.WorkerRef, availability.PriorRevision, availability.State, availability.Restored,
		availability.IntentRef, availability.ProposalDigest, availability.Reason, availability.LeaveEventID,
		availability.EffectiveStart, availability.EffectiveEnd, availability.Digest).Scan(&revisionID)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return true, nil
		}
		return false, fmt.Errorf("leavestore: append availability: %w", err)
	}
	return false, nil
}

// BalancePosting is one exact balance posting: numerics carry the full
// kernel decimal, never float.
type BalancePosting struct {
	AccountRef     string
	Opening        string
	Ending         string
	Remainder      string
	ReceiptDigest  string
	IdempotencyKey string
}

// PostBalance records one posting once per idempotency key: repeats
// return done=false with no second row, so double debits cannot land.
func (s Store) PostBalance(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, recordID uuid.UUID, recordRevision int64, posting BalancePosting) (done bool, err error) {
	var one int
	err = tx.QueryRow(ctx, `INSERT INTO leave_balance_posting
		(tenant_id, record_id, record_revision, account_ref, opening, ending, remainder, receipt_digest, idempotency_key)
		VALUES ($1,$2,$3,$4,$5::numeric,$6::numeric,$7::numeric,$8,$9)
		ON CONFLICT (tenant_id, record_id, idempotency_key) DO NOTHING
		RETURNING 1`,
		tenant, recordID, recordRevision, posting.AccountRef, posting.Opening, posting.Ending, posting.Remainder, posting.ReceiptDigest, posting.IdempotencyKey).Scan(&one)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("leavestore: post balance: %w", err)
	}
	return true, nil
}

// EvidenceRef is one governed evidence reference.
type EvidenceRef struct {
	Ref              string
	Compartment      string
	Quarantined      bool
	Classified       bool
	AuthorityCurrent bool
}

// PutEvidenceRef records one governed reference for a record revision.
func (s Store) PutEvidenceRef(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, recordID uuid.UUID, recordRevision int64, ref EvidenceRef) error {
	_, err := tx.Exec(ctx, `INSERT INTO leave_evidence_ref
		(tenant_id, record_id, record_revision, ref, compartment, quarantined, classified, authority_current)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		tenant, recordID, recordRevision, ref.Ref, ref.Compartment, ref.Quarantined, ref.Classified, ref.AuthorityCurrent)
	if err != nil {
		return fmt.Errorf("leavestore: put evidence ref: %w", err)
	}
	return nil
}

// PutRestriction records one work restriction with its readiness revision.
func (s Store) PutRestriction(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, recordID uuid.UUID, recordRevision int64, restriction, readinessRef string) error {
	_, err := tx.Exec(ctx, `INSERT INTO leave_work_restriction
		(tenant_id, record_id, record_revision, restriction, readiness_ref) VALUES ($1,$2,$3,$4,$5)`,
		tenant, recordID, recordRevision, restriction, readinessRef)
	if err != nil {
		return fmt.Errorf("leavestore: put restriction: %w", err)
	}
	return nil
}

// PutObligation records one obligation state for a record revision.
func (s Store) PutObligation(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, recordID uuid.UUID, recordRevision int64, obligation, state string) error {
	_, err := tx.Exec(ctx, `INSERT INTO leave_obligation
		(tenant_id, record_id, record_revision, obligation, state) VALUES ($1,$2,$3,$4,$5)`,
		tenant, recordID, recordRevision, obligation, state)
	if err != nil {
		return fmt.Errorf("leavestore: put obligation: %w", err)
	}
	return nil
}

// LinkIntent binds one related intent digest to a record revision.
func (s Store) LinkIntent(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, recordID uuid.UUID, recordRevision int64, intentDigest, kind string) error {
	_, err := tx.Exec(ctx, `INSERT INTO leave_intent_link
		(tenant_id, record_id, record_revision, intent_digest, kind) VALUES ($1,$2,$3,$4,$5)`,
		tenant, recordID, recordRevision, intentDigest, kind)
	if err != nil {
		return fmt.Errorf("leavestore: link intent: %w", err)
	}
	return nil
}

// PutMedicalDetail files one restricted medical reference. The store is
// the sole writer path and records only references, never content.
func (s Store) PutMedicalDetail(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, recordID uuid.UUID, recordRevision int64, detailRef string) error {
	_, err := tx.Exec(ctx, `INSERT INTO leave_medical_detail
		(tenant_id, record_id, record_revision, detail_ref) VALUES ($1,$2,$3,$4)`,
		tenant, recordID, recordRevision, detailRef)
	if err != nil {
		return fmt.Errorf("leavestore: put medical detail: %w", err)
	}
	return nil
}

// MedicalDetail is one restricted reference returned only to the leave
// administrator role: unrestricted medical metadata has no reader path.
func (s Store) MedicalDetail(ctx context.Context, q dbport.Querier, tenant uuid.UUID, recordID uuid.UUID, role string) ([]string, error) {
	if role != "leave-administrator" {
		return nil, fmt.Errorf("leavestore: medical detail requires the leave-administrator role: %w", ErrForbidden)
	}
	rows, err := q.Query(ctx, `SELECT detail_ref FROM leave_medical_detail
		WHERE tenant_id=$1 AND record_id=$2 ORDER BY detail_ref`, tenant, recordID)
	if err != nil {
		return nil, fmt.Errorf("leavestore: medical detail: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, fmt.Errorf("leavestore: medical detail scan: %w", err)
		}
		out = append(out, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("leavestore: medical detail rows: %w", err)
	}
	return out, nil
}

// CountRevisions reports how many rows one revision table holds for a record.
func (s Store) CountRevisions(ctx context.Context, q dbport.Querier, tenant uuid.UUID, table string, recordID uuid.UUID) (int, error) {
	allowed := map[string]bool{
		"leave_record": true, "leave_program_eligibility": true, "leave_entitlement_segment": true,
		"leave_absence_link": true, "leave_evidence_ref": true, "leave_work_restriction": true,
		"leave_obligation": true, "leave_intent_link": true,
		"leave_medical_detail": true, "leave_balance_posting": true,
	}
	if !allowed[table] {
		return 0, fmt.Errorf("leavestore: cannot count %s", table)
	}
	var count int
	idColumn := "record_id"
	if err := q.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE tenant_id=$1 AND `+idColumn+`=$2`, tenant, recordID).Scan(&count); err != nil {
		return 0, fmt.Errorf("leavestore: count: %w", err)
	}
	return count, nil
}
