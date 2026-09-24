// Package evidencestore is the durable execution and capability evidence
// store over capability_invocation_evidence (migration 00303, WF-RUN-035).
//
// It is the served composition's evidence sink: the capability gateway's
// invocation and refusal decisions, the execution-authority gate's decisions
// and the workflow driver's OBS-024 execution evidence are all recorded here,
// tenant-scoped under row-level security, so the chronology a journey's
// Inspect reads survives restart. Rows are append-only and digest-sealed: the
// evidence id is derived from the row's canonical digest, so re-recording
// the same decision replays the existing row instead of adding a second one,
// and every read re-derives the digest and refuses a row that no longer
// matches it.
package evidencestore

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

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Evidence id and execution-evidence packing. The packing mirrors
// internal/intent/app.ExecutionEvidenceOf exactly (this data package does not
// import the application layer); the composition test in
// internal/application proves the two agree on a journey read.
const (
	evidenceIDPrefix              = "ev:capability:"
	executionEvidenceCapabilityID = "workflow.execution.evidence"
)

var (
	// ErrInvalid reports a missing dependency or a malformed record.
	ErrInvalid = errors.New("evidencestore: invalid input")
	// ErrTenantRequired reports a record or read that names no tenant. The
	// store never guesses one.
	ErrTenantRequired = errors.New("evidencestore: a tenant is required")
	// ErrTenantMismatch reports a read whose tenant key and storage tenant
	// name different tenants.
	ErrTenantMismatch = errors.New("evidencestore: tenant key and storage tenant disagree")
	// ErrDigestMismatch reports a stored row whose content no longer matches
	// its recorded digest, or an evidence id already bound to other content.
	ErrDigestMismatch = errors.New("evidencestore: evidence digest mismatch")
)

// TenantIDs maps a tenant key to its storage identity. A served composition
// passes the same derivation its intent store's tenant table uses.
type TenantIDs func(values.TenantId) uuid.UUID

// Store is the PostgreSQL evidence store.
type Store struct {
	db        dbport.Beginner
	tenantIDs TenantIDs
}

// New builds a Store over db. tenantIDs maps a capability decision's tenant
// key to the storage tenant its row is scoped to.
func New(db dbport.Beginner, tenantIDs TenantIDs) *Store {
	return &Store{db: db, tenantIDs: tenantIDs}
}

// Record is one stored evidence row, read back and digest-verified.
type Record struct {
	TenantID          uuid.UUID
	EvidenceID        string
	Sequence          int64
	CapabilityID      string
	CapabilityVersion uint32
	SubjectRef        string
	Decision          string
	ReasonCode        string
	OccurredAt        time.Time
	Purpose           string
	IdempotencyKey    string
	Deadline          time.Time
	EffectClass       string
	Digest            string
}

// RecordInvocation implements capability.EvidenceSink. The row is scoped to
// the tenant key evt carries; a record without one is refused.
func (s *Store) RecordInvocation(ctx context.Context, evt capability.InvocationEvidence) (string, error) {
	return s.RecordInvocationTx(ctx, evt)
}

// RecordInvocationTx is the gateway's transaction-aware invocation path. If
// ctx carries a caller-owned transaction, the evidence joins it and its tenant
// scope; this method never commits or re-scopes that transaction. Outside a
// caller transaction it preserves the standalone recording contract.
func (s *Store) RecordInvocationTx(ctx context.Context, evt capability.InvocationEvidence) (string, error) {
	if strings.TrimSpace(evt.Tenant) == "" {
		return "", fmt.Errorf("%w: capability evidence for %s names no tenant", ErrTenantRequired, evt.CapabilityID)
	}
	if s == nil || s.tenantIDs == nil {
		return "", fmt.Errorf("%w: tenant mapping is required", ErrInvalid)
	}
	tenantID := s.tenantIDs(values.TenantId(evt.Tenant))
	if tx, ok := dbport.TxFromContext(ctx); ok {
		return insertTx(ctx, tx, tenantID, recordOf(evt))
	}
	return s.insert(ctx, tenantID, recordOf(evt))
}

// RecordExecutionEvidence implements the workflow driver's OBS-024 port
// (internal/workflow/execute.ExecutionEvidence) for the storage tenant the
// run committed under.
func (s *Store) RecordExecutionEvidence(ctx context.Context, tenantID uuid.UUID, kind, instanceID, nodeID, refID, digest string, occurredAt time.Time) (string, error) {
	return s.insert(ctx, tenantID, executionRecord(kind, instanceID, nodeID, refID, digest, occurredAt))
}

