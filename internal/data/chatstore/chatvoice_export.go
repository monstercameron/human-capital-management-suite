package chatstore

import (
	"context"
	"encoding/json"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type VoiceCorrection struct {
	Author     string `json:"author"`
	HomeTenant string `json:"home_tenant"`
	Revision   uint64 `json:"revision"`
	Text       string `json:"text"`
}

// VoiceExport is the message's derived content and correction history in one
// snapshot. Audio stays in the protected media store, identified by ArtifactID.
type VoiceExport struct {
	Record      chat.VoiceRecord
	Corrections []VoiceCorrection
}

func (s *Adapter) ExportVoiceTranscript(ctx context.Context, p chat.Principal, r chat.TranscriptionRequest) (VoiceExport, error) {
	var out VoiceExport
	err := s.voiceTx(ctx, p, r, func(tx dbport.Tx) error {
		var err error
		out.Record, err = voiceRead(ctx, tx, r)
		if err != nil {
			return err
		}
		var raw []byte
		if err = tx.QueryRow(ctx, `SELECT corrections FROM chat_voice_transcript WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3 AND artifact_id=$4`, r.TenantID, r.ConversationID, r.PostID, r.ArtifactID).Scan(&raw); err != nil {
			return err
		}
		return json.Unmarshal(raw, &out.Corrections)
	})
	if err != nil {
		return VoiceExport{}, err
	}
	return out, nil
}
