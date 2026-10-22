package chatstore

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TestTodo_CHATBUG_092_Integration removes a message as one administrator and
// restores it as another. The queue item must keep both people, each with the
// time of their own action, and must not read the restorer as the remover.
func TestTodo_CHATBUG_092_Integration(t *testing.T) {
	s, remover, post := chatremoveDB(t)
	ctx := context.Background()
	restorer := chat.Principal{TenantID: remover.TenantID, SubjectID: "second-admin"}
	if err := s.removalTx(ctx, remover.TenantID, func(tx dbport.Tx) error {
		for _, id := range []string{remover.SubjectID, restorer.SubjectID} {
			if _, err := tx.Exec(ctx, `INSERT INTO chat_moderation_role(tenant_id,subject_id,role) VALUES($1,$2,'WORKSPACE_ADMIN') ON CONFLICT DO NOTHING`, remover.TenantID, id); err != nil {
				return err
			}
		}
		// The room is private: the restorer must be a member to act there,
		// like any moderator who did not take the original action.
		if _, err := tx.Exec(ctx, `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,role,state,history_visibility,joined_at) VALUES($1,$2,$1,$3,'member','active','FULL_HISTORY',now()) ON CONFLICT DO NOTHING`, remover.TenantID, post.ConversationID, restorer.SubjectID); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	chatremoveApply(t, s, remover, post, "remove")

	// Still removed: the item names the remover and nobody else.
	before, err := s.SearchModeration(ctx, remover, remover.TenantID, "state:closed")
	if err != nil || len(before) != 1 {
		t.Fatalf("the removal is not listed: %+v err=%v", before, err)
	}
	if before[0].RemovedBy != remover.SubjectID || before[0].RestoredBy != "" || !before[0].RestoredAt.IsZero() {
		t.Fatalf("a removal names the wrong people before any restore: removedBy=%q restoredBy=%q restoredAt=%v", before[0].RemovedBy, before[0].RestoredBy, before[0].RestoredAt)
	}
	removedAt := before[0].At

	restoredAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Microsecond)
	if err := s.ResolveModeration(ctx, restorer, restorer.TenantID, "removal:"+post.ID, "restore", "restored", restoredAt); err != nil {
		t.Fatalf("restore: %v", err)
	}
	after, err := s.SearchModeration(ctx, remover, remover.TenantID, "state:closed")
	if err != nil || len(after) != 1 {
		t.Fatalf("the restored removal is not listed: %+v err=%v", after, err)
	}
	item := after[0]
	if item.RemovedBy != remover.SubjectID {
		t.Fatalf("the restore took the removal's credit: removedBy=%q want %q", item.RemovedBy, remover.SubjectID)
	}
	if item.RestoredBy != restorer.SubjectID || !item.RestoredAt.Equal(restoredAt) {
		t.Fatalf("the restorer is not recorded with their time: restoredBy=%q restoredAt=%v want %q %v", item.RestoredBy, item.RestoredAt, restorer.SubjectID, restoredAt)
	}
	if !item.At.Equal(removedAt) || !item.RestoredAt.After(item.At) {
		t.Fatalf("the two times are not kept apart: removed %v restored %v", item.At, item.RestoredAt)
	}
	// The last decision is still the restore; the removal lines must not use it.
	if item.Decision != "restore" || item.DecidedBy != restorer.SubjectID {
		t.Fatalf("the decision record changed: %q by %q", item.Decision, item.DecidedBy)
	}
}
