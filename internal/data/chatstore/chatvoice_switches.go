package chatstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Voice switch scopes of chat_voice_setting.
const (
	VoiceScopeWorkspace = "workspace"
	VoiceScopeChannel   = "channel"
	VoiceScopePerson    = "person"
)

// VoicePolicy reads the three switches that decide whether a voice message may
// be sent into a conversation. A switch nobody has set takes the decision
// record's default: on for the workspace and for the person, off for a channel.
func (s *Store) VoicePolicy(ctx context.Context, tenantID, person, conversation string) (chat.VoicePolicy, error) {
	policy := chat.VoicePolicy{WorkspaceEnabled: true, PersonalEnabled: true}
	if tenantID == "" || person == "" || conversation == "" {
		return chat.VoicePolicy{}, chat.ErrInvalidArgument
	}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT scope,scope_id,enabled FROM chat_voice_setting WHERE tenant_id=$1 AND ((scope='workspace') OR (scope='channel' AND scope_id=$2) OR (scope='person' AND scope_id=$3))`, tenantID, conversation, person)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var scope, id string
			var enabled bool
			if err = rows.Scan(&scope, &id, &enabled); err != nil {
				return err
			}
			switch scope {
			case VoiceScopeWorkspace:
				policy.WorkspaceEnabled = enabled
			case VoiceScopeChannel:
				policy.ChannelEnabled = enabled
			case VoiceScopePerson:
				policy.PersonalEnabled = enabled
			}
		}
		return rows.Err()
	})
	return policy, err
}

// VoiceSwitchValues reads the two switches that are not about one conversation,
// for the settings pages. A switch nobody has set is on.
func (s *Store) VoiceSwitchValues(ctx context.Context, tenantID, person string) (workspace, personal bool, err error) {
	workspace, personal = true, true
	if tenantID == "" || person == "" {
		return false, false, chat.ErrInvalidArgument
	}
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT scope,enabled FROM chat_voice_setting WHERE tenant_id=$1 AND ((scope='workspace') OR (scope='person' AND scope_id=$2))`, tenantID, person)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var scope string
			var enabled bool
			if err = rows.Scan(&scope, &enabled); err != nil {
				return err
			}
			if scope == VoiceScopeWorkspace {
				workspace = enabled
			} else {
				personal = enabled
			}
		}
		return rows.Err()
	})
	return workspace, personal, err
}

// PutVoiceSwitch stores one switch. Who may set it is decided by the caller;
// the store only refuses a malformed scope.
func (s *Store) PutVoiceSwitch(ctx context.Context, tenantID, by, scope, scopeID string, enabled bool) error {
	if tenantID == "" || by == "" || (scope == VoiceScopeWorkspace) != (scopeID == "") || (scope != VoiceScopeWorkspace && scope != VoiceScopeChannel && scope != VoiceScopePerson) {
		return chat.ErrInvalidArgument
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_voice_setting(tenant_id,scope,scope_id,enabled,updated_by) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,scope,scope_id) DO UPDATE SET enabled=EXCLUDED.enabled,updated_by=EXCLUDED.updated_by,updated_at=now()`, tenantID, scope, scopeID, enabled, by)
		return err
	})
}

// VoiceListenBarred says where text may not be sent to an outside service for
// the person: whether the workspace bars it everywhere, and the conversations
// they are in whose channel setting bars it ("Never use an outside service").
// Listen is absent in those places, so the page never offers an action the
// server would refuse.
func (s *Store) VoiceListenBarred(ctx context.Context, tenantID, home, person string) (workspace bool, conversations []string, err error) {
	if tenantID == "" || home == "" || person == "" {
		return false, nil, chat.ErrInvalidArgument
	}
	conversations = []string{}
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		allowed := true
		if err := tx.QueryRow(ctx, `SELECT external_allowed FROM chatlang_setting WHERE tenant_id=$1 AND conversation_id=''`, tenantID).Scan(&allowed); err != nil && err != dbport.ErrNoRows {
			return err
		}
		workspace = !allowed
		rows, err := tx.Query(ctx, `SELECT s.conversation_id FROM chatlang_setting s JOIN chat_membership m ON m.tenant_id=s.tenant_id AND m.conversation_id=s.conversation_id AND m.home_tenant_id=$2 AND m.member_id=$3 AND m.state='active' AND m.left_at IS NULL WHERE s.tenant_id=$1 AND s.conversation_id<>'' AND s.external='barred'`, tenantID, home, person)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				return err
			}
			conversations = append(conversations, id)
		}
		return rows.Err()
	})
	return workspace, conversations, err
}
