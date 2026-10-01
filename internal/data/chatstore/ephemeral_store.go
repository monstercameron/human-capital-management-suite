package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// DurableEphemeralStore persists recipient-only chat envelopes outside chat_post
// and chat_outbox. Its stream offsets share the outbox sequence without
// creating an outbox row, so a signed chat cursor can replay either source.
type DurableEphemeralStore struct{ store *Store }

// NewDurableEphemeralStore creates the chat-owned ephemeral envelope store.
func NewDurableEphemeralStore(store *Store) *DurableEphemeralStore {
	return &DurableEphemeralStore{store: store}
}

var _ chat.EphemeralStore = (*DurableEphemeralStore)(nil)

// PutEphemeral writes one invoker-only envelope with a globally ordered stream
// offset. The row is independent of history, search, unread, and outbox views.
func (s *DurableEphemeralStore) PutEphemeral(ctx context.Context, p chat.EphemeralPost) (chat.EphemeralPost, error) {
	if s == nil || s.store == nil || !validEphemeral(p) {
		return chat.EphemeralPost{}, chat.ErrInvalidArgument
	}
	fp, err := ephemeralFingerprint(p)
	if err != nil {
		return chat.EphemeralPost{}, err
	}
	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return chat.EphemeralPost{}, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, p.TenantID); err != nil {
		return chat.EphemeralPost{}, err
	}
	if err = fenceContextWrite(ctx, tx, p.TenantID, p.ConversationID); err != nil {
		return chat.EphemeralPost{}, err
	}
	if err = lockEphemeralRecipient(ctx, tx, p.TenantID, p.ConversationID, p.RecipientHomeTenantID, p.RecipientSubjectID); err != nil {
		return chat.EphemeralPost{}, err
	}
	if existing, readErr := readEphemeralByID(ctx, tx, p.TenantID, p.ConversationID, p.ID); readErr == nil {
		if existing.fingerprint != fp {
			return chat.EphemeralPost{}, chat.ErrConflict
		}
		if err = tx.Commit(ctx); err != nil {
			return chat.EphemeralPost{}, err
		}
		return existing.post, nil
	} else if !errors.Is(readErr, dbport.ErrNoRows) {
		return chat.EphemeralPost{}, readErr
	}
	var offset int64
	if err = tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('chat_outbox','id'))`).Scan(&offset); err != nil {
		return chat.EphemeralPost{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_ephemeral_post(
		id,tenant_id,conversation_id,stream_offset,thread_id,recipient_home_tenant_id,recipient_subject_id,
		body,only_visible_to_you,created_at,expires_at,durable_copy_conversation_id,durable_copy_post_id,thread_link,fingerprint
	) VALUES($1,$2,$3,$4,$5,$6,$7,$8,true,$9,$10,$11,$12,$13,$14)`,
		p.ID, p.TenantID, p.ConversationID, offset, p.ThreadID, p.RecipientHomeTenantID, p.RecipientSubjectID,
		p.Body, p.CreatedAt, p.ExpiresAt, p.DurableCopyConversationID, p.DurableCopyPostID, p.ThreadLink, fp)
	if err != nil {
		return chat.EphemeralPost{}, err
	}
	p.Sequence = uint64(offset)
	if err = tx.Commit(ctx); err != nil {
		return chat.EphemeralPost{}, err
	}
	return p, nil
}

// ListEphemeral returns unexpired envelopes for the current active recipient.
// Its watermark advances over hidden and expired rows to support bounded scans.
func (s *DurableEphemeralStore) ListEphemeral(ctx context.Context, principal chat.Principal, tenantID, conversationID string, after uint64, limit int) ([]chat.EphemeralPost, uint64, error) {
	if s == nil || s.store == nil || principal.TenantID == "" || principal.SubjectID == "" || tenantID == "" || conversationID == "" || after > uint64(^uint64(0)>>1) {
		return nil, after, chat.ErrInvalidArgument
	}
	limit, err := ephemeralPageLimit(limit)
	if err != nil {
		return nil, after, err
	}
	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, after, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, tenantID); err != nil {
		return nil, after, err
	}
	if err = lockEphemeralRecipient(ctx, tx, tenantID, conversationID, principal.TenantID, principal.SubjectID); err != nil {
		return nil, after, err
	}
	rows, err := tx.Query(ctx, `SELECT e.id,e.stream_offset,e.thread_id,e.recipient_home_tenant_id,e.recipient_subject_id,
		CASE WHEN recipient_home_tenant_id=$3 AND recipient_subject_id=$4 AND expires_at>now() AND EXISTS (
			SELECT 1 FROM chat_membership m WHERE m.tenant_id=e.tenant_id AND m.conversation_id=e.conversation_id
			AND m.home_tenant_id=$3 AND m.member_id=$4 AND m.state='active'
			AND (m.history_visibility='FULL_HISTORY' OR e.created_at>=m.joined_at)
		) THEN body ELSE NULL END,
		e.created_at,e.expires_at,e.durable_copy_conversation_id,e.durable_copy_post_id,e.thread_link
		FROM chat_ephemeral_post e WHERE e.tenant_id=$1 AND e.conversation_id=$2 AND e.stream_offset>$5
		ORDER BY e.stream_offset LIMIT $6`, tenantID, conversationID, principal.TenantID, principal.SubjectID, int64(after), limit)
	if err != nil {
		return nil, after, err
	}
	defer rows.Close()
	out := make([]chat.EphemeralPost, 0, limit)
	next := after
	for rows.Next() {
		var post chat.EphemeralPost
		var seq int64
		var body *string
		if err = rows.Scan(&post.ID, &seq, &post.ThreadID, &post.RecipientHomeTenantID, &post.RecipientSubjectID, &body,
			&post.CreatedAt, &post.ExpiresAt, &post.DurableCopyConversationID, &post.DurableCopyPostID, &post.ThreadLink); err != nil {
			return nil, after, err
		}
		next = uint64(seq)
		if body == nil || post.RecipientHomeTenantID != principal.TenantID || post.RecipientSubjectID != principal.SubjectID {
			continue
		}
		post.TenantID = tenantID
		post.ConversationID = conversationID
		post.Sequence = uint64(seq)
		post.Body = *body
		post.OnlyVisibleToYou = true
		out = append(out, post)
	}
	if err = rows.Err(); err != nil {
		return nil, after, err
	}
	return out, next, tx.Commit(ctx)
}

