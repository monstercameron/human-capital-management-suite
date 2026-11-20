package chatstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3809. Which public channels may publish message events to signed
// outbound webhooks, and the committed posts of those channels in order.
//
// The post is read from chat_post when it is delivered rather than copied into
// a queue, so an edited message goes out as it reads now and a deleted one does
// not go out at all. Only a public, active channel with an allowance row is
// read; a direct message, a group, a private channel, a post addressed to a
// restricted audience and a post from before the allowance are never returned.
// The caller decides who may change the allowance; the store scopes by tenant.

const (
	// WebhookPostBatch bounds one read of committed posts.
	WebhookPostBatch = 200
	// WebhookChannelListLimit bounds the allowed channels of one company.
	WebhookChannelListLimit = 500
	// webhookAuditAllowed and webhookAuditRemoved name the audit entries.
	webhookAuditAllowed = "chat.webhook_channel.allowed"
	webhookAuditRemoved = "chat.webhook_channel.removed"
)

// WebhookChannel is one channel an administrator allowed to publish.
type WebhookChannel struct {
	TenantID, ConversationID, Name, AllowedBy string
	AllowedAt                                 time.Time
}

// WebhookPost is one committed post of an allowed channel.
type WebhookPost struct {
	OutboxID                                                   int64
	TenantID, ConversationID, ConversationName, PostID, Author string
	ParentID, Body                                             string
	Sequence, Revision                                         int64
	PostedAt                                                   time.Time
}

// WebhookPostPage is the eligible posts after a cursor and the cursor to
// resume from. Through advances past events that were examined and skipped, so
// a busy company with one quiet allowed channel is not rescanned on every read.
type WebhookPostPage struct {
	Posts   []WebhookPost
	Through int64
}

// AllowWebhookChannel lets a public, active channel publish. It reports whether
// the allowance is new. Any other kind of conversation is invalid; a channel
// that does not exist is not found.
func (s *Store) AllowWebhookChannel(ctx context.Context, tenantID, conversationID, actor string) (bool, error) {
	if s == nil || s.pool == nil || ctx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(actor) == "" {
		return false, chat.ErrInvalidArgument
	}
	added := false
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var kind, lifecycle string
		err := tx.QueryRow(ctx, `SELECT kind,lifecycle FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR SHARE`, tenantID, conversationID).Scan(&kind, &lifecycle)
		if errors.Is(err, dbport.ErrNoRows) {
			return chat.ErrNotFound
		}
		if err != nil {
			return err
		}
		if kind != string(chat.PublicChannel) || lifecycle != "ACTIVE" {
			return chat.ErrInvalidArgument
		}
		n, err := tx.Exec(ctx, `INSERT INTO chat_webhook_channel(tenant_id,conversation_id,allowed_by) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, tenantID, conversationID, actor)
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		added = true
		return webhookAudit(ctx, tx, tenantID, actor, conversationID, webhookAuditAllowed)
	})
	return added, err
}

// RemoveWebhookChannel stops a channel publishing. It reports whether an
// allowance was removed; removing one that is not there changes nothing.
func (s *Store) RemoveWebhookChannel(ctx context.Context, tenantID, conversationID, actor string) (bool, error) {
	if s == nil || s.pool == nil || ctx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(actor) == "" {
		return false, chat.ErrInvalidArgument
	}
	removed := false
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `DELETE FROM chat_webhook_channel WHERE tenant_id=$1 AND conversation_id=$2`, tenantID, conversationID)
		if err != nil || n == 0 {
			return err
		}
		removed = true
		return webhookAudit(ctx, tx, tenantID, actor, conversationID, webhookAuditRemoved)
	})
	return removed, err
}

// ListWebhookChannels reads the allowed channels of one company, oldest first.
func (s *Store) ListWebhookChannels(ctx context.Context, tenantID string) ([]WebhookChannel, error) {
	if s == nil || s.pool == nil || ctx == nil || strings.TrimSpace(tenantID) == "" {
		return nil, chat.ErrInvalidArgument
	}
	out := []WebhookChannel{}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT w.conversation_id,c.name,w.allowed_by,w.created_at FROM chat_webhook_channel w
 JOIN chat_conversation c ON c.tenant_id=w.tenant_id AND c.id=w.conversation_id
 WHERE w.tenant_id=$1 ORDER BY w.created_at,w.conversation_id LIMIT $2`, tenantID, WebhookChannelListLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			w := WebhookChannel{TenantID: tenantID}
			if err := rows.Scan(&w.ConversationID, &w.Name, &w.AllowedBy, &w.AllowedAt); err != nil {
				return err
			}
			w.AllowedAt = w.AllowedAt.UTC()
			out = append(out, w)
		}
		return rows.Err()
	})
	return out, err
}

