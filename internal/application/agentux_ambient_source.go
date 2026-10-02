package application

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Source authority is checked even after an extraction budget is exhausted.
// A changed accepted item requires fresh consent; old consent cannot authorize
// a reminder whose source no longer says the same thing.
func (s *AgentUXAmbientService) invalidateAcceptedSource(ctx context.Context, tx dbport.Tx, tenant, conversation, source, agent string, revision uint64) error {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT data FROM agentux_ambient_offer WHERE tenant_id=$1 AND conversation_id=$2 AND source_id=$3 AND agent_id=$4 AND source_revision<>$5 AND state IN ('ADDED','SET') FOR UPDATE`, tenant, conversation, source, agent, revision).Scan(&raw)
	if errors.Is(err, dbport.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var o AgentUXAmbientOffer
	if err = json.Unmarshal(raw, &o); err != nil {
		return err
	}
	if o.Kind == "REMINDER" {
		if s.Effects == nil {
			return ErrAgentUXAmbientInvalid
		}
		if err = s.Effects.CancelAmbientMessageAnnouncement(dbport.ContextWithTx(ctx, tx), o); err != nil {
			return err
		}
	}
	if o.Kind == "TASK" && o.Scope == "PUBLIC" {
		store, ok := s.DB.(interface {
			CancelAmbientChannelTask(context.Context, string, string, string, string) error
		})
		if !ok {
			return ErrAgentUXAmbientInvalid
		}
		if err = store.CancelAmbientChannelTask(dbport.ContextWithTx(ctx, tx), tenant, conversation, o.ID, source); err != nil {
			return err
		}
	}
	if o.Kind == "TASK" && o.Scope == "PRIVATE" {
		if _, err = tx.Exec(ctx, `DELETE FROM agentux_ambient_task WHERE tenant_id=$1 AND id=$2 AND person_id=$3 AND NOT completed`, tenant, o.ID, o.Person); err != nil {
			return err
		}
	}
	o.State = "SOURCE_CHANGED"
	o.Reason = "source_changed"
	o.Revision++
	// Keep the former source revision until extraction succeeds, so Control
	// cannot reconfirm stale text when screening or a budget prevents a read.
	return agentUXAmbientSave(ctx, tx, o)
}
