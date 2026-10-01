package projectstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	projectdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/project"
)

const agentTaskProposalEvent = "agent.task.proposal"

// SaveAgentTaskProposal stores the proposal as an append-only project activity
// record. Draft, review, and commit are immutable facts; the latest proposal
// state is reconstructed from the activity stream, so process restarts do not
// lose an in-flight agent proposal.
func (s *Store) SaveAgentTaskProposal(ctx context.Context, proposal projectdomain.AgentTaskProposal) error {
	if s == nil || proposal.ID == "" || proposal.TenantID == "" || proposal.ProjectID == "" {
		return ErrInvalidRecord
	}
	if proposal.Revision != 1 {
		return ErrInvalidRecord
	}
	payload, err := json.Marshal(proposal)
	if err != nil {
		return err
	}
	return s.RunTenantTx(ctx, proposal.TenantID, func(tx dbport.Tx) error {
		// Serialize proposal streams through the owning project row. The lock is
		// a separate statement so READ COMMITTED gives the next query a fresh
		// snapshot after a concurrent writer commits.
		var projectID string
		if err := tx.QueryRow(ctx, `SELECT id FROM project WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, proposal.TenantID, proposal.ProjectID).Scan(&projectID); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM project_activity WHERE tenant_id=$1 AND project_id=$2 AND aggregate_id=$3 AND event_type=$4)`, proposal.TenantID, proposal.ProjectID, proposal.ID, agentTaskProposalEvent).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("%w: agent proposal already exists", ErrRevisionConflict)
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_activity(tenant_id,id,project_id,aggregate_id,actor_id,origin,event_type,prior_revision,new_revision,payload) VALUES($1,$2,$3,$4,$5,'AGENT',$6,0,$7,$8::jsonb)`, proposal.TenantID, uuid.NewString(), string(proposal.ProjectID), proposal.ID, proposal.AgentID, agentTaskProposalEvent, proposal.Revision, payload)
		return err
	})
}

func (s *Store) GetAgentTaskProposal(ctx context.Context, tenantID, projectID, proposalID string) (projectdomain.AgentTaskProposal, error) {
	if s == nil || tenantID == "" || projectID == "" || proposalID == "" {
		return projectdomain.AgentTaskProposal{}, ErrInvalidRecord
	}
	var payload []byte
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT payload FROM project_activity WHERE tenant_id=$1 AND project_id=$2 AND aggregate_id=$3 AND event_type=$4 ORDER BY new_revision DESC,id DESC LIMIT 1`, tenantID, projectID, proposalID, agentTaskProposalEvent).Scan(&payload)
	})
	if errors.Is(err, dbport.ErrNoRows) {
		return projectdomain.AgentTaskProposal{}, ErrNotFound
	}
	if err != nil {
		return projectdomain.AgentTaskProposal{}, err
	}
	var proposal projectdomain.AgentTaskProposal
	if err := json.Unmarshal(payload, &proposal); err != nil {
		return projectdomain.AgentTaskProposal{}, err
	}
	if proposal.TenantID != tenantID || string(proposal.ProjectID) != projectID || proposal.ID != proposalID {
		return projectdomain.AgentTaskProposal{}, ErrInvalidRecord
	}
	return proposal, nil
}

func (s *Store) UpdateAgentTaskProposal(ctx context.Context, proposal projectdomain.AgentTaskProposal) error {
	if proposal.Revision < 2 {
		return ErrInvalidRecord
	}
	payload, err := json.Marshal(proposal)
	if err != nil {
		return err
	}
	return s.RunTenantTx(ctx, proposal.TenantID, func(tx dbport.Tx) error {
		var projectID string
		if err := tx.QueryRow(ctx, `SELECT id FROM project WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, proposal.TenantID, proposal.ProjectID).Scan(&projectID); err != nil {
			return err
		}
		var current int64
		var previous []byte
		if err := tx.QueryRow(ctx, `SELECT new_revision,payload FROM project_activity WHERE tenant_id=$1 AND project_id=$2 AND aggregate_id=$3 AND event_type=$4 ORDER BY new_revision DESC,id DESC LIMIT 1 FOR UPDATE`, proposal.TenantID, proposal.ProjectID, proposal.ID, agentTaskProposalEvent).Scan(&current, &previous); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if proposal.Revision != uint64(current)+1 {
			return ErrRevisionConflict
		}
		var prior projectdomain.AgentTaskProposal
		if err := json.Unmarshal(previous, &prior); err != nil {
			return err
		}
		if prior.TenantID != proposal.TenantID || prior.ProjectID != proposal.ProjectID || prior.ID != proposal.ID || prior.TaskID != proposal.TaskID || prior.AgentID != proposal.AgentID || prior.BaseTaskRevision != proposal.BaseTaskRevision {
			return ErrRevisionConflict
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_activity(tenant_id,id,project_id,aggregate_id,actor_id,origin,event_type,prior_revision,new_revision,payload) VALUES($1,$2,$3,$4,$5,'AGENT',$6,$7,$8,$9::jsonb)`, proposal.TenantID, uuid.NewString(), string(proposal.ProjectID), proposal.ID, proposal.AgentID, agentTaskProposalEvent, current, proposal.Revision, payload)
		return err
	})
}
