package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type mergedChatEvent struct {
	id      int64
	kind    string
	payload []byte
	created time.Time
}

// chatstateStatusChangedEvent is the outbox event type chatstateEvent writes
// when a channel is locked, archived, reopened or made announcements only.
const chatstateStatusChangedEvent = "conversation.status_changed"

// statusChangedConversation reads the conversation a status change belongs to,
// as it is now. The outbox row of a status change carries the status alone; a
// watcher is sent the whole conversation so the update cannot blank its name
// or kind on the page.
func statusChangedConversation(ctx context.Context, tx dbport.Tx, tenantID, conversationID string) (chat.Conversation, error) {
	query, err := chatscaleActivityQuery(ctx, tx, getConversationRow)
	if err != nil {
		return chat.Conversation{}, err
	}
	var c chat.Conversation
	var kind, life string
	var rev, members int64
	if err = tx.QueryRow(ctx, query, tenantID, conversationID).Scan(&c.ID, &c.TenantID, &kind, &c.Name, &c.OwnerID, &rev, &life, &members, &c.LastActivityAt); err != nil {
		return chat.Conversation{}, err
	}
	c.Kind = chat.ConversationKind(kind)
	c.Revision = uint64(rev)
	c.Archived = life == "ARCHIVED"
	c.MemberCount = uint32(members)
	return c, nil
}

