package chatstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

const AgentUXAmbientOffersChanged = "ambient.offers.changed"

// AppendAmbientOfferChanged is a durable invalidation, never a card payload.
// Subscribers refresh the authenticated offer endpoint; private title, scope,
// recipient and time must not enter the conversation's broadcast event.
func AppendAmbientOfferChanged(ctx context.Context, tx dbport.Tx, tenant, conversation, agent, id string, revision uint64) error {
	var stored bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agentux_ambient_offer WHERE tenant_id=$1 AND conversation_id=$2 AND agent_id=$3 AND id=$4 AND revision=$5)`, tenant, conversation, agent, id, revision).Scan(&stored); err != nil {
		return err
	}
	if !stored {
		return chat.ErrPermissionDenied
	}
	return writeOutbox(ctx, tx, outboxWrite{TenantID: tenant, ConversationID: conversation, AggregateID: conversation, EventType: AgentUXAmbientOffersChanged, ActorID: agent, ActorHomeTenantID: tenant, TargetID: conversation, Revision: revision, Value: struct{}{}})
}

func AppendAmbientGrantChanged(ctx context.Context, tx dbport.Tx, tenant, conversation, agent, actor string) error {
	var stored bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agentux_ambient_grant WHERE tenant_id=$1 AND conversation_id=$2 AND agent_id=$3 AND updated_by=$4)`, tenant, conversation, agent, actor).Scan(&stored); err != nil {
		return err
	}
	if !stored {
		return chat.ErrPermissionDenied
	}
	return writeOutbox(ctx, tx, outboxWrite{TenantID: tenant, ConversationID: conversation, AggregateID: conversation, EventType: AgentUXAmbientOffersChanged, ActorID: actor, ActorHomeTenantID: tenant, TargetID: conversation, Value: struct{}{}})
}
