package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// AddAmbientChannelTask shares the offer's transaction, including the normal
// to-do mutation, source visibility, policy, revisions and audit inventory.
func (s *Store) AddAmbientChannelTask(ctx context.Context, tenant, conversation, actor, id, text, source string, authorize func(context.Context) error) error {
	tx, ok := dbport.TxFromContext(ctx)
	if !ok || tx == nil {
		return chat.ErrInvalidArgument
	}
	if id == "" || source == "" {
		return chat.ErrInvalidArgument
	}
	m := ChannelTodoMutation{Operation: "ADD", Text: text, SourcePostID: source}
	if err := validateChannelTodoMutation(m); err != nil {
		return err
	}
	if _, err := channelTodoMember(ctx, tx, tenant, tenant, conversation, actor, true); err != nil {
		return err
	}
	if err := channelTodoPolicyFence(ctx, tx, tenant, conversation, authorize); err != nil {
		return err
	}
	if err := AmbientChannelTaskSourceReadable(ctx, tx, tenant, tenant, conversation, actor, source); err != nil {
		return err
	}
	list := ChannelTodoList{ConversationID: conversation, Revision: 1, Items: []ChannelTodoItem{}}
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT revision,pinned,items_json FROM chat_channel_todo WHERE tenant_id=$1 AND conversation_id=$2`, tenant, conversation).Scan(&list.Revision, &list.Pinned, &raw)
	if err != nil && !errors.Is(err, dbport.ErrNoRows) {
		return err
	}
	if err == nil {
		if err = json.Unmarshal(raw, &list.Items); err != nil {
			return err
		}
	}
	existing := -1
	for i, item := range list.Items {
		if item.ID == id {
			if item.SourcePostID != source {
				return chat.ErrPermissionDenied
			}
			if item.Completed || item.Text == text {
				return nil
			}
			existing = i
		}
	}
	prior, err := json.Marshal(list.Items)
	if err != nil {
		return err
	}
	operation := "ADD"
	if existing >= 0 {
		var authorized bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agentux_ambient_offer o JOIN chat_post p ON p.tenant_id=o.tenant_id AND p.id=o.source_id AND p.conversation_id=o.conversation_id WHERE o.tenant_id=$1 AND o.conversation_id=$2 AND o.id=$3 AND o.scope='PUBLIC' AND o.kind='TASK' AND o.title=$4 AND o.source_id=$5 AND o.state IN ('SOURCE_CHANGED','OFFERED') AND NOT p.tombstoned AND p.revision=o.source_revision)`, tenant, conversation, id, text, source).Scan(&authorized); err != nil {
			return err
		}
		if !authorized {
			return chat.ErrPermissionDenied
		}
		list.Items[existing].Text = text
		operation = "EDIT"
	} else {
		if err = applyChannelTodoMutation(&list, tenant, actor, m); err != nil {
			return err
		}
		list.Items[len(list.Items)-1].ID = id
	}
	list.Revision++
	raw, err = json.Marshal(list.Items)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO chat_channel_todo(tenant_id,conversation_id,revision,pinned,items_json) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,conversation_id) DO UPDATE SET revision=EXCLUDED.revision,items_json=EXCLUDED.items_json,updated_at=now()`, tenant, conversation, list.Revision, list.Pinned, raw); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO chat_channel_todo_revision(tenant_id,conversation_id,revision,actor_home_tenant_id,actor_id,operation,prior_pinned,pinned,prior_items_json,items_json) VALUES($1,$2,$3,$1,$4,$8,$5,$5,$6,$7)`, tenant, conversation, list.Revision, actor, list.Pinned, prior, raw, operation); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_record_inventory(tenant_id,record_id,conversation_id,kind,source_id,revision,created_at) VALUES($1,$2,$3,'CHANNEL_TODO',$3,$4,now())`, tenant, "todo:"+conversation+":"+strconv.FormatUint(list.Revision, 10), conversation, list.Revision)
	return err
}

// AmbientChannelTaskSourceReadable checks the message's actual visibility.
// Unlike a manually copied pinned item, an ambient source need not be pinned.
func AmbientChannelTaskSourceReadable(ctx context.Context, tx dbport.Tx, tenant, home, conversation, actor, source string) error {
	if home != tenant {
		var shared bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_share_grant WHERE tenant_id=$1 AND conversation_id=$2 AND consumer_tenant=$3 AND accepted_at IS NOT NULL AND revoked_at IS NULL AND expires_at>now())`, tenant, conversation, home).Scan(&shared); err != nil {
			return err
		}
		if !shared {
			return chat.ErrPermissionDenied
		}
	}
	var found string
	err := tx.QueryRow(ctx, `SELECT p.id FROM chat_post p JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=$3 AND m.member_id=$4 AND m.state='active' AND m.left_at IS NULL WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.id=$5 AND NOT p.tombstoned AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at)) FOR SHARE OF p,m`, tenant, conversation, home, actor, source).Scan(&found)
	if errors.Is(err, dbport.ErrNoRows) {
		return chat.ErrNotFound
	}
	return err
}

