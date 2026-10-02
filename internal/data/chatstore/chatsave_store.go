package chatstore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var _ chat.SavedStore = (*Store)(nil)

const chatsaveColumns = `host_tenant_id,home_tenant_id,person_id,conversation_id,post_id,state,note,due_at,created_at,updated_at,revision`

func chatsaveScan(row interface{ Scan(...any) error }) (chat.SavedItem, error) {
	var item chat.SavedItem
	err := row.Scan(&item.TenantID, &item.HomeTenantID, &item.PersonID, &item.ConversationID, &item.PostID, &item.State, &item.Note, &item.DueAt, &item.CreatedAt, &item.UpdatedAt, &item.Revision)
	if errors.Is(err, dbport.ErrNoRows) {
		return item, chat.ErrNotFound
	}
	return item, err
}

func (s *Store) chatsaveTx(ctx context.Context, p chat.Principal, tenantID string, fn func(dbport.Tx) error) error {
	if err := chat.ValidateSavedOwner(ctx, p, tenantID); err != nil {
		return err
	}
	return s.RunTenantTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('hcmnext.saved_home_tenant_id',$1,true),set_config('hcmnext.saved_person_id',$2,true)`, p.TenantID, p.SubjectID); err != nil {
			return err
		}
		return fn(tx)
	})
}

func (s *Store) SaveItem(ctx context.Context, p chat.Principal, item chat.SavedItem) (chat.SavedItem, error) {
	if item.HomeTenantID != p.TenantID || item.PersonID != p.SubjectID {
		return chat.SavedItem{}, chat.ErrPermissionDenied
	}
	if item.ConversationID == "" || item.PostID == "" {
		return chat.SavedItem{}, chat.ErrInvalidArgument
	}
	var result chat.SavedItem
	if err := chat.ValidateSavedOwner(ctx, p, item.TenantID); err != nil {
		return result, err
	}
	err := s.RunTenantTx(ctx, item.TenantID, func(tx dbport.Tx) error {
		// Recheck membership and history inside the save transaction. The service
		// already checked current policy; a concurrent leave cannot commit first.
		// Lock the membership and post against leave/deletion until commit.
		rows, err := tx.Query(ctx, `SELECT m.member_id FROM chat_membership m JOIN chat_post p ON p.tenant_id=m.tenant_id AND p.conversation_id=m.conversation_id WHERE m.tenant_id=$1 AND m.home_tenant_id=$2 AND m.member_id=$3 AND m.conversation_id=$4 AND p.id=$5 AND m.state='active' AND m.left_at IS NULL AND NOT p.tombstoned AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at)) FOR SHARE OF m,p`, item.TenantID, p.TenantID, p.SubjectID, item.ConversationID, item.PostID)
		if err != nil {
			return err
		}
		found := rows.Next()
		rowErr := rows.Err()
		rows.Close()
		if rowErr != nil {
			return rowErr
		}
		if !found {
			return chat.ErrPermissionDenied
		}
		if _, err = tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id',$1,true),set_config('hcmnext.saved_home_tenant_id',$1,true),set_config('hcmnext.saved_person_id',$2,true)`, p.TenantID, p.SubjectID); err != nil {
			return err
		}
		result, err = chatsaveScan(tx.QueryRow(ctx, `INSERT INTO chat_saved_item(tenant_id,home_tenant_id,person_id,conversation_id,post_id,host_tenant_id) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,home_tenant_id,person_id,host_tenant_id,conversation_id,post_id) DO UPDATE SET person_id=EXCLUDED.person_id RETURNING `+chatsaveColumns, p.TenantID, p.TenantID, p.SubjectID, item.ConversationID, item.PostID, item.TenantID))
		return err
	})
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == "P5021" {
		err = chat.ErrSavedLimit
	}
	return result, err
}

