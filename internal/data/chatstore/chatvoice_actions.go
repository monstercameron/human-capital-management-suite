package chatstore

import (
	"context"
	"encoding/json"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func (s *Adapter) RetryVoiceTranscript(ctx context.Context, p chat.Principal, r chat.TranscriptionRequest, expected uint64) (chat.VoiceRecord, error) {
	var out chat.VoiceRecord
	err := s.voiceTx(ctx, p, r, func(tx dbport.Tx) error {
		current, err := voiceRead(ctx, tx, r)
		if err != nil {
			return err
		}
		if expected == 0 || current.Transcript.Revision != expected {
			return ErrIdempotencyConflict
		}
		if current.Transcript.State != chat.TranscriptUnavailable && current.Transcript.State != chat.TranscriptFailed {
			return chat.ErrInvalidArgument
		}
		value := chat.VoiceTranscript{State: chat.TranscriptPending, Revision: expected + 1}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE chat_voice_transcript SET state='pending',transcript=$5,revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3 AND artifact_id=$4`, r.TenantID, r.ConversationID, r.PostID, r.ArtifactID, raw); err != nil {
			return err
		}
		out, err = voiceRead(ctx, tx, r)
		return err
	})
	return out, err
}
func (s *Adapter) ReportVoiceTranscript(ctx context.Context, p chat.Principal, r chat.TranscriptionRequest) error {
	return s.voiceTx(ctx, p, r, func(tx dbport.Tx) error {
		if _, err := voiceRead(ctx, tx, r); err != nil {
			return err
		}
		// Reports have no quoted content and remain under the message's retention.
		raw, err := json.Marshal([]map[string]string{{"home_tenant": p.TenantID, "subject": p.SubjectID}})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE chat_voice_transcript SET reports=CASE WHEN reports @> $5::jsonb THEN reports ELSE reports || $5::jsonb END WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3 AND artifact_id=$4`, r.TenantID, r.ConversationID, r.PostID, r.ArtifactID, raw)
		return err
	})
}
