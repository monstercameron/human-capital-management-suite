package application

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// localAgentDemoSupportRoom is the channel Support Desk's alerts go to.
const localAgentDemoSupportRoom = "incident-review"

// ensureLocalAgentDemoAmbientInstallations installs Task Catcher and Reminder in
// the working channel as chat apps that may read its posts, which is what puts
// their "Reads every message here" switch in Conversation details. It never
// writes the switch itself: the channel administrator turns it on, so on a
// prepared cell neither agent has read a message (AGENTUX-066, -067, -068). The
// number of installations created is returned; a second run creates none.
func ensureLocalAgentDemoAmbientInstallations(ctx context.Context, chat *chatstore.Store, tenant, admin string, now time.Time) (int, error) {
	if chat == nil || ctx == nil || tenant == "" || admin == "" {
		return 0, ErrLocalDevPersonaChatBootstrap
	}
	conversation := localDevPersonaDemoConversationID(tenant, localAgentDemoAmbientRoom)
	created := 0
	for _, handle := range []string{"task-catcher", "reminder"} {
		manifest, err := json.Marshal(map[string]any{"app_id": handle, "version": 1, "scopes": []string{"chat.posts.read"}, "commands": []any{}, "cards": []any{}, "tabs": []any{}, "callback_origins": []string{}})
		if err != nil {
			return created, err
		}
		err = chat.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			n, err := tx.Exec(ctx, `INSERT INTO chat_app_installation(id,tenant_id,conversation_id,app_id,version,manifest,granted_scopes,status,approver,revision,created_at,updated_at) SELECT $1,$2,$3,$4,1,$5,ARRAY['chat.posts.read'],'ACTIVE',$6,1,$7,$7 WHERE EXISTS(SELECT 1 FROM chat_conversation WHERE tenant_id=$2 AND id=$3) ON CONFLICT(id) DO NOTHING`, tenant+":"+conversation+":"+handle, tenant, conversation, handle, manifest, admin, now.Add(-time.Minute).UTC())
			if err != nil {
				return err
			}
			created += int(n)
			return nil
		})
		if err != nil {
			return created, fmt.Errorf("install %s in %s: %w", handle, localAgentDemoAmbientRoom, err)
		}
	}
	return created, nil
}

// ensureLocalAgentDemoSupportAlertMember adds Support Desk to the incident
// channel as a member, which is how its alert (a title, a severity, a claimed
// customer and a link, never an email's text) reaches a private channel. It is
// visible in the channel's member list like any member; it reads nothing and
// answers nobody. A cell without that channel is left alone.
func ensureLocalAgentDemoSupportAlertMember(ctx context.Context, chat *chatstore.Store, tenant string) (int, error) {
	if chat == nil || ctx == nil || tenant == "" {
		return 0, ErrLocalDevPersonaChatBootstrap
	}
	conversation := localDevPersonaDemoConversationID(tenant, localAgentDemoSupportRoom)
	created := 0
	err := chat.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `INSERT INTO chat_membership(tenant_id,conversation_id,member_id,home_tenant_id,role,state) SELECT $1,$2,$3,$1,'MEMBER','active' WHERE EXISTS(SELECT 1 FROM chat_conversation WHERE tenant_id=$1 AND id=$2) ON CONFLICT DO NOTHING`, tenant, conversation, localAgentDemoSupportAgent)
		if err != nil {
			return err
		}
		created = int(n)
		return nil
	})
	return created, err
}

// ensureLocalAgentDemoRoomManager makes the demo administrator an active manager
// of a room an agent is installed in. Installing an agent is a manager's act: the
// placement check requires the installer to be a current manager of the
// conversation, and in the seeded workspace the administrator manages #general
// but is only a member of the other rooms. A room where the administrator
// already manages is left as it is; the change is one membership row and the
// room's owner can undo it like any role change.
func ensureLocalAgentDemoRoomManager(ctx context.Context, chat *chatstore.Store, tenant, conversation, admin string) error {
	if chat == nil || ctx == nil || tenant == "" || conversation == "" || admin == "" {
		return ErrLocalDevPersonaChatBootstrap
	}
	return chat.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,role,state,history_visibility,revision) SELECT $1,$2,$1,$3,'MANAGER','active','FULL_HISTORY',1 WHERE EXISTS(SELECT 1 FROM chat_conversation WHERE tenant_id=$1 AND id=$2) ON CONFLICT (tenant_id,conversation_id,home_tenant_id,member_id) DO UPDATE SET role='MANAGER',state='active',left_at=NULL,revision=chat_membership.revision+1 WHERE upper(chat_membership.role)<>'MANAGER' OR chat_membership.state<>'active'`, tenant, conversation, admin)
		return err
	})
}