func (s *Store) RemoveSaved(ctx context.Context, p chat.Principal, tenantID, conversationID, postID string) error {
	return s.chatsaveTx(ctx, p, tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM chat_saved_item WHERE tenant_id=$1 AND home_tenant_id=$2 AND person_id=$3 AND conversation_id=$4 AND post_id=$5 AND host_tenant_id=$6`, p.TenantID, p.TenantID, p.SubjectID, conversationID, postID, tenantID)
		return err
	})
}

func (s *Store) ChangeSaved(ctx context.Context, p chat.Principal, tenantID, conversationID, postID string, change chat.SavedChange) (chat.SavedItem, error) {
	if (change.State != nil && *change.State != chat.SavedTodo && *change.State != chat.SavedDone) || (change.Note != nil && len(*change.Note) > 4000) {
		return chat.SavedItem{}, chat.ErrInvalidArgument
	}
	var result chat.SavedItem
	err := s.chatsaveTx(ctx, p, tenantID, func(tx dbport.Tx) error {
		var err error
		result, err = chatsaveScan(tx.QueryRow(ctx, `UPDATE chat_saved_item SET state=COALESCE($6,state),note=COALESCE($7,note),due_at=CASE WHEN $8 THEN $9 ELSE due_at END,reminded_at=CASE WHEN $8 THEN NULL ELSE reminded_at END WHERE tenant_id=$1 AND home_tenant_id=$2 AND person_id=$3 AND conversation_id=$4 AND post_id=$5 AND host_tenant_id=$10 RETURNING `+chatsaveColumns, p.TenantID, p.TenantID, p.SubjectID, conversationID, postID, change.State, change.Note, change.SetDue, change.DueAt, tenantID))
		return err
	})
	return result, err
}

type chatsaveCursor struct {
	Tenant, Home, Person string
	Tab                  chat.SavedState
	Created              time.Time
	Conversation, Post   string
	Host                 string
}

func (s *Store) ListSavedItems(ctx context.Context, p chat.Principal, tenantID string, tab chat.SavedState, page chat.Page) (chat.SavedPage, error) {
	result := chat.SavedPage{Items: []chat.SavedItem{}}
	if tab != chat.SavedTodo && tab != chat.SavedDone && tab != chat.SavedAll || page.PageSize > 200 || len(page.Cursor) > 4096 {
		return result, chat.ErrInvalidArgument
	}
	limit := int(page.PageSize)
	if limit == 0 {
		limit = 50
	}
	var cursor chatsaveCursor
	if page.Cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(page.Cursor)
		if err != nil || json.Unmarshal(b, &cursor) != nil || cursor.Tenant != tenantID || cursor.Home != p.TenantID || cursor.Person != p.SubjectID || cursor.Tab != tab || cursor.Created.IsZero() {
			return result, chat.ErrInvalidArgument
		}
	}
	err := s.chatsaveTx(ctx, p, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+chatsaveColumns+` FROM chat_saved_item WHERE tenant_id=$1 AND home_tenant_id=$2 AND person_id=$3 AND ($4='all' OR state=$4) AND ($5 OR (created_at,host_tenant_id,conversation_id,post_id)<($6,$10,$7,$8)) ORDER BY created_at DESC,host_tenant_id DESC,conversation_id DESC,post_id DESC LIMIT $9`, p.TenantID, p.TenantID, p.SubjectID, string(tab), page.Cursor == "", cursor.Created, cursor.Conversation, cursor.Post, limit+1, cursor.Host)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := chatsaveScan(rows)
			if err != nil {
				return err
			}
			result.Items = append(result.Items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return chat.SavedPage{}, err
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		last := result.Items[limit-1]
		b, _ := json.Marshal(chatsaveCursor{tenantID, p.TenantID, p.SubjectID, tab, last.CreatedAt, last.ConversationID, last.PostID, last.TenantID})
		result.NextCursor = base64.RawURLEncoding.EncodeToString(b)
	}
	return result, nil
}

// EraseSavedPerson is the erasure coordinator's delete-only port. The
// coordinator authorizes erasure before calling it; it exposes no read and
// removes every host reference even when the person no longer has a session.
func (s *Store) EraseSavedPerson(ctx context.Context, p chat.Principal, tenantID string) error {
	if p.TenantID != tenantID || p.TenantID == "" || p.SubjectID == "" {
		return chat.ErrInvalidArgument
	}
	return s.RunTenantTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('hcmnext.saved_home_tenant_id',$1,true),set_config('hcmnext.saved_person_id',$2,true)`, p.TenantID, p.SubjectID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM chat_saved_item WHERE tenant_id=$1 AND home_tenant_id=$2 AND person_id=$3`, p.TenantID, p.TenantID, p.SubjectID)
		return err
	})
}

func (s *Store) DeliverSavedReminder(ctx context.Context, p chat.Principal, item chat.SavedItem, sink chat.SavedReminderSink) error {
	if item.PersonID != p.SubjectID || item.HomeTenantID != p.TenantID {
		return chat.ErrPermissionDenied
	}
	if sink == nil {
		return chat.ErrUnavailable
	}
	return s.chatsaveTx(ctx, p, item.TenantID, func(tx dbport.Tx) error {
		var due time.Time
		err := tx.QueryRow(ctx, `SELECT due_at FROM chat_saved_item WHERE tenant_id=$1 AND home_tenant_id=$2 AND person_id=$3 AND conversation_id=$4 AND post_id=$5 AND host_tenant_id=$6 AND state='todo' AND due_at<=now() AND reminded_at IS NULL FOR UPDATE`, p.TenantID, p.TenantID, p.SubjectID, item.ConversationID, item.PostID, item.TenantID).Scan(&due)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// A changed due time cannot deliver a stale projection from a prior read.
		if item.DueAt == nil || !item.DueAt.Equal(due) {
			return chat.ErrConflict
		}
		identity, err := json.Marshal([]string{item.TenantID, p.TenantID, p.SubjectID, item.ConversationID, item.PostID, due.UTC().Format(time.RFC3339Nano)})
		if err != nil {
			return err
		}
		key := fmt.Sprintf("saved:%x", sha256.Sum256(identity))
		item.Post, item.Channel, item.Note = nil, "", ""
		if err := sink.NotifySaved(ctx, p, item, key); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE chat_saved_item SET reminded_at=now() WHERE tenant_id=$1 AND home_tenant_id=$2 AND person_id=$3 AND conversation_id=$4 AND post_id=$5 AND host_tenant_id=$6`, p.TenantID, p.TenantID, p.SubjectID, item.ConversationID, item.PostID, item.TenantID)
		return err
	})
}
