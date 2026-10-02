package chatstore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PendingVoiceTranscriptsAll lists the tenant's pending requests across its
// conversations, oldest first, for the server's transcription worker. Only the
// worker identity may ask. A message that has been removed is not listed, and a
// request is listed only while its message is live; whether the worker may read
// a message's transcript is the same identity check the per-conversation queue
// makes, so the worker does not have to be a member of every conversation.
func (s *Adapter) PendingVoiceTranscriptsAll(ctx context.Context, p chat.Principal, t string, limit int) ([]chat.TranscriptionRequest, error) {
	if !chatvoiceWorkerAuthorized(ctx, p, t) {
		return nil, chat.ErrPermissionDenied
	}
	if limit < 1 || limit > 100 {
		return nil, chat.ErrInvalidArgument
	}
	out := []chat.TranscriptionRequest{}
	err := s.Store.RunTenantTx(ctx, t, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT v.conversation_id,v.post_id,v.artifact_id,v.duration_ms FROM chat_voice_transcript v JOIN chat_post p ON p.tenant_id=v.tenant_id AND p.conversation_id=v.conversation_id AND p.id=v.post_id AND NOT p.tombstoned WHERE v.tenant_id=$1 AND v.state='pending' ORDER BY v.updated_at,v.post_id,v.artifact_id LIMIT $2`, t, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r := chat.TranscriptionRequest{TenantID: t}
			if err = rows.Scan(&r.ConversationID, &r.PostID, &r.ArtifactID, &r.DurationMS); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// VoiceRecordsForPosts returns the voice records of the given posts as a reader
// who may open them. It is the one lookup behind the unified message content
// reader (CHATVOICE-005): a post the reader cannot see, or that was removed, has
// no entry, so a transcript is never readable by someone who cannot read the
// message.
func (s *Adapter) VoiceRecordsForPosts(ctx context.Context, p chat.Principal, t, conversation string, posts []string) (map[string]chat.VoiceRecord, error) {
	out := map[string]chat.VoiceRecord{}
	if t == "" || conversation == "" || p.TenantID == "" || p.SubjectID == "" || len(posts) > 200 {
		return nil, chat.ErrInvalidArgument
	}
	if len(posts) == 0 {
		return out, nil
	}
	err := s.Store.RunTenantTx(ctx, t, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT v.post_id,v.artifact_id,v.duration_ms,v.attachment,v.transcript,v.revision FROM chat_voice_transcript v JOIN chat_post p ON p.tenant_id=v.tenant_id AND p.conversation_id=v.conversation_id AND p.id=v.post_id AND NOT p.tombstoned JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=$3 AND m.member_id=$4 AND m.state='active' AND m.left_at IS NULL WHERE v.tenant_id=$1 AND v.conversation_id=$2 AND v.post_id = ANY($5) AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at))`, t, conversation, p.TenantID, p.SubjectID, posts)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var record chat.VoiceRecord
			var attachment, transcript []byte
			var revision uint64
			record.TenantID, record.ConversationID = t, conversation
			if err = rows.Scan(&record.PostID, &record.ArtifactID, &record.DurationMS, &attachment, &transcript, &revision); err != nil {
				return err
			}
			if err = unmarshalVoice(attachment, transcript, revision, &record); err != nil {
				return err
			}
			out[record.PostID] = record
		}
		return rows.Err()
	})
	return out, err
}

func unmarshalVoice(attachment, transcript []byte, revision uint64, record *chat.VoiceRecord) error {
	if err := json.Unmarshal(attachment, &record.Attachment); err != nil {
		return err
	}
	if err := json.Unmarshal(transcript, &record.Transcript); err != nil {
		return err
	}
	record.Transcript.Revision = revision
	record.Attachment.TranscriptState = record.Transcript.State
	return nil
}

// VoicePostAuthor tells the worker whose message it is transcribing, for the
// budget a call is charged to and for the content check that runs on the
// transcript. Only the worker identity may ask.
func (s *Adapter) VoicePostAuthor(ctx context.Context, p chat.Principal, r chat.TranscriptionRequest) (homeTenant, author string, err error) {
	if !chatvoiceWorkerAuthorized(ctx, p, r.TenantID) {
		return "", "", chat.ErrPermissionDenied
	}
	err = s.Store.RunTenantTx(ctx, r.TenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT p.author_home_tenant_id,p.author_id FROM chat_post p JOIN chat_voice_transcript v ON v.tenant_id=p.tenant_id AND v.conversation_id=p.conversation_id AND v.post_id=p.id AND v.artifact_id=$4 WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.id=$3 AND NOT p.tombstoned`, r.TenantID, r.ConversationID, r.PostID, r.ArtifactID).Scan(&homeTenant, &author)
	})
	if errors.Is(err, dbport.ErrNoRows) {
		return "", "", chat.ErrNotFound
	}
	return homeTenant, author, err
}
