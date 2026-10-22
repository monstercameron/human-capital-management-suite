package chatstore

import (
	"context"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ReadAgentReactionPolicy returns the workspace switch (on when none was
// stored) and the conversation's own switch when it has one (AGENTUX-052).
func (s *Store) ReadAgentReactionPolicy(ctx context.Context, tenant, conversation string) (chat.AgentReactionPolicy, *chat.AgentReactionPolicy, error) {
	workspace := chat.DefaultAgentReactionPolicy()
	var channel *chat.AgentReactionPolicy
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var enabled bool
		err := tx.QueryRow(ctx, `SELECT reactions_enabled FROM chat_agent_reaction_policy WHERE tenant_id=$1 AND conversation_id=''`, tenant).Scan(&enabled)
		if err == nil {
			workspace = chat.AgentReactionPolicy{ReactionsEnabled: enabled}
		} else if err != dbport.ErrNoRows {
			return err
		}
		if conversation == "" {
			return nil
		}
		err = tx.QueryRow(ctx, `SELECT reactions_enabled FROM chat_agent_reaction_policy WHERE tenant_id=$1 AND conversation_id=$2`, tenant, conversation).Scan(&enabled)
		if err == nil {
			p := chat.AgentReactionPolicy{ReactionsEnabled: enabled}
			channel = &p
		} else if err != dbport.ErrNoRows {
			return err
		}
		return nil
	})
	return workspace, channel, err
}

// WriteAgentReactionPolicy stores the workspace switch (conversation empty) or
// one conversation's, replacing what was there.
func (s *Store) WriteAgentReactionPolicy(ctx context.Context, tenant, conversation string, p chat.AgentReactionPolicy, by string) error {
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_agent_reaction_policy(tenant_id,conversation_id,reactions_enabled,updated_by,updated_at) VALUES($1,$2,$3,$4,now()) ON CONFLICT (tenant_id,conversation_id) DO UPDATE SET reactions_enabled=$3,updated_by=$4,updated_at=now()`, tenant, conversation, p.ReactionsEnabled, by)
		return err
	})
}