// RecordExecutionEvidenceTx records the same OBS-024 entry on the caller's
// transaction, for the workflow driver's in-advance recording
// (internal/workflow/execute.ExecutionEvidenceTx): the entry commits beside
// the outcome it describes and rolls back with it. tx must already be scoped
// to tenantID the way the driver's advance transaction is; this method
// neither re-scopes nor commits it.
func (s *Store) RecordExecutionEvidenceTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, kind, instanceID, nodeID, refID, digest string, occurredAt time.Time) (string, error) {
	if tx == nil {
		return "", fmt.Errorf("%w: execution evidence needs the advance transaction", ErrInvalid)
	}
	return insertTx(ctx, tx, tenantID, executionRecord(kind, instanceID, nodeID, refID, digest, occurredAt))
}

func executionRecord(kind, instanceID, nodeID, refID, digest string, occurredAt time.Time) Record {
	return Record{
		CapabilityID: executionEvidenceCapabilityID, CapabilityVersion: 1,
		SubjectRef: instanceID + "|" + nodeID, Decision: kind, ReasonCode: refID + "|" + digest,
		OccurredAt: occurredAt,
	}
}

// JourneyEvidenceIDs returns, in recording order, the ids of tenant's
// evidence whose subject is intentID, instanceID or a node of instanceID.
// tenantID, when not uuid.Nil, must be the storage identity of tenant.
func (s *Store) JourneyEvidenceIDs(ctx context.Context, tenant values.TenantId, tenantID uuid.UUID, intentID, instanceID string) ([]string, error) {
	storage, err := s.tenantOf(tenant, tenantID)
	if err != nil {
		return nil, err
	}
	if intentID == "" && instanceID == "" {
		return nil, nil
	}
	var ids []string
	err = s.tx(ctx, storage, func(tx dbport.Tx) error {
		records, err := query(ctx, tx, `WHERE tenant_id = $1 AND (
				($2::text <> '' AND subject_ref = $2::text) OR
				($3::text <> '' AND (subject_ref = $3::text OR starts_with(subject_ref, $3::text || '|'))))
			ORDER BY record_seq`, storage, intentID, instanceID)
		if err != nil {
			return err
		}
		for _, r := range records {
			ids = append(ids, r.EvidenceID)
		}
		return nil
	})
	return ids, err
}

// List returns every evidence row recorded for tenant, in recording order,
// each digest-verified.
func (s *Store) List(ctx context.Context, tenant values.TenantId) ([]Record, error) {
	storage, err := s.tenantOf(tenant, uuid.Nil)
	if err != nil {
		return nil, err
	}
	var out []Record
	err = s.tx(ctx, storage, func(tx dbport.Tx) error {
		var qErr error
		out, qErr = query(ctx, tx, `WHERE tenant_id = $1 ORDER BY record_seq`, storage)
		return qErr
	})
	return out, err
}

func (s *Store) tenantOf(tenant values.TenantId, tenantID uuid.UUID) (uuid.UUID, error) {
	if strings.TrimSpace(string(tenant)) == "" {
		return uuid.Nil, ErrTenantRequired
	}
	if s == nil || s.tenantIDs == nil {
		return uuid.Nil, fmt.Errorf("%w: tenant mapping is required", ErrInvalid)
	}
	storage := s.tenantIDs(tenant)
	if tenantID != uuid.Nil && tenantID != storage {
		return uuid.Nil, fmt.Errorf("%w: %s", ErrTenantMismatch, tenant)
	}
	return storage, nil
}

func (s *Store) tx(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("%w: database is required", ErrInvalid)
	}
	if tenantID == uuid.Nil {
		return ErrTenantRequired
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("evidencestore: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("evidencestore: commit: %w", err)
	}
	return nil
}

