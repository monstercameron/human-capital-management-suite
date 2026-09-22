package chatstore

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// RecipientStateStore keeps personal chat state in the chat database. The
// authenticated home tenant and subject are supplied by the application port.
type RecipientStateStore struct{ store *Store }

func NewRecipientStateStore(s *Store) *RecipientStateStore { return &RecipientStateStore{store: s} }

// countScanLimit bounds the recipient counter. The counter used to scan every
// post in a conversation and expand references_json for each row, which made a
// badge read as expensive as the whole channel. The candidate set is now the
// most recent countScanLimit qualifying posts, walked backwards on the
// (tenant_id, conversation_id, sequence DESC) index, so a large channel costs a
// bounded amount of work. A member who is further behind than this sees the cap
// rather than an exact number, which is what an unread badge needs.
const countScanLimit = 5000

func (r *RecipientStateStore) Counts(ctx context.Context, id chatrecipient.Identity) (chatrecipient.Counts, error) {
	var out chatrecipient.Counts
	err := r.store.RunTenantTx(ctx, id.HostTenantID, func(tx dbport.Tx) error {
		var unread, mentions int64
		// The mention predicate uses jsonb containment so the chat_post_references
		// GIN index can serve it; the earlier jsonb_array_elements lateral could
		// not be indexed.
		err := tx.QueryRow(ctx, `WITH candidate AS (
            SELECT p.sequence, p.references_json, (p.sequence>COALESCE(c.last_sequence,0)) AS unseen
            FROM chat_post p
            JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id
                AND m.home_tenant_id=$3 AND m.member_id=$4 AND m.state='active'
            LEFT JOIN chat_cursor c ON c.tenant_id=p.tenant_id AND c.conversation_id=p.conversation_id
                AND c.home_tenant_id=$3 AND c.member_id=$4
            WHERE p.tenant_id=$1 AND p.conversation_id=$2
                AND (p.sequence>COALESCE(c.last_sequence,0) OR (p.revision>1 AND p.updated_at>c.updated_at))
                AND p.tombstoned=false AND NOT (p.author_home_tenant_id=$3 AND p.author_id=$4)
                AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at))
            ORDER BY p.sequence DESC
            LIMIT $5
        ) SELECT count(*) FILTER (WHERE unseen), count(*) FILTER (
            WHERE references_json @> jsonb_build_array(jsonb_build_object('Kind','PERSON_MENTION','TenantID',$3::text,'ID',$4::text))
        ) FROM candidate`, id.HostTenantID, id.ConversationID, id.HomeTenantID, id.SubjectID, countScanLimit).Scan(&unread, &mentions)
		if err != nil {
			return err
		}
		out = chatrecipient.Counts{Unread: uint64(unread), Mentions: uint64(mentions)}
		return nil
	})
	return out, err
}

func (r *RecipientStateStore) Follow(ctx context.Context, id chatrecipient.Identity, root string) (chatrecipient.Follow, error) {
	out := chatrecipient.Follow{RootPostID: root, Revision: 1}
	err := r.store.RunTenantTx(ctx, id.HostTenantID, func(tx dbport.Tx) error {
		if err := recipientMember(ctx, tx, id); err != nil {
			return err
		}
		if err := visibleRoot(ctx, tx, id, root); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `SELECT followed,revision FROM chat_thread_follow WHERE tenant_id=$1 AND home_tenant_id=$2 AND member_id=$3 AND conversation_id=$4 AND root_post_id=$5`, id.HostTenantID, id.HomeTenantID, id.SubjectID, id.ConversationID, root).Scan(&out.Followed, &out.Revision)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		return err
	})
	return out, err
}
func (r *RecipientStateStore) PutFollow(ctx context.Context, id chatrecipient.Identity, x chatrecipient.Follow, expected uint64) (chatrecipient.Follow, error) {
	out := x
	err := r.store.RunTenantTx(ctx, id.HostTenantID, func(tx dbport.Tx) error {
		if err := recipientMember(ctx, tx, id); err != nil {
			return err
		}
		if err := visibleRoot(ctx, tx, id, x.RootPostID); err != nil {
			return err
		}
		var rev int64
		err := tx.QueryRow(ctx, `WITH updated AS (
            UPDATE chat_thread_follow SET followed=$6,revision=revision+1,updated_at=now()
            WHERE tenant_id=$1 AND home_tenant_id=$2 AND member_id=$3 AND conversation_id=$4 AND root_post_id=$5 AND revision=$7 RETURNING revision
        ), inserted AS (
            INSERT INTO chat_thread_follow(tenant_id,home_tenant_id,member_id,conversation_id,root_post_id,followed,revision)
            SELECT $1,$2,$3,$4,$5,$6,2 WHERE $7=1 AND NOT EXISTS (SELECT 1 FROM updated)
            ON CONFLICT DO NOTHING RETURNING revision
        ) SELECT revision FROM updated UNION ALL SELECT revision FROM inserted`, id.HostTenantID, id.HomeTenantID, id.SubjectID, id.ConversationID, x.RootPostID, x.Followed, expected).Scan(&rev)
		if errors.Is(err, dbport.ErrNoRows) {
			return chat.ErrConflict
		}
		if err != nil {
			return err
		}
		out.Revision = uint64(rev)
		return nil
	})
	return out, err
}
func visibleRoot(ctx context.Context, tx dbport.Tx, id chatrecipient.Identity, root string) error {
	var parent string
	err := tx.QueryRow(ctx, `SELECT p.parent_id FROM chat_post p JOIN chat_membership m
        ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=$4 AND m.member_id=$5 AND m.state='active'
        WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.id=$3
        AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at))`, id.HostTenantID, id.ConversationID, root, id.HomeTenantID, id.SubjectID).Scan(&parent)
	if errors.Is(err, dbport.ErrNoRows) {
		return chat.ErrNotFound
	}
	if err != nil {
		return err
	}
	if parent != "" {
		return chat.ErrInvalidArgument
	}
	return nil
}
func recipientMember(ctx context.Context, tx dbport.Tx, id chatrecipient.Identity) error {
	var active string
	err := tx.QueryRow(ctx, `SELECT state FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4 AND state='active'`, id.HostTenantID, id.ConversationID, id.HomeTenantID, id.SubjectID).Scan(&active)
	if errors.Is(err, dbport.ErrNoRows) {
		return chat.ErrPermissionDenied
	}
	return err
}

