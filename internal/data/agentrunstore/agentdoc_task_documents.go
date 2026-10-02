package agentrunstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// RecordDocumentOmissions updates only the mutable, content-free resolver
// projection. Task state and its CAS revision are deliberately unchanged.
func (s *TenantStore) RecordDocumentOmissions(ctx context.Context, id string, refs []agentdocref.Reference, omissions []agentdocref.Omission) error {
	if s == nil || agentdocref.Validate(refs, agentdocref.MaxRequestReferences) != nil || agentrun.ValidateTaskDocumentOmissions(refs, omissions) != nil {
		return agentrun.ErrDocumentReferenceInvalid
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT plan FROM agent_task WHERE tenant_id=$1 AND task_id=$2 FOR UPDATE`, s.tenant, id).Scan(&raw); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return fmt.Errorf("%w: %s", agentrun.ErrNotFound, id)
		}
		return fmt.Errorf("agentrunstore: read task documents: %w", err)
	}
	var plan agentrun.AgentPlan
	if json.Unmarshal(raw, &plan) != nil || !slices.Equal(plan.DocumentReferences, refs) {
		return agentrun.ErrDocumentReferenceInvalid
	}
	encoded, err := json.Marshal(omissions)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE agent_task SET plan=jsonb_set(plan,'{document_omissions}',$3::jsonb,true) WHERE tenant_id=$1 AND task_id=$2`, s.tenant, id, string(encoded)); err != nil {
		return fmt.Errorf("agentrunstore: record task document omissions: %w", err)
	}
	return commit(ctx, tx)
}