// CancelAmbientChannelTask removes only an unfinished item whose durable offer
// proves its source. The source deletion is the authorization for cancellation.
func (s *Store) CancelAmbientChannelTask(ctx context.Context, tenant, conversation, id, source string) error {
	tx, ok := dbport.TxFromContext(ctx)
	if !ok {
		return chat.ErrInvalidArgument
	}
	var authorized bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agentux_ambient_offer o JOIN chat_post p ON p.tenant_id=o.tenant_id AND p.conversation_id=o.conversation_id AND p.id=o.source_id WHERE o.tenant_id=$1 AND o.conversation_id=$2 AND o.id=$3 AND o.source_id=$4 AND o.kind='TASK' AND o.scope='PUBLIC' AND o.state IN ('ADDED','SOURCE_CHANGED') AND (p.tombstoned OR p.revision<>o.source_revision))`, tenant, conversation, id, source).Scan(&authorized); err != nil {
		return err
	}
	if !authorized {
		return chat.ErrPermissionDenied
	}
	var list ChannelTodoList
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT revision,pinned,items_json FROM chat_channel_todo WHERE tenant_id=$1 AND conversation_id=$2 FOR UPDATE`, tenant, conversation).Scan(&list.Revision, &list.Pinned, &raw)
	if errors.Is(err, dbport.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = json.Unmarshal(raw, &list.Items); err != nil {
		return err
	}
	found := false
	for _, item := range list.Items {
		if item.ID == id && item.SourcePostID == source && !item.Completed {
			found = true
		}
	}
	if !found {
		return nil
	}
	prior := raw
	if err = applyChannelTodoMutation(&list, tenant, "task-catcher", ChannelTodoMutation{Operation: "DELETE", ItemID: id}); err != nil {
		return err
	}
	list.Revision++
	raw, err = json.Marshal(list.Items)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE chat_channel_todo SET revision=$3,items_json=$4,updated_at=now() WHERE tenant_id=$1 AND conversation_id=$2`, tenant, conversation, list.Revision, raw); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO chat_channel_todo_revision(tenant_id,conversation_id,revision,actor_home_tenant_id,actor_id,operation,prior_pinned,pinned,prior_items_json,items_json) VALUES($1,$2,$3,$1,'task-catcher','DELETE',$4,$4,$5,$6)`, tenant, conversation, list.Revision, list.Pinned, prior, raw); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_record_inventory(tenant_id,record_id,conversation_id,kind,source_id,revision,created_at) VALUES($1,$2,$3,'CHANNEL_TODO',$3,$4,now())`, tenant, "todo:"+conversation+":"+strconv.FormatUint(list.Revision, 10), conversation, list.Revision)
	return err
}
