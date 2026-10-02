package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var _ chat.VoiceStore = (*Adapter)(nil)

func voiceIdentity(r chat.TranscriptionRequest) bool {
	return r.TenantID != "" && r.ConversationID != "" && r.PostID != "" && r.ArtifactID != ""
}

func (s *Adapter) voiceTx(ctx context.Context, p chat.Principal, r chat.TranscriptionRequest, fn func(dbport.Tx) error) error {
	if !voiceIdentity(r) || p.SubjectID == "" || p.TenantID == "" {
		return chat.ErrInvalidArgument
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, r.TenantID); err != nil {
		return err
	}
	// Lock membership and message before any voice side effect; removal and
	// erasure cannot race a correction or a worker completion.
	if err = lockVisibleReactionPost(ctx, tx, r.TenantID, r.ConversationID, r.PostID, p.TenantID, p.SubjectID); err != nil {
		return err
	}
	var post string
	if err = tx.QueryRow(ctx, `SELECT id FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3 FOR UPDATE`, r.TenantID, r.ConversationID, r.PostID).Scan(&post); err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func voiceRead(ctx context.Context, tx dbport.Tx, r chat.TranscriptionRequest) (chat.VoiceRecord, error) {
	out := chat.VoiceRecord{TranscriptionRequest: r}
	var a, t []byte
	err := tx.QueryRow(ctx, `SELECT duration_ms,attachment,transcript,revision FROM chat_voice_transcript WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3 AND artifact_id=$4 FOR UPDATE`, r.TenantID, r.ConversationID, r.PostID, r.ArtifactID).Scan(&out.DurationMS, &a, &t, &out.Transcript.Revision)
	if errors.Is(err, dbport.ErrNoRows) {
		return out, chat.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(a, &out.Attachment); err != nil {
		return out, err
	}
	revision := out.Transcript.Revision
	err = json.Unmarshal(t, &out.Transcript)
	out.Transcript.Revision = revision
	out.Attachment.TranscriptState = out.Transcript.State
	return out, err
}

func (s *Adapter) RequestVoiceTranscript(ctx context.Context, p chat.Principal, r chat.VoiceRecord) (chat.VoiceRecord, error) {
	var out chat.VoiceRecord
	if r.Attachment.Validate() != nil || r.Attachment.DurationMS != r.DurationMS || r.Attachment.ArtifactID != r.ArtifactID {
		return out, chat.ErrInvalidArgument
	}
	err := s.voiceTx(ctx, p, r.TranscriptionRequest, func(tx dbport.Tx) error {
		var refs []byte
		var author, home string
		if err := tx.QueryRow(ctx, `SELECT references_json,author_id,author_home_tenant_id FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3`, r.TenantID, r.ConversationID, r.PostID).Scan(&refs, &author, &home); err != nil {
			return err
		}
		if author != p.SubjectID || home != p.TenantID {
			return chat.ErrPermissionDenied
		}
		var references []chat.Reference
		if err := json.Unmarshal(refs, &references); err != nil {
			return err
		}
		found := false
		for _, ref := range references {
			if ref.Kind == chat.MediaAttachment && ref.ID == r.ArtifactID && ref.TenantID == r.TenantID && (ref.ConversationID == "" || ref.ConversationID == r.ConversationID) && ref.ContentType == r.Attachment.ContentType && ref.ByteSize == uint64(r.Attachment.Bytes) {
				found = true
			}
		}
		if !found {
			return chat.ErrInvalidArgument
		}
		a, err := json.Marshal(r.Attachment)
		if err != nil {
			return err
		}
		t, err := json.Marshal(chat.VoiceTranscript{State: chat.TranscriptPending})
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO chat_voice_transcript(tenant_id,conversation_id,post_id,artifact_id,duration_ms,attachment,transcript,state) VALUES($1,$2,$3,$4,$5,$6,$7,'pending') ON CONFLICT DO NOTHING`, r.TenantID, r.ConversationID, r.PostID, r.ArtifactID, r.DurationMS, a, t); err != nil {
			return err
		}
		out, err = voiceRead(ctx, tx, r.TranscriptionRequest)
		if err == nil && (out.DurationMS != r.DurationMS || out.Attachment.ContentType != r.Attachment.ContentType || out.Attachment.Bytes != r.Attachment.Bytes) {
			return ErrIdempotencyConflict
		}
		return err
	})
	return out, err
}

func (s *Adapter) ReadVoiceTranscript(ctx context.Context, p chat.Principal, r chat.TranscriptionRequest) (chat.VoiceRecord, error) {
	var out chat.VoiceRecord
	err := s.voiceTx(ctx, p, r, func(tx dbport.Tx) error { var err error; out, err = voiceRead(ctx, tx, r); return err })
	return out, err
}

func (s *Adapter) SaveVoiceTranscript(ctx context.Context, p chat.Principal, r chat.TranscriptionRequest, expected uint64, value chat.VoiceTranscript) (chat.VoiceRecord, error) {
	return s.updateVoice(ctx, p, r, expected, value, "", false)
}
func (s *Adapter) CorrectVoiceTranscript(ctx context.Context, p chat.Principal, r chat.TranscriptionRequest, expected uint64, text string) (chat.VoiceRecord, error) {
	if strings.TrimSpace(text) == "" || len(text) > 16000 {
		return chat.VoiceRecord{}, chat.ErrInvalidArgument
	}
	return s.updateVoice(ctx, p, r, expected, chat.VoiceTranscript{}, text, true)
}
func (s *Adapter) updateVoice(ctx context.Context, p chat.Principal, r chat.TranscriptionRequest, expected uint64, value chat.VoiceTranscript, correction string, correct bool) (chat.VoiceRecord, error) {
	var out chat.VoiceRecord
	err := s.voiceTx(ctx, p, r, func(tx dbport.Tx) error {
		current, err := voiceRead(ctx, tx, r)
		if err != nil {
			return err
		}
		if expected == 0 || current.Transcript.Revision != expected {
			return ErrIdempotencyConflict
		}
		if correct {
			var author, home string
			if err = tx.QueryRow(ctx, `SELECT author_id,author_home_tenant_id FROM chat_post WHERE tenant_id=$1 AND id=$2`, r.TenantID, r.PostID).Scan(&author, &home); err != nil {
				return err
			}
			if author != p.SubjectID || home != p.TenantID {
				return chat.ErrPermissionDenied
			}
			value = current.Transcript
			if value.State != chat.TranscriptReady {
				return chat.ErrInvalidArgument
			}
			value.Correction = correction
		} else {
			// Only trusted worker identities may submit automatic results. A
			// human may request/retry, but cannot impersonate the speech engine.
			machine, err := machineActor(ctx, r.TenantID, p.SubjectID)
			if err != nil {
				return err
			}
			identity, trusted := trust.FromContext(ctx)
			if !machine || !trusted || identity.SubjectKind() != trust.SubjectKindIntegration || p.SubjectID != chat.VoiceWorkerSubject {
				return chat.ErrPermissionDenied
			}
			value.Correction = ""
			if value.State == chat.TranscriptReady {
				// Correction history survives unavailable/failed results and retries.
				// A later engine result must never replace the author's wording.
				if err = tx.QueryRow(ctx, `SELECT COALESCE(corrections->-1->>'text','') FROM chat_voice_transcript WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3 AND artifact_id=$4`, r.TenantID, r.ConversationID, r.PostID, r.ArtifactID).Scan(&value.Correction); err != nil {
					return err
				}
			}
		}
		if err = value.Validate(current.DurationMS); err != nil {
			return err
		}
		value.Revision = expected + 1
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		history := []byte("[]")
		if correct {
			history, err = json.Marshal([]map[string]any{{"author": p.SubjectID, "home_tenant": p.TenantID, "revision": value.Revision, "text": correction}})
			if err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE chat_voice_transcript SET transcript=$5,state=$6,search_text=$7,revision=revision+1,corrections=corrections || $8::jsonb,updated_at=now() WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3 AND artifact_id=$4`, r.TenantID, r.ConversationID, r.PostID, r.ArtifactID, raw, string(value.State), value.Text(), history); err != nil {
			return err
		}
		out, err = voiceRead(ctx, tx, r)
		return err
	})
	return out, err
}

// SearchVoiceTranscripts applies current membership, history and tombstone
// filters in SQL before matching text or limiting results. No hidden counts.
func (s *Adapter) SearchVoiceTranscripts(ctx context.Context, p chat.Principal, t, query string, limit int) ([]chat.VoiceRecord, error) {
	if t == "" || p.TenantID == "" || p.SubjectID == "" || strings.TrimSpace(query) == "" || limit < 1 || limit > 100 {
		return nil, chat.ErrInvalidArgument
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, t); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT v.conversation_id,v.post_id,v.artifact_id,v.duration_ms,v.attachment,v.transcript,v.revision FROM chat_voice_transcript v JOIN chat_post p ON p.tenant_id=v.tenant_id AND p.conversation_id=v.conversation_id AND p.id=v.post_id AND NOT p.tombstoned JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=$2 AND m.member_id=$3 AND m.state='active' WHERE v.tenant_id=$1 AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at)) AND v.state='ready' AND to_tsvector('simple',v.search_text) @@ plainto_tsquery('simple',$4) ORDER BY p.sequence DESC,v.artifact_id LIMIT $5`, t, p.TenantID, p.SubjectID, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []chat.VoiceRecord{}
	for rows.Next() {
		var r chat.VoiceRecord
		var a, tr []byte
		var rev uint64
		r.TenantID = t
		if err = rows.Scan(&r.ConversationID, &r.PostID, &r.ArtifactID, &r.DurationMS, &a, &tr, &rev); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(a, &r.Attachment); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(tr, &r.Transcript); err != nil {
			return nil, err
		}
		r.Transcript.Revision = rev
		out = append(out, r)
	}
	return out, rows.Err()
}