// ListWebhookPosts reads the committed posts of allowed channels after an outbox
// position, oldest first. Through is the highest position examined.
func (s *Store) ListWebhookPosts(ctx context.Context, tenantID string, after int64, limit int) (WebhookPostPage, error) {
	if s == nil || s.pool == nil || ctx == nil || strings.TrimSpace(tenantID) == "" || after < 0 {
		return WebhookPostPage{}, chat.ErrInvalidArgument
	}
	if limit <= 0 || limit > WebhookPostBatch {
		limit = WebhookPostBatch
	}
	page := WebhookPostPage{Posts: []WebhookPost{}, Through: after}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `WITH page AS (
   SELECT id,payload->>'`+OutboxKeyTargetID+`' AS post_id FROM chat_outbox
   WHERE tenant_id=$1 AND id>$2 AND event_type='post.created' ORDER BY id LIMIT $3)
 SELECT pg.id,
   (p.id IS NOT NULL AND w.conversation_id IS NOT NULL AND c.kind='PUBLIC_CHANNEL' AND c.lifecycle='ACTIVE'
     AND NOT p.tombstoned AND p.created_at>=w.created_at
     AND NOT EXISTS (SELECT 1 FROM chat_post_audience au WHERE au.tenant_id=p.tenant_id AND au.post_id=p.id AND au.restricted)) AS ok,
   COALESCE(p.conversation_id,''),COALESCE(c.name,''),COALESCE(p.id,''),COALESCE(p.author_id,''),COALESCE(p.parent_id,''),
   COALESCE(p.body,''),COALESCE(p.sequence,0),COALESCE(p.revision,0),COALESCE(p.created_at,now())
 FROM page pg
 LEFT JOIN chat_post p ON p.tenant_id=$1 AND p.id=pg.post_id
 LEFT JOIN chat_conversation c ON c.tenant_id=p.tenant_id AND c.id=p.conversation_id
 LEFT JOIN chat_webhook_channel w ON w.tenant_id=p.tenant_id AND w.conversation_id=p.conversation_id
 ORDER BY pg.id`, tenantID, after, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var ok bool
			w := WebhookPost{TenantID: tenantID}
			if err := rows.Scan(&id, &ok, &w.ConversationID, &w.ConversationName, &w.PostID, &w.Author, &w.ParentID, &w.Body, &w.Sequence, &w.Revision, &w.PostedAt); err != nil {
				return err
			}
			if id > page.Through {
				page.Through = id
			}
			if ok {
				w.OutboxID, w.PostedAt = id, w.PostedAt.UTC()
				page.Posts = append(page.Posts, w)
			}
		}
		return rows.Err()
	})
	return page, err
}

func webhookAudit(ctx context.Context, tx dbport.Tx, tenantID, actor, conversationID, action string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "chat-audit:"+tenantID); err != nil {
		return err
	}
	var sequence uint64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(sequence),0)+1 FROM chat_audit_event WHERE tenant_id=$1`, tenantID).Scan(&sequence); err != nil {
		return err
	}
	e := chatrecords.AuditEvent{TenantID: tenantID, EventID: fmt.Sprintf("webhook-channel:%s:%d", conversationID, sequence), Sequence: sequence, ActorID: actor, Action: action, TargetType: "chat_webhook_channel", TargetID: conversationID, Reason: action, PolicyEvidence: "webhook-channel:" + tenantID, At: time.Now().UTC()}
	_, err := tx.Exec(ctx, `INSERT INTO chat_audit_event(tenant_id,event_id,sequence,actor_id,action,target_type,target_id,prior_revision,reason,policy_evidence,at_time,digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, e.TenantID, e.EventID, e.Sequence, e.ActorID, e.Action, e.TargetType, e.TargetID, e.PriorRevision, e.Reason, e.PolicyEvidence, e.At, chatrecords.DigestEvent(e))
	return err
}
