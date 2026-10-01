package agentcandidateevalstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// ModelCall records normalized observations directly from the real candidate
// provider dispatcher, after authoritative budget settlement. No prompts or
// provider response bytes are retained here.
type ModelCall struct {
	Target                                               agenteval.PersonaEvaluationTarget
	TaskID, StepID, RequestDigest, ResultDigest, LeaseID string
	Provider                                             agentmodel.ModelIdentity
	Usage                                                agentmodel.ModelUsage
	ActionPolicy                                         *agentmodel.RequestedActionPolicy
	RequestedActions                                     []agentmodel.RequestedAction
	CompletedAt                                          time.Time
	EvidenceDigest                                       string
}

func (s *Store) AppendModelCall(ctx context.Context, call ModelCall) error {
	if s == nil || s.db == nil || s.tenantUUID == nil || ctx == nil || validateModelCall(call) != nil {
		return ErrEvidence
	}
	tenant, production := s.tenantUUID(call.Target.SyntheticTenantID), s.tenantUUID(call.Target.TenantID)
	if tenant == uuid.Nil || production == uuid.Nil || tenant == production {
		return ErrEvidence
	}
	call.CompletedAt = call.CompletedAt.UTC().Truncate(time.Microsecond)
	call.EvidenceDigest = modelCallDigest(call)
	encoded, err := json.Marshal(call)
	if err != nil {
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
	if _, err := tx.Exec(ctx, `INSERT INTO persona_candidate_model_calls(tenant_id,task_id,step_id,evidence_digest,record,completed_at) VALUES($1,$2,$3,$4,$5::jsonb,$6) ON CONFLICT(tenant_id,task_id,step_id) DO NOTHING`, tenant, call.TaskID, call.StepID, call.EvidenceDigest, encoded, call.CompletedAt); err != nil {
		return err
	}
	var retained string
	if err := tx.QueryRow(ctx, `SELECT evidence_digest FROM persona_candidate_model_calls WHERE tenant_id=$1 AND task_id=$2 AND step_id=$3`, tenant, call.TaskID, call.StepID).Scan(&retained); err != nil {
		return err
	}
	if retained != call.EvidenceDigest {
		return ErrEvidence
	}
	return tx.Commit(ctx)
}

func (s *Store) ReadModelCalls(ctx context.Context, target agenteval.PersonaEvaluationTarget, task string) ([]ModelCall, error) {
	if s == nil || s.db == nil || s.tenantUUID == nil || ctx == nil || task == "" {
		return nil, ErrEvidence
	}
	tenant, production := s.tenantUUID(target.SyntheticTenantID), s.tenantUUID(target.TenantID)
	if tenant == uuid.Nil || production == uuid.Nil || tenant == production {
		return nil, ErrEvidence
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT record FROM persona_candidate_model_calls WHERE tenant_id=$1 AND task_id=$2 ORDER BY step_id COLLATE "C"`, tenant, task)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ModelCall{}
	for rows.Next() {
		var encoded []byte
		if err := rows.Scan(&encoded); err != nil {
			return nil, err
		}
		var call ModelCall
		if json.Unmarshal(encoded, &call) != nil || validateModelCall(call) != nil || call.Target != target || call.TaskID != task || call.EvidenceDigest != modelCallDigest(call) {
			return nil, ErrEvidence
		}
		result = append(result, call)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func validateModelCall(call ModelCall) error {
	if call.Target.TenantID == "" || call.Target.SyntheticTenantID == "" || call.Target.TenantID == call.Target.SyntheticTenantID || call.Target.PersonaID == "" || call.Target.PersonaVersion <= 0 || call.Target.InvokerID == "" || !isDigest(call.Target.ProfileDigest) || !isDigest(call.Target.ModelDigest) || !isDigest(call.RequestDigest) || !isDigest(call.ResultDigest) || call.TaskID == "" || call.StepID == "" || call.LeaseID == "" || call.CompletedAt.IsZero() || call.Provider.ProviderID == "" || call.Provider.ModelID == "" || call.Provider.Version == "" || call.Usage.InputTokens < 0 || call.Usage.OutputTokens < 0 || call.Usage.TotalTokens < 0 || call.Usage.CostMicros < 0 || call.Usage.CachedInputTokens < 0 || call.Usage.CachedInputTokens > call.Usage.InputTokens || call.Usage.InputTokens+call.Usage.OutputTokens != call.Usage.TotalTokens {
		return ErrEvidence
	}
	if call.ActionPolicy != nil && (call.ActionPolicy.ProfileDigest != call.Target.ProfileDigest || agentmodel.ValidateRequestedActionPolicy(*call.ActionPolicy) != nil) {
		return ErrEvidence
	}
	return nil
}

func modelCallDigest(call ModelCall) string {
	call.EvidenceDigest = ""
	encoded, _ := json.Marshal(call)
	sum := sha256.Sum256(append([]byte("hcm-next-persona-candidate-model-call/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}
