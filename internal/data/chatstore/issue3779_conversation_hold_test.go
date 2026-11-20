package chatstore

import (
	"context"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func issue3779Conversation(t *testing.T, s *Adapter, id string) chat.Post {
	t.Helper()
	ctx := context.Background()
	c := chat.Conversation{ID: id, TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "held " + id, OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	p, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "p-" + id}, chat.Post{AuthorID: "alice", AuthorHomeTenantID: c.TenantID, Body: "evidence in " + id})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func issue3779Hold(t *testing.T, s *Adapter, holdID, conversation string, release bool) {
	t.Helper()
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	if release {
		_, err = tx.Exec(ctx, `UPDATE chat_record_hold SET released_at=now() WHERE tenant_id='tenant-a' AND hold_id=$1`, holdID)
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO chat_record_hold(tenant_id,hold_id,matter_ref,reason,placed_by,placed_at,conversation_id) VALUES('tenant-a',$1,'','claim','counsel',now(),$2)`, holdID, conversation)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func issue3779RevisionBody(t *testing.T, s *Adapter, postID string, revision uint64) string {
	t.Helper()
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	var body string
	if err = tx.QueryRow(ctx, `SELECT body FROM chat_post_revision WHERE tenant_id='tenant-a' AND post_id=$1 AND revision=$2`, postID, revision).Scan(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

// A legal hold on a conversation must keep the evidence of a post deleted
// afterwards, even though nobody tagged that post's record with the hold.
func TestIssue3779_Integration_ConversationHoldPreservesADeletedPost(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	held := issue3779Conversation(t, s, "held")
	free := issue3779Conversation(t, s, "free")
	issue3779Hold(t, s, "h-1", "held", false)

	del := func(p chat.Post, conversation string) chat.Post {
		out, err := s.DeletePost(ctx, chat.DeletePostRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}, TenantID: "tenant-a", ConversationID: conversation, PostID: p.ID, ExpectedRevision: p.Revision})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got := issue3779RevisionBody(t, s, held.ID, del(held, "held").Revision); got != "evidence in held" {
		t.Fatalf("a delete in a held conversation must preserve the body, got %q", got)
	}
	if got := issue3779RevisionBody(t, s, free.ID, del(free, "free").Revision); got != "" {
		t.Fatalf("a delete in another conversation stays destructive, got %q", got)
	}
}

func TestIssue3779_Integration_ReleasedConversationHoldNoLongerPreserves(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	p := issue3779Conversation(t, s, "held")
	issue3779Hold(t, s, "h-1", "held", false)
	issue3779Hold(t, s, "h-1", "held", true)
	out, err := s.DeletePost(ctx, chat.DeletePostRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}, TenantID: "tenant-a", ConversationID: "held", PostID: p.ID, ExpectedRevision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if got := issue3779RevisionBody(t, s, p.ID, out.Revision); got != "" {
		t.Fatalf("a released hold must not preserve the body, got %q", got)
	}
}