// PruneExpiredEphemeral deletes a bounded batch whose recipient-view lifetime
// ended. Durable DM copies remain governed by ordinary chat retention.
func (s *DurableEphemeralStore) PruneExpiredEphemeral(ctx context.Context, tenantID string, before time.Time, limit int) (int64, error) {
	if s == nil || s.store == nil || tenantID == "" || before.IsZero() || limit <= 0 || limit > 1000 {
		return 0, chat.ErrInvalidArgument
	}
	var removed int64
	err := s.store.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var err error
		removed, err = tx.Exec(ctx, `DELETE FROM chat_ephemeral_post WHERE tenant_id=$1 AND (conversation_id,id) IN (
			SELECT conversation_id,id FROM chat_ephemeral_post WHERE tenant_id=$1 AND expires_at<=$2 ORDER BY expires_at,conversation_id,id LIMIT $3
		)`, tenantID, before, limit)
		return err
	})
	return removed, err
}

type ephemeralRecord struct {
	post        chat.EphemeralPost
	offset      int64
	fingerprint string
}

func readEphemeralByID(ctx context.Context, tx dbport.Tx, tenantID, conversationID, id string) (ephemeralRecord, error) {
	var row ephemeralRecord
	var visible bool
	err := tx.QueryRow(ctx, `SELECT id,tenant_id,conversation_id,stream_offset,thread_id,recipient_home_tenant_id,
		recipient_subject_id,body,only_visible_to_you,created_at,expires_at,durable_copy_conversation_id,
		durable_copy_post_id,thread_link,fingerprint FROM chat_ephemeral_post
		WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3`, tenantID, conversationID, id).Scan(
		&row.post.ID, &row.post.TenantID, &row.post.ConversationID, &row.offset, &row.post.ThreadID,
		&row.post.RecipientHomeTenantID, &row.post.RecipientSubjectID, &row.post.Body, &visible,
		&row.post.CreatedAt, &row.post.ExpiresAt, &row.post.DurableCopyConversationID,
		&row.post.DurableCopyPostID, &row.post.ThreadLink, &row.fingerprint)
	if err != nil {
		return ephemeralRecord{}, err
	}
	row.post.Sequence = uint64(row.offset)
	row.post.OnlyVisibleToYou = visible
	return row, nil
}

func lockEphemeralRecipient(ctx context.Context, tx dbport.Tx, tenantID, conversationID, homeTenantID, subjectID string) error {
	var member string
	err := tx.QueryRow(ctx, `SELECT member_id FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2
		AND home_tenant_id=$3 AND member_id=$4 AND state='active' FOR UPDATE`, tenantID, conversationID, homeTenantID, subjectID).Scan(&member)
	if errors.Is(err, dbport.ErrNoRows) {
		return chat.ErrPermissionDenied
	}
	return err
}

func validEphemeral(p chat.EphemeralPost) bool {
	return p.ID != "" && p.TenantID != "" && p.ConversationID != "" && p.ThreadID != "" &&
		p.RecipientHomeTenantID != "" && p.RecipientSubjectID != "" && strings.TrimSpace(p.Body) != "" &&
		len(p.Body) <= 4000 && p.OnlyVisibleToYou && !p.CreatedAt.IsZero() && p.ExpiresAt.After(p.CreatedAt) &&
		!p.ExpiresAt.After(p.CreatedAt.Add(24*time.Hour)) && p.DurableCopyConversationID != "" &&
		p.DurableCopyPostID != "" && p.ThreadLink != ""
}

func ephemeralFingerprint(p chat.EphemeralPost) (string, error) {
	request := struct {
		TenantID, ConversationID, ThreadID                             string
		RecipientHomeTenantID, RecipientSubjectID                      string
		Body, DurableCopyConversationID, DurableCopyPostID, ThreadLink string
		OnlyVisibleToYou                                               bool
	}{p.TenantID, p.ConversationID, p.ThreadID, p.RecipientHomeTenantID, p.RecipientSubjectID,
		p.Body, p.DurableCopyConversationID, p.DurableCopyPostID, p.ThreadLink, p.OnlyVisibleToYou}
	b, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("chatstore: encode ephemeral fingerprint: %w", err)
	}
	return fingerprint(string(b)), nil
}

func ephemeralPageLimit(limit int) (int, error) {
	if limit == 0 {
		return 100, nil
	}
	if limit < 0 || limit > 100 {
		return 0, chat.ErrInvalidArgument
	}
	return limit, nil
}
