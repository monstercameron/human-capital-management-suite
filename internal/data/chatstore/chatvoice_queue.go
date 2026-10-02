package chatstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func chatvoiceWorkerAuthorized(ctx context.Context, p chat.Principal, t string) bool {
	identity, ok := trust.FromContext(ctx)
	if !ok || identity == nil || identity.SubjectKind() != trust.SubjectKindIntegration || p.SubjectID != chat.VoiceWorkerSubject || p.TenantID != t {
		return false
	}
	active, err := machineActor(ctx, t, p.SubjectID)
	return active && err == nil
}

// PendingVoiceTranscripts is the durable queue read seam. Results are bounded
// and limited to messages the deployment worker currently has permission to
// read. Completions use revision CAS, so concurrent consumers are harmless.
func (s *Adapter) PendingVoiceTranscripts(ctx context.Context, p chat.Principal, t, cid string, limit int) ([]chat.TranscriptionRequest, error) {
	if !chatvoiceWorkerAuthorized(ctx, p, t) {
		return nil, chat.ErrPermissionDenied
	}
	if cid == "" || limit < 1 || limit > 100 {
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
	rows, err := tx.Query(ctx, `SELECT v.post_id,v.artifact_id,v.duration_ms FROM chat_voice_transcript v JOIN chat_post p ON p.tenant_id=v.tenant_id AND p.conversation_id=v.conversation_id AND p.id=v.post_id AND NOT p.tombstoned JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=$1 AND m.member_id=$3 AND m.state='active' WHERE v.tenant_id=$1 AND v.conversation_id=$2 AND v.state='pending' AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at)) ORDER BY v.updated_at,v.post_id,v.artifact_id LIMIT $4`, t, cid, p.SubjectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []chat.TranscriptionRequest{}
	for rows.Next() {
		r := chat.TranscriptionRequest{TenantID: t, ConversationID: cid}
		if err = rows.Scan(&r.PostID, &r.ArtifactID, &r.DurationMS); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PurgeErasedVoiceTranscripts removes derived parts after a previously held
// tombstone's hold is released. The records erasure worker calls this alongside
// media erasure; live messages and active holds cannot be purged by this seam.
func (s *Adapter) PurgeErasedVoiceTranscripts(ctx context.Context, p chat.Principal, t string) error {
	if !chatvoiceWorkerAuthorized(ctx, p, t) {
		return chat.ErrPermissionDenied
	}
	return s.Store.RunTenantTx(ctx, t, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM chat_voice_transcript v USING chat_post p WHERE v.tenant_id=$1 AND p.tenant_id=v.tenant_id AND p.id=v.post_id AND p.conversation_id=v.conversation_id AND p.tombstoned AND NOT EXISTS (SELECT 1 FROM chat_record_inventory i JOIN chat_record_hold h ON h.tenant_id=i.tenant_id AND h.hold_id IN (SELECT jsonb_array_elements_text(i.hold_ids)) WHERE i.tenant_id=p.tenant_id AND i.record_id='post:' || p.id AND h.released_at IS NULL)`, t)
		return err
	})
}