func (r *RecipientStateStore) Sidebar(ctx context.Context, home, subject string) (chatrecipient.Sidebar, error) {
	out := chatrecipient.Sidebar{Layout: []byte(`{"sections":[]}`), Revision: 1}
	err := r.store.RunTenantTx(ctx, home, func(tx dbport.Tx) error {
		err := tx.QueryRow(ctx, `SELECT layout,revision FROM chat_personal_sidebar WHERE tenant_id=$1 AND member_id=$2`, home, subject).Scan(&out.Layout, &out.Revision)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		return err
	})
	return out, err
}
func (r *RecipientStateStore) PutSidebar(ctx context.Context, home, subject string, x chatrecipient.Sidebar, expected uint64) (chatrecipient.Sidebar, error) {
	out := x
	err := r.store.RunTenantTx(ctx, home, func(tx dbport.Tx) error {
		var rev int64
		err := tx.QueryRow(ctx, `WITH updated AS (
            UPDATE chat_personal_sidebar SET layout=$3,revision=revision+1,updated_at=now()
            WHERE tenant_id=$1 AND member_id=$2 AND revision=$4 RETURNING revision
        ), inserted AS (
            INSERT INTO chat_personal_sidebar(tenant_id,member_id,layout,revision)
            SELECT $1,$2,$3,2 WHERE $4=1 AND NOT EXISTS (SELECT 1 FROM updated)
            ON CONFLICT DO NOTHING RETURNING revision
        ) SELECT revision FROM updated UNION ALL SELECT revision FROM inserted`, home, subject, x.Layout, expected).Scan(&rev)
		if errors.Is(err, dbport.ErrNoRows) {
			return chat.ErrConflict
		}
		if err != nil {
			return err
		}
		out.Revision = uint64(rev)
		return nil
	})
	return out, err
}
func (r *RecipientStateStore) QuietHours(ctx context.Context, home, subject string) (chatrecipient.QuietHours, error) {
	out := chatrecipient.QuietHours{Timezone: "UTC", StartMinute: 1320, EndMinute: 420, Revision: 1}
	err := r.store.RunTenantTx(ctx, home, func(tx dbport.Tx) error {
		err := tx.QueryRow(ctx, `SELECT timezone,start_minute,end_minute,enabled,revision FROM chat_quiet_hours WHERE tenant_id=$1 AND member_id=$2`, home, subject).Scan(&out.Timezone, &out.StartMinute, &out.EndMinute, &out.Enabled, &out.Revision)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		return err
	})
	return out, err
}
func (r *RecipientStateStore) PutQuietHours(ctx context.Context, home, subject string, x chatrecipient.QuietHours, expected uint64) (chatrecipient.QuietHours, error) {
	out := x
	err := r.store.RunTenantTx(ctx, home, func(tx dbport.Tx) error {
		var rev int64
		err := tx.QueryRow(ctx, `WITH updated AS (
            UPDATE chat_quiet_hours SET timezone=$3,start_minute=$4,end_minute=$5,enabled=$6,revision=revision+1,updated_at=now()
            WHERE tenant_id=$1 AND member_id=$2 AND revision=$7 RETURNING revision
        ), inserted AS (
            INSERT INTO chat_quiet_hours(tenant_id,member_id,timezone,start_minute,end_minute,enabled,revision)
            SELECT $1,$2,$3,$4,$5,$6,2 WHERE $7=1 AND NOT EXISTS (SELECT 1 FROM updated)
            ON CONFLICT DO NOTHING RETURNING revision
        ) SELECT revision FROM updated UNION ALL SELECT revision FROM inserted`, home, subject, x.Timezone, x.StartMinute, x.EndMinute, x.Enabled, expected).Scan(&rev)
		if errors.Is(err, dbport.ErrNoRows) {
			return chat.ErrConflict
		}
		if err != nil {
			return err
		}
		out.Revision = uint64(rev)
		return nil
	})
	return out, err
}