func (s *Adapter) readConversationEventPage(ctx context.Context, request chat.WatchConversationRequest, after uint64, limit int) (EventPage, error) {
	if s == nil || s.pool == nil || request.TenantID == "" || request.ConversationID == "" || request.Principal.TenantID == "" || request.Principal.SubjectID == "" || after > uint64(^uint64(0)>>1) {
		return EventPage{}, chat.ErrInvalidArgument
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return EventPage{}, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, request.TenantID); err != nil {
		return EventPage{}, err
	}
	machine, identityErr := machineActor(ctx, request.Principal.TenantID, request.Principal.SubjectID)
	if identityErr != nil {
		return EventPage{}, chat.ErrPermissionDenied
	}
	var history string
	var joined time.Time
	if machine {
		if request.Principal.TenantID != request.TenantID {
			return EventPage{}, chat.ErrPermissionDenied
		}
		history = string(chat.FromJoin)
		err = tx.QueryRow(ctx, `SELECT created_at FROM chat_app_installation WHERE tenant_id=$1 AND conversation_id=$2 AND app_id=$3 AND id=tenant_id||':'||conversation_id||':'||app_id AND status='ACTIVE' AND version>0 AND created_at<=now() AND 'chat.posts.read'=ANY(granted_scopes)`, request.TenantID, request.ConversationID, request.Principal.SubjectID).Scan(&joined)
	} else {
		err = tx.QueryRow(ctx, `SELECT history_visibility,joined_at FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4 AND state='active' FOR SHARE`, request.TenantID, request.ConversationID, request.Principal.TenantID, request.Principal.SubjectID).Scan(&history, &joined)
	}
	if err != nil {
		if machine && !errors.Is(err, dbport.ErrNoRows) {
			return EventPage{}, fmt.Errorf("machine watch installation: %w: %w", chat.ErrUnavailable, err)
		}
		return EventPage{}, chat.ErrPermissionDenied
	}
	ephemeralPayload := `CASE WHEN NOT $5 AND recipient_home_tenant_id=$6 AND recipient_subject_id=$7
		AND expires_at>now() AND ($8='FULL_HISTORY' OR created_at>=$9)
		THEN jsonb_build_object('ID',id,'TenantID',tenant_id,'ConversationID',conversation_id,'ThreadID',thread_id,
		'RecipientHomeTenantID',recipient_home_tenant_id,'RecipientSubjectID',recipient_subject_id,'Body',body,
		'OnlyVisibleToYou',only_visible_to_you,'CreatedAt',created_at,'ExpiresAt',expires_at,
		'DurableCopyConversationID',durable_copy_conversation_id,'DurableCopyPostID',durable_copy_post_id,
		'ThreadLink',thread_link,'Sequence',stream_offset) ELSE '{}'::jsonb END`
	ephemeralRows := `SELECT stream_offset AS id,'ephemeral.post'::text AS event_type,` + ephemeralPayload + ` AS payload,created_at
		FROM chat_ephemeral_post WHERE tenant_id=$1 AND conversation_id=$2 AND stream_offset>$3`
	args := []any{request.TenantID, request.ConversationID, int64(after), limit, machine, request.Principal.TenantID, request.Principal.SubjectID, history, joined}
	var outboxRows string
	if machine {
		outboxRows = `SELECT o.id,o.event_type,o.payload,o.created_at FROM chat_outbox o JOIN chat_post p
			ON p.id=(o.payload->>'` + OutboxKeyTargetID + `') AND p.tenant_id=o.tenant_id AND p.conversation_id=$2 AND p.created_at>=$9
			WHERE o.tenant_id=$1 AND o.id>$3 AND o.payload->>'` + OutboxKeyConversationID + `'=$2
			AND o.created_at>=$9 AND o.event_type IN ('post.created','post.edited','post.deleted')`
	} else {
		outboxRows = `SELECT id,event_type,payload,created_at FROM chat_outbox WHERE tenant_id=$1 AND id>$3
			AND payload->>'` + OutboxKeyConversationID + `'=$2`
	}
	query := chatscaleTimelineSQL(outboxRows, ephemeralRows)
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return EventPage{}, err
	}
	defer rows.Close()
	page := EventPage{Events: make([]chat.WatchEvent, 0, limit), EphemeralPosts: make([]chat.EphemeralPost, 0, 1), NextOffset: after}
	// statusChanges holds the positions in page.Events of status changes. Their
	// conversation value is read once after the rows are closed.
	var statusChanges []int
	scanned := 0
	for rows.Next() {
		scanned++
		var row mergedChatEvent
		if err = rows.Scan(&row.id, &row.kind, &row.payload, &row.created); err != nil {
			return EventPage{}, err
		}
		page.NextOffset = uint64(row.id)
		if row.kind == "ephemeral.post" {
			if len(row.payload) == 0 || string(row.payload) == "{}" {
				continue
			}
			var post chat.EphemeralPost
			if err = json.Unmarshal(row.payload, &post); err != nil {
				return EventPage{}, err
			}
			if post.OnlyVisibleToYou && post.RecipientHomeTenantID == request.Principal.TenantID &&
				post.RecipientSubjectID == request.Principal.SubjectID && time.Now().Before(post.ExpiresAt) {
				page.EphemeralPosts = append(page.EphemeralPosts, post)
			}
			continue
		}
		if history != string(chat.FullHistory) && row.created.Before(joined) {
			continue
		}
		if machine && row.kind != "post.created" && row.kind != "post.edited" && row.kind != "post.deleted" {
			continue
		}
		event := chat.ConversationEvent{Sequence: uint64(row.id)}
		switch row.kind {
		case "post.created":
			event.Kind = chat.PostCreated
		case "post.edited":
			event.Kind = chat.PostEdited
		case "post.deleted":
			event.Kind = chat.PostDeleted
		case "conversation.created", "conversation.updated":
			event.Kind = chat.ConversationUpdated
		case chatstateStatusChangedEvent:
			// CHATBUG-075: a status change (locked, archived, announcements only)
			// used to be dropped here, so another person's open page learned of it
			// only by polling. It is delivered as a conversation update; the page
			// re-reads its own permissions when it sees one.
			event.Kind = chat.ConversationUpdated
		case "membership.added", "membership.removed":
			event.Kind = chat.MembershipChanged
			event.Removed = row.kind == "membership.removed"
		case "reaction.added", "reaction.removed":
			event.Kind = chat.ReactionChanged
			event.Removed = row.kind == "reaction.removed"
		case "pin.added", "pin.removed":
			event.Kind = chat.PinChanged
			event.Removed = row.kind == "pin.removed"
		default:
			continue
		}
		var envelope OutboxEnvelope
		if err = json.Unmarshal(row.payload, &envelope); err != nil {
			return EventPage{}, err
		}
		if envelope.EventSequence > 0 {
			event.Sequence = uint64(envelope.EventSequence)
		}
		if strings.HasPrefix(row.kind, "post.") {
			var post chat.Post
			if err = json.Unmarshal(envelope.Value, &post); err != nil {
				return EventPage{}, err
			}
			if history != string(chat.FullHistory) && post.CreatedAt.Before(joined) {
				continue
			}
			event.Post = &post
			event.Revision = post.Revision
		} else {
			event.Revision = envelope.Revision
			switch event.Kind {
			case chat.ConversationUpdated:
				if row.kind == chatstateStatusChangedEvent {
					// The envelope holds the status, not the conversation.
					statusChanges = append(statusChanges, len(page.Events))
					break
				}
				var value chat.Conversation
				if err = json.Unmarshal(envelope.Value, &value); err != nil {
					return EventPage{}, err
				}
				event.Conversation = &value
			case chat.MembershipChanged:
				var value chat.Membership
				if err = json.Unmarshal(envelope.Value, &value); err != nil {
					return EventPage{}, err
				}
				event.Membership = &value
			case chat.ReactionChanged:
				var value chat.Reaction
				if err = json.Unmarshal(envelope.Value, &value); err != nil {
					return EventPage{}, err
				}
				event.Reaction = &value
			case chat.PinChanged:
				var value chat.Pin
				if len(envelope.Value) > 0 && string(envelope.Value) != "null" {
					if err = json.Unmarshal(envelope.Value, &value); err != nil {
						return EventPage{}, err
					}
				} else {
					value.PostID = envelope.TargetID
					value.ConversationID = request.ConversationID
					value.TenantID = request.TenantID
				}
				event.Pin = &value
			}
		}
		page.Events = append(page.Events, chat.WatchEvent{Event: event, ResumeCursor: strconv.FormatInt(row.id, 10)})
	}
	if err = rows.Err(); err != nil {
		return EventPage{}, err
	}
	page.Complete = scanned < limit
	if len(statusChanges) > 0 {
		rows.Close()
		current, readErr := statusChangedConversation(ctx, tx, request.TenantID, request.ConversationID)
		if readErr != nil {
			return EventPage{}, readErr
		}
		for _, at := range statusChanges {
			value := current
			page.Events[at].Event.Conversation = &value
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return EventPage{}, err
	}
	return page, nil
}
