package chatrecordstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_CHAT_047_Integration_ModerationAuditAtomicity(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	at := time.Unix(1700000000, 0).UTC()
	action := chatrecords.CaseAction{CaseID: "case-7", Action: "remove", ActorID: "moderator", Reason: "policy violation", EvidenceRef: "evidence://case-7", At: at}
	event := chatrecords.AuditEvent{TenantID: "tenant-a", ActorID: "moderator", Action: "moderation.remove", TargetType: "chat_post", TargetID: "post-9", PriorRevision: 12, Reason: "policy violation", PolicyEvidence: "chat:conv:12", At: at}
	committed, err := s.PutCaseActionAudited(ctx, "tenant-a", "conv-3", action, event)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Events(ctx, "tenant-a")
	if err != nil || len(rows) != 1 || rows[0].TenantID != committed.TenantID || rows[0].EventID != committed.EventID || rows[0].Sequence != committed.Sequence || rows[0].ActorID != committed.ActorID || rows[0].Action != committed.Action || rows[0].TargetType != committed.TargetType || rows[0].TargetID != committed.TargetID || rows[0].PriorRevision != committed.PriorRevision || rows[0].Reason != committed.Reason || rows[0].PolicyEvidence != committed.PolicyEvidence || !rows[0].At.Equal(committed.At) || rows[0].Digest != committed.Digest || committed.Sequence != 1 || committed.Digest != chatrecords.DigestEvent(committed) {
		t.Fatalf("persisted audit = %+v, committed=%+v err=%v", rows, committed, err)
	}
	if err := s.Chat.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		var actions, outbox int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_moderation_action WHERE tenant_id=$1 AND case_id=$2`, "tenant-a", action.CaseID).Scan(&actions); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='records.audit'`, "tenant-a", committed.EventID).Scan(&outbox); err != nil {
			return err
		}
		if actions != 1 || outbox != 1 {
			t.Fatalf("atomic projection counts: actions=%d outbox=%d", actions, outbox)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
