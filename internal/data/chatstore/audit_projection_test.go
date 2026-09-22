package chatstore

import (
	"context"
	"strings"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_CHAT_047_Integration_AtomicCoreAudit(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	principal := chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}
	c := chat.Conversation{ID: "c1", TenantID: principal.TenantID, Kind: chat.PrivateChannel, OwnerID: principal.SubjectID, Revision: 1}
	m := chat.Membership{TenantID: c.TenantID, ConversationID: c.ID, HomeTenantID: principal.TenantID, SubjectID: principal.SubjectID, Role: chat.Manager}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, "once"); err != nil {
		t.Fatal(err)
	}
	r := chat.SendPostRequest{Principal: principal, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "post-once"}
	p, err := s.SendPost(ctx, r, chat.Post{AuthorID: principal.SubjectID, Body: "private message body"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendPost(ctx, r, chat.Post{AuthorID: principal.SubjectID, Body: "private message body"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT actor_id,action,policy_evidence,digest FROM chat_audit_event WHERE tenant_id=$1 ORDER BY sequence`, c.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		count, posts := 0, 0
		for rows.Next() {
			var actor, action, evidence, digest string
			if err := rows.Scan(&actor, &action, &evidence, &digest); err != nil {
				return err
			}
			if actor != "alice" || !strings.Contains(evidence, "actor-home:tenant-a") || strings.Contains(evidence, "private message body") || strings.Contains(digest, "private message body") {
				t.Fatalf("invalid metadata: %q %q %q %q", actor, action, evidence, digest)
			}
			count++
			if action == "post.created" {
				posts++
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if count != 3 || posts != 1 {
			t.Fatalf("audit events=%d posts=%d", count, posts)
		}
		var revision int64
		if err := tx.QueryRow(ctx, `SELECT revision FROM chat_record_inventory WHERE tenant_id=$1 AND record_id=$2`, c.TenantID, "post:"+p.ID).Scan(&revision); err != nil {
			return err
		}
		if revision != 1 {
			t.Fatalf("record revision=%d", revision)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_CHAT_047_Integration_AuditFailureRollsBackCoreMutation(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "c1", TenantID: "tenant-a", Kind: chat.PrivateChannel, OwnerID: "alice", Revision: 1}
	m := chat.Membership{TenantID: c.TenantID, ConversationID: c.ID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.execTenant(ctx, c.TenantID, `ALTER TABLE chat_audit_event ADD CONSTRAINT reject_post_audit CHECK (action <> 'post.created') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	_, err := s.SendPost(ctx, chat.SendPostRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, TenantID: c.TenantID, ConversationID: c.ID}, chat.Post{AuthorID: "alice", Body: "uncommitted"})
	if err == nil {
		t.Fatal("audit failure acknowledged a post")
	}
	if err := s.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		for _, table := range []string{"chat_post", "chat_post_revision"} {
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE tenant_id=$1`, c.TenantID).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				t.Fatalf("%s rows=%d", table, count)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
