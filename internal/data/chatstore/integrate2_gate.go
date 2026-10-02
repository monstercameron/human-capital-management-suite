package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func checkGateMembershipTx(ctx context.Context, tx dbport.Tx, m chat.Membership) error {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT state_json FROM chat_gate_state WHERE tenant_id=$1 AND conversation_id=$2`, m.TenantID, m.ConversationID).Scan(&raw)
	if errors.Is(err, dbport.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var state chatgate.State
	if err = json.Unmarshal(raw, &state); err != nil {
		return chat.ErrUnavailable
	}
	gate := state.Gate
	if gate.Current == "" || gate.State == "paused" || gate.State == "retired" {
		return nil
	}
	if m.HomeTenantID != m.TenantID {
		return chat.ErrPermissionDenied
	}
	live, err := chatgate.ParseVersion(gate.Current)
	if err != nil {
		return chat.ErrUnavailable
	}
	for _, sub := range state.Submissions {
		accepted, e := chatgate.ParseVersion(sub.Version)
		if sub.Person == m.SubjectID && sub.Status == "admitted" && e == nil && accepted.Major == live.Major {
			return nil
		}
	}
	for _, override := range state.Overrides {
		if override.Person == m.SubjectID && override.Version == gate.Current {
			return nil
		}
	}
	return chat.ErrPermissionDenied
}
