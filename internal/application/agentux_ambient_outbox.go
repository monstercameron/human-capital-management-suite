package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// AgentUXAmbientOutbox is an independent canonical-event-log consumer. It
// never marks the publisher's receipt and never reads payload.Value (text).
type AgentUXAmbientOutbox struct {
	Service    *AgentUXAmbientService
	AuthorZone func(context.Context, string, string) (string, error)
}

func (w AgentUXAmbientOutbox) Drain(ctx context.Context, tenant string) error {
	if w.Service == nil || !w.Service.available() || w.AuthorZone == nil {
		return ErrAgentUXAmbientInvalid
	}
	type event struct {
		id                           int64
		conversation, source, author string
		deleted, human               bool
	}
	var events []event
	err := w.Service.DB.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT o.id,o.payload->>'ConversationID',o.aggregate_id,COALESCE(p.author_id,''),COALESCE(p.tombstoned,false),EXISTS(SELECT 1 FROM chat_membership m WHERE m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.member_id=p.author_id AND m.home_tenant_id=$1 AND m.state='active' AND m.left_at IS NULL) AND p.source_attribution IS NULL AND NOT EXISTS(SELECT 1 FROM chat_app_installation i WHERE i.tenant_id=$1 AND i.app_id=p.author_id) FROM chat_outbox o LEFT JOIN chat_post p ON p.tenant_id=o.tenant_id AND p.id=o.aggregate_id AND p.conversation_id=o.payload->>'ConversationID' WHERE o.tenant_id=$1 AND o.id>COALESCE((SELECT last_outbox_id FROM chat_outbox_cursor WHERE tenant_id=$1 AND consumer='agentux-ambient'),0) AND o.event_type IN ('post.created','post.edited','post.deleted') ORDER BY o.id LIMIT 100`, tenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e event
			if err = rows.Scan(&e.id, &e.conversation, &e.source, &e.author, &e.deleted, &e.human); err != nil {
				return err
			}
			events = append(events, e)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	for _, e := range events {
		if e.human || e.deleted {
			zone := "UTC"
			if !e.deleted {
				zone, err = w.AuthorZone(ctx, tenant, e.author)
				if err != nil {
					return err
				}
			}
			for _, agent := range []string{"task-catcher", "reminder"} {
				if err = w.Service.ProcessMessage(ctx, tenant, e.conversation, e.source, agent, zone); err != nil {
					return err
				}
			}
		}
		if err = w.Service.DB.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO chat_outbox_cursor(tenant_id,consumer,last_outbox_id) VALUES($1,'agentux-ambient',$2) ON CONFLICT(tenant_id,consumer) DO UPDATE SET last_outbox_id=GREATEST(chat_outbox_cursor.last_outbox_id,EXCLUDED.last_outbox_id),updated_at=now()`, tenant, e.id)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

type AgentUXAmbientFixtureModel struct{}

func (AgentUXAmbientFixtureModel) ExtractAmbient(_ context.Context, agent string, input AgentUXAmbientModelInput) (AgentUXAmbientProposal, error) {
	if !input.Untrusted || input.SourceKind != "CHAT_MESSAGE" {
		return AgentUXAmbientProposal{}, ErrAgentUXAmbientDenied
	}
	return AgentUXAmbientFixtureProposal(input.Message, agent), nil
}