func (s *Store) insert(ctx context.Context, tenantID uuid.UUID, r Record) (string, error) {
	var id string
	err := s.tx(ctx, tenantID, func(tx dbport.Tx) error {
		var err error
		id, err = insertTx(ctx, tx, tenantID, r)
		return err
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// insertTx writes r on tx, which the caller owns: it must already be scoped
// to tenantID, and it is committed or rolled back by the caller, never here.
func insertTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, r Record) (string, error) {
	if tenantID == uuid.Nil {
		return "", ErrTenantRequired
	}
	if r.CapabilityID == "" || r.CapabilityVersion == 0 || r.Decision == "" || r.OccurredAt.IsZero() {
		return "", fmt.Errorf("%w: evidence needs a capability, version, decision and instant", ErrInvalid)
	}
	r.TenantID = tenantID
	r = normalize(r)
	r.Digest = digestOf(r)
	r.EvidenceID = evidenceIDPrefix + strings.TrimPrefix(r.Digest, "sha256:")[:24]
	var deadline any
	if !r.Deadline.IsZero() {
		deadline = r.Deadline
	}
	inserted, err := tx.Exec(ctx, `INSERT INTO capability_invocation_evidence
		(tenant_id, evidence_id, capability_id, capability_version, subject_ref, decision, reason_code,
		 occurred_at, purpose, idempotency_key, deadline, effect_class, record_digest)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (tenant_id, evidence_id) DO NOTHING`,
		tenantID, r.EvidenceID, r.CapabilityID, int64(r.CapabilityVersion), r.SubjectRef, r.Decision, r.ReasonCode,
		r.OccurredAt, r.Purpose, r.IdempotencyKey, deadline, r.EffectClass, r.Digest)
	if err != nil {
		return "", fmt.Errorf("evidencestore: insert evidence: %w", err)
	}
	if inserted == 1 {
		return r.EvidenceID, nil
	}
	// A replay of the same decision: the existing row must be this one.
	var stored string
	if err := tx.QueryRow(ctx, `SELECT record_digest FROM capability_invocation_evidence WHERE tenant_id = $1 AND evidence_id = $2`,
		tenantID, r.EvidenceID).Scan(&stored); err != nil {
		return "", fmt.Errorf("evidencestore: load replayed evidence: %w", err)
	}
	if stored != r.Digest {
		return "", fmt.Errorf("%w: %s is already bound to %s", ErrDigestMismatch, r.EvidenceID, stored)
	}
	return r.EvidenceID, nil
}

const selectColumns = `SELECT tenant_id, evidence_id, record_seq, capability_id, capability_version, subject_ref, decision,
	reason_code, occurred_at, purpose, idempotency_key, deadline, effect_class, record_digest
	FROM capability_invocation_evidence `

func query(ctx context.Context, tx dbport.Tx, where string, args ...any) ([]Record, error) {
	rows, err := tx.Query(ctx, selectColumns+where, args...)
	if err != nil {
		return nil, fmt.Errorf("evidencestore: query evidence: %w", err)
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		var version int64
		var deadline *time.Time
		if err := rows.Scan(&r.TenantID, &r.EvidenceID, &r.Sequence, &r.CapabilityID, &version, &r.SubjectRef, &r.Decision,
			&r.ReasonCode, &r.OccurredAt, &r.Purpose, &r.IdempotencyKey, &deadline, &r.EffectClass, &r.Digest); err != nil {
			return nil, fmt.Errorf("evidencestore: scan evidence: %w", err)
		}
		r.CapabilityVersion = uint32(version)
		if deadline != nil {
			r.Deadline = *deadline
		}
		r = normalize(r)
		if got := digestOf(r); got != r.Digest {
			return nil, fmt.Errorf("%w: stored row %s re-derives %s", ErrDigestMismatch, r.EvidenceID, got)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evidencestore: read evidence: %w", err)
	}
	return out, nil
}

func recordOf(evt capability.InvocationEvidence) Record {
	return Record{
		CapabilityID: evt.CapabilityID, CapabilityVersion: evt.CapabilityVersion, SubjectRef: evt.SubjectRef,
		Decision: evt.Decision, ReasonCode: evt.ReasonCode, OccurredAt: evt.OccurredAt, Purpose: evt.Purpose,
		IdempotencyKey: evt.IdempotencyKey, Deadline: evt.Deadline, EffectClass: string(evt.EffectClass),
	}
}

// normalize puts instants in the precision and zone PostgreSQL stores, so
// the digest computed before the insert is the digest re-derived on read.
func normalize(r Record) Record {
	r.OccurredAt = r.OccurredAt.UTC().Truncate(time.Microsecond)
	if !r.Deadline.IsZero() {
		r.Deadline = r.Deadline.UTC().Truncate(time.Microsecond)
	}
	return r
}

// digestOf is the canonical sha256 of one row's content, tenant included.
func digestOf(r Record) string {
	deadline := ""
	if !r.Deadline.IsZero() {
		deadline = r.Deadline.Format(time.RFC3339Nano)
	}
	body, _ := json.Marshal(struct {
		Tenant            string `json:"tenant_id"`
		CapabilityID      string `json:"capability_id"`
		CapabilityVersion uint32 `json:"capability_version"`
		SubjectRef        string `json:"subject_ref"`
		Decision          string `json:"decision"`
		ReasonCode        string `json:"reason_code"`
		OccurredAt        string `json:"occurred_at"`
		Purpose           string `json:"purpose"`
		IdempotencyKey    string `json:"idempotency_key"`
		Deadline          string `json:"deadline"`
		EffectClass       string `json:"effect_class"`
	}{
		r.TenantID.String(), r.CapabilityID, r.CapabilityVersion, r.SubjectRef, r.Decision, r.ReasonCode,
		r.OccurredAt.Format(time.RFC3339Nano), r.Purpose, r.IdempotencyKey, deadline, r.EffectClass,
	})
	sum := sha256.Sum256(append([]byte("hcmnext.capability_invocation_evidence.v1\n"), body...))
	return "sha256:" + hex.EncodeToString(sum[:])
}
