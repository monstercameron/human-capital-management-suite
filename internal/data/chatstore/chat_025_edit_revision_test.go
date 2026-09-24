package chatstore

import (
	"context"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestTodo_CHAT_025 is this todo's PRIMARY matrix test: it proves that an
// authorized edit against the real store creates an immutable revision entry
// (the prior body stays permanently readable in chat_post_revision under its
// own revision number, never overwritten by a later edit), that the live post
// projection carries the latest body forward, and that the edit produces
// exactly one "post.edited" outbox effect without re-emitting the original
// "post.created" effect (i.e. editing never repeats the send-time HCM effect).
func TestTodo_CHAT_025(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "c1", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "edits", OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	principal := chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}
	p, err := s.SendPost(ctx, chat.SendPostRequest{Principal: principal, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "k1"}, chat.Post{AuthorID: "alice", AuthorHomeTenantID: c.TenantID, Body: "original"})
	if err != nil {
		t.Fatal(err)
	}

	firstEdit, err := s.EditPost(ctx, chat.EditPostRequest{Principal: principal, TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, Body: "revised once", ExpectedRevision: p.Revision})
	if err != nil {
		t.Fatalf("first edit: %v", err)
	}
	if firstEdit.Revision != p.Revision+1 || firstEdit.Body != "revised once" {
		t.Fatalf("first edit = %+v, want revision=%d body=%q", firstEdit, p.Revision+1, "revised once")
	}

	secondEdit, err := s.EditPost(ctx, chat.EditPostRequest{Principal: principal, TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, Body: "revised twice", ExpectedRevision: firstEdit.Revision})
	if err != nil {
		t.Fatalf("second edit: %v", err)
	}
	if secondEdit.Revision != firstEdit.Revision+1 || secondEdit.Body != "revised twice" {
		t.Fatalf("second edit = %+v, want revision=%d body=%q", secondEdit, firstEdit.Revision+1, "revised twice")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, c.TenantID); err != nil {
		t.Fatal(err)
	}

	var firstBody, secondBody string
	if err = tx.QueryRow(ctx, `SELECT body FROM chat_post_revision WHERE tenant_id=$1 AND post_id=$2 AND revision=$3`, c.TenantID, p.ID, firstEdit.Revision).Scan(&firstBody); err != nil {
		t.Fatalf("read first revision row: %v", err)
	}
	if firstBody != "revised once" {
		t.Fatalf("first revision row body = %q, want %q", firstBody, "revised once")
	}
	if err = tx.QueryRow(ctx, `SELECT body FROM chat_post_revision WHERE tenant_id=$1 AND post_id=$2 AND revision=$3`, c.TenantID, p.ID, secondEdit.Revision).Scan(&secondBody); err != nil {
		t.Fatalf("read second revision row: %v", err)
	}
	if secondBody != "revised twice" {
		t.Fatalf("second revision row body = %q, want %q", secondBody, "revised twice")
	}
	// The earlier revision row must still be exactly what it was: a later edit
	// appends a new immutable row, it never rewrites the one before it.
	if err = tx.QueryRow(ctx, `SELECT body FROM chat_post_revision WHERE tenant_id=$1 AND post_id=$2 AND revision=$3`, c.TenantID, p.ID, firstEdit.Revision).Scan(&firstBody); err != nil {
		t.Fatalf("re-read first revision row: %v", err)
	}
	if firstBody != "revised once" {
		t.Fatalf("first revision row mutated by a later edit: now %q", firstBody)
	}

	var createdCount, editedCount int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM chat_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='post.created'`, c.TenantID, p.ID).Scan(&createdCount); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM chat_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='post.edited'`, c.TenantID, p.ID).Scan(&editedCount); err != nil {
		t.Fatal(err)
	}
	if createdCount != 1 {
		t.Fatalf("post.created outbox events = %d, want exactly 1 (two edits must never repeat the send-time effect)", createdCount)
	}
	if editedCount != 2 {
		t.Fatalf("post.edited outbox events = %d, want exactly 2 (one per authorized edit)", editedCount)
	}

	live, err := s.GetPost(ctx, c.TenantID, c.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if live.Body != "revised twice" || live.Revision != secondEdit.Revision {
		t.Fatalf("live post = %+v, want the latest edit projected forward", live)
	}
}
