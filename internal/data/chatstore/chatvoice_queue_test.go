package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_CHATVOICE_003_Security(t *testing.T) {
	s, p, r := chatvoiceDB(t)
	ctx := context.Background()
	if _, err := s.RequestVoiceTranscript(ctx, p, r); err != nil {
		t.Fatal(err)
	}
	wc, worker := chatvoiceWorker(t)
	pending, err := s.PendingVoiceTranscripts(wc, worker, p.TenantID, r.ConversationID, 1)
	if err != nil || len(pending) != 1 || pending[0].ArtifactID != r.ArtifactID {
		t.Fatalf("pending=%+v,%v", pending, err)
	}
	if _, err = s.PendingVoiceTranscripts(ctx, p, p.TenantID, r.ConversationID, 1); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("human impersonated worker")
	}
	if _, err = s.PendingVoiceTranscripts(wc, worker, "other", r.ConversationID, 1); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("cross-tenant queue")
	}
	var enabled, forced bool
	err = s.Store.RunTenantTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid='chat_voice_transcript'::regclass`).Scan(&enabled, &forced)
	})
	if err != nil || !enabled || !forced {
		t.Fatalf("RLS=%v,%v,%v", enabled, forced, err)
	}
}
func TestTodo_CHATVOICE_005_Integration(t *testing.T) {
	s, p, r := chatvoiceDB(t)
	ctx := context.Background()
	if _, err := s.RequestVoiceTranscript(ctx, p, r); err != nil {
		t.Fatal(err)
	}
	if err := s.PlaceRecordHold(ctx, p.TenantID, "voice-hold", "matter", "fixture", p.SubjectID, []string{"post:" + r.PostID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeletePost(ctx, chat.DeletePostRequest{Principal: p, TenantID: p.TenantID, ConversationID: r.ConversationID, PostID: r.PostID, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	wc, worker := chatvoiceWorker(t)
	if err := s.PurgeErasedVoiceTranscripts(wc, worker, p.TenantID); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		var n int
		err := s.Store.RunTenantTx(ctx, p.TenantID, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM chat_voice_transcript WHERE tenant_id=$1 AND post_id=$2`, p.TenantID, r.PostID).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count() != 1 {
		t.Fatal("active hold purged")
	}
	if err := s.Store.RunTenantTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_record_hold SET released_at=now() WHERE tenant_id=$1 AND hold_id='voice-hold'`, p.TenantID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.PurgeErasedVoiceTranscripts(ctx, p, p.TenantID); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("untrusted erasure allowed")
	}
	if err := s.PurgeErasedVoiceTranscripts(wc, worker, p.TenantID); err != nil {
		t.Fatal(err)
	}
	if count() != 0 {
		t.Fatal("released hold left derived content")
	}
}
