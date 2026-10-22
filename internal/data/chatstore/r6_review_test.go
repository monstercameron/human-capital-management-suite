package chatstore

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// A person's own open report about a message is found, and nobody else's, and
// once a moderator has decided it the person may report again (lane R6).
func TestR6_HasOpenReportIsPerReporterAndOpenOnly(t *testing.T) {
	s, p, post := chatremoveDB(t)
	ctx := context.Background()
	if err := s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO chat_moderation_report(tenant_id,report_id,conversation_id,target_id,reporter_id,reason,created_at,state) VALUES($1,'r6a','room',$2,'author','spam',now(),'OPEN')`, p.TenantID, post.ID)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	reporter := chat.Principal{TenantID: p.TenantID, SubjectID: "author"}
	if open, err := s.HasOpenReport(ctx, reporter, p.TenantID, "room", post.ID); err != nil || !open {
		t.Fatalf("the reporter's own open report was not found: %v %v", open, err)
	}
	if open, err := s.HasOpenReport(ctx, p, p.TenantID, "room", post.ID); err != nil || open {
		t.Fatalf("another person's report counted: %v %v", open, err)
	}
	if err := s.ResolveModeration(ctx, p, p.TenantID, "report:r6a", "dismiss", "not a problem", time.Now()); err != nil {
		t.Fatal(err)
	}
	if open, err := s.HasOpenReport(ctx, reporter, p.TenantID, "room", post.ID); err != nil || open {
		t.Fatalf("a decided report still blocks a new one: %v %v", open, err)
	}
}
