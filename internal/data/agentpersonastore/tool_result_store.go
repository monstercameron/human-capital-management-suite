package agentpersonastore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// ToolResultRecord retains locally executed tool output with its complete
// accepted run identity. Provider responses never enter this journal.
type ToolResultRecord struct {
	TenantID        values.TenantId    `json:"tenant_id"`
	RunID           string             `json:"run_id"`
	AdmissionDigest string             `json:"admission_digest"`
	InvocationID    string             `json:"invocation_id"`
	InvokerID       string             `json:"invoker_id"`
	ConversationID  string             `json:"conversation_id"`
	ThreadID        string             `json:"thread_id"`
	PersonaID       string             `json:"persona_id"`
	PersonaVersion  string             `json:"persona_version"`
	InstallationID  string             `json:"installation_id"`
	AgentID         string             `json:"agent_id"`
	ToolCallID      string             `json:"tool_call_id"`
	ToolName        string             `json:"tool_name"`
	SkillID         string             `json:"skill_id"`
	SkillVersion    uint32             `json:"skill_version"`
	SkillDigest     string             `json:"skill_digest"`
	Arguments       []byte             `json:"arguments"`
	Output          []byte             `json:"output"`
	OutputDigest    string             `json:"output_digest"`
	DataClass       trustdlp.DataClass `json:"data_class"`
	CreatedAt       time.Time          `json:"created_at"`
}

func validateToolResultRecord(r ToolResultRecord) error {
	if r.TenantID.Validate() != nil || r.SkillVersion == 0 || r.CreatedAt.IsZero() || !r.DataClass.Valid() ||
		!sha256DigestPattern.MatchString(r.AdmissionDigest) || !sha256DigestPattern.MatchString("sha256:"+strings.TrimPrefix(r.SkillDigest, "sha256:")) || !sha256DigestPattern.MatchString(r.OutputDigest) ||
		len(r.Arguments) == 0 || len(r.Arguments) > 4096 || !json.Valid(r.Arguments) || len(r.Output) == 0 || len(r.Output) > 64*1024 || !json.Valid(r.Output) {
		return fmt.Errorf("%w: malformed tool result", ErrInvalid)
	}
	for _, value := range []string{r.RunID, r.InvocationID, r.InvokerID, r.ConversationID, r.ThreadID, r.PersonaID, r.PersonaVersion, r.InstallationID, r.AgentID, r.ToolCallID, r.ToolName, r.SkillID} {
		if value == "" || len(value) > 512 || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: incomplete tool result identity", ErrInvalid)
		}
	}
	sum := sha256.Sum256(r.Output)
	if r.OutputDigest != "sha256:"+hex.EncodeToString(sum[:]) {
		return fmt.Errorf("%w: tool result output digest mismatch", ErrInvalid)
	}
	return nil
}

// PutToolResult persists an immutable result. Replays must supply exactly the
// same identity, arguments and bytes; a reused call ID cannot replace evidence.
func (s *TenantStore) PutToolResult(ctx context.Context, r ToolResultRecord) error {
	if s == nil || ctx == nil || r.TenantID != s.tenant {
		return ErrInvalid
	}
	if err := validateToolResultRecord(r); err != nil {
		return err
	}
	body, err := json.Marshal(r)
	if err != nil {
		return ErrInvalid
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO persona_tool_results (tenant_id,run_id,tool_call_id,record,created_at) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (tenant_id,run_id,tool_call_id) DO NOTHING`, s.tenantID, r.RunID, r.ToolCallID, body, r.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("agentpersonastore: insert tool result: %w", err)
	}
	var retained []byte
	if err := tx.QueryRow(ctx, `SELECT record FROM persona_tool_results WHERE tenant_id=$1 AND run_id=$2 AND tool_call_id=$3`, s.tenantID, r.RunID, r.ToolCallID).Scan(&retained); err != nil {
		return err
	}
	var existing ToolResultRecord
	if json.Unmarshal(retained, &existing) != nil {
		return ErrInvalid
	}
	// Creation time belongs to the first successful persistence attempt.
	existing.CreatedAt = r.CreatedAt
	if !sameToolResult(existing, r) {
		return ErrConflict
	}
	return commit(ctx, tx)
}

func sameToolResult(a, b ToolResultRecord) bool {
	return reflect.DeepEqual(a, b)
}

// ListToolResults reads only one run from this tenant. Every row is validated
// before it can become grounding, including the digest of its original bytes.
func (s *TenantStore) ListToolResults(ctx context.Context, runID string) ([]ToolResultRecord, error) {
	if s == nil || ctx == nil || strings.TrimSpace(runID) == "" || strings.TrimSpace(runID) != runID {
		return nil, ErrInvalid
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT record FROM persona_tool_results WHERE tenant_id=$1 AND run_id=$2 ORDER BY created_at,tool_call_id COLLATE "C"`, s.tenantID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ToolResultRecord
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var r ToolResultRecord
		if json.Unmarshal(body, &r) != nil || r.TenantID != s.tenant || r.RunID != runID || validateToolResultRecord(r) != nil {
			return nil, ErrInvalid
		}
		r.Arguments = bytes.Clone(r.Arguments)
		r.Output = bytes.Clone(r.Output)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := commit(ctx, tx); err != nil {
		return nil, err
	}
	return out, nil
}
