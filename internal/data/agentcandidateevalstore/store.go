// Package agentcandidateevalstore retains immutable synthetic runtime case
// checkpoints. It stores observations, never caller-selected evaluation passes.
package agentcandidateevalstore

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
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

var ErrEvidence = errors.New("agentcandidateevalstore: runtime evidence unavailable")

// Record is written by the candidate runtime after owner services have
// committed their observations. All sensitive content stays in owner stores.
type Record struct {
	Target                                       agenteval.PersonaEvaluationTarget
	CaseDigest, RequestDigest                    string
	InvocationID, TaskID, AdmissionDigest        string
	Outcome, RefusalCode, RefusalPointer         string
	Skills, DeliveredTo                          []string
	PlanDigest, BaselineInvocationID             string
	OutputDigest, DeliveryDigest, EvidenceDigest string
	CompletedAt                                  time.Time
}

type Store struct {
	db         dbport.Beginner
	tenantUUID func(string) uuid.UUID
}

func New(db dbport.Beginner) (*Store, error) {
	return NewWithTenantUUID(db, func(tenant string) uuid.UUID { id, _ := uuid.Parse(tenant); return id })
}

// NewWithTenantUUID preserves logical tenant keys while binding both physical
// database scopes to their canonical, distinct storage UUIDs.
func NewWithTenantUUID(db dbport.Beginner, mapper func(string) uuid.UUID) (*Store, error) {
	if db == nil || mapper == nil {
		return nil, ErrEvidence
	}
	return &Store{db: db, tenantUUID: mapper}, nil
}

// Append is idempotent only for the same complete immutable observation.
// The dedicated hcmnext_agent_eval_runtime role owns this write privilege.
func (s *Store) Append(ctx context.Context, record Record) error {
	if s == nil || s.db == nil || s.tenantUUID == nil || ctx == nil || validate(record) != nil {
		return ErrEvidence
	}
	record.CompletedAt = record.CompletedAt.UTC().Truncate(time.Microsecond)
	record.EvidenceDigest = digest(record)
	encoded, err := json.Marshal(record)
	if err != nil {
		return ErrEvidence
	}
	tenant, production := s.tenantUUID(record.Target.SyntheticTenantID), s.tenantUUID(record.Target.TenantID)
	if tenant == uuid.Nil || production == uuid.Nil || tenant == production {
		return ErrEvidence
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO persona_candidate_case_journal
		(tenant_id,invocation_id,production_tenant_id,persona_id,persona_version,profile_digest,model_digest,case_digest,evidence_digest,record,completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11) ON CONFLICT (tenant_id,invocation_id) DO NOTHING`,
		tenant, record.InvocationID, production, record.Target.PersonaID, record.Target.PersonaVersion,
		record.Target.ProfileDigest, record.Target.ModelDigest, record.CaseDigest, record.EvidenceDigest, encoded, record.CompletedAt)
	if err != nil {
		return err
	}
	var retained string
	if err := tx.QueryRow(ctx, `SELECT evidence_digest FROM persona_candidate_case_journal WHERE tenant_id=$1 AND invocation_id=$2`, tenant, record.InvocationID).Scan(&retained); err != nil {
		return err
	}
	if retained != record.EvidenceDigest {
		return ErrEvidence
	}
	return tx.Commit(ctx)
}

// Read scopes both SQL and the decoded observation to the fixed target.
func (s *Store) Read(ctx context.Context, target agenteval.PersonaEvaluationTarget, invocationID string) (Record, error) {
	if s == nil || s.db == nil || s.tenantUUID == nil || ctx == nil || strings.TrimSpace(invocationID) == "" {
		return Record{}, ErrEvidence
	}
	tenant, production := s.tenantUUID(target.SyntheticTenantID), s.tenantUUID(target.TenantID)
	if tenant == uuid.Nil || production == uuid.Nil || tenant == production {
		return Record{}, ErrEvidence
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return Record{}, err
	}
	var encoded []byte
	if err := tx.QueryRow(ctx, `SELECT record FROM persona_candidate_case_journal WHERE tenant_id=$1 AND invocation_id=$2`, tenant, invocationID).Scan(&encoded); err != nil {
		return Record{}, fmt.Errorf("%w: read checkpoint", ErrEvidence)
	}
	var record Record
	if json.Unmarshal(encoded, &record) != nil || validate(record) != nil || record.Target != target || record.InvocationID != invocationID ||
		!isDigest(record.EvidenceDigest) || record.EvidenceDigest != digest(record) {
		return Record{}, ErrEvidence
	}
	if err := tx.Commit(ctx); err != nil {
		return Record{}, err
	}
	return record, nil
}

func validate(record Record) error {
	if record.Target.TenantID == record.Target.SyntheticTenantID || record.Target.PersonaVersion <= 0 ||
		!isDigest(record.Target.ProfileDigest) || !isDigest(record.Target.ModelDigest) || !isDigest(record.CaseDigest) || !isDigest(record.RequestDigest) || record.CompletedAt.IsZero() {
		return ErrEvidence
	}
	for _, value := range []string{record.Target.TenantID, record.Target.SyntheticTenantID, record.Target.PersonaID, record.Target.InvokerID, record.InvocationID} {
		if value == "" || value != strings.TrimSpace(value) {
			return ErrEvidence
		}
	}
	switch record.Outcome {
	case "COMPLETED":
		if record.TaskID == "" || record.RefusalCode != "" || !isDigest(record.AdmissionDigest) || !isDigest(record.PlanDigest) ||
			!isDigest(record.OutputDigest) || !isDigest(record.DeliveryDigest) || len(record.DeliveredTo) == 0 {
			return ErrEvidence
		}
	case "REFUSED":
		if record.RefusalCode == "" || len(record.Skills) != 0 || len(record.DeliveredTo) != 0 || record.OutputDigest != "" || record.DeliveryDigest != "" {
			return ErrEvidence
		}
	case "FAILED":
		if record.TaskID == "" || record.RefusalCode == "" || !isDigest(record.AdmissionDigest) {
			return ErrEvidence
		}
	default:
		return ErrEvidence
	}
	return nil
}

func digest(record Record) string {
	record.EvidenceDigest = ""
	encoded, _ := json.Marshal(record)
	sum := sha256.Sum256(append([]byte("hcm-next-persona-candidate-case/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func isDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(value[7:])
	return err == nil && strings.ToLower(value) == value
}
