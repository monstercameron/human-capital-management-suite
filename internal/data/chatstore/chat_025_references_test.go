package chatstore

import (
	"context"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestTodo_CHAT_025_ReferencesPersist proves the store commits the reference
// set an edit carries (CHAT-025 revalidation), keeps the stored set when the
// edit carries none, and records the committed set in the revision ledger.
func TestTodo_CHAT_025_ReferencesPersist(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "c1", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "edits", OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	principal := chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}
	mention := chat.Reference{Kind: chat.PersonMention, TenantID: c.TenantID, ID: "bob", Display: "Bob"}
	p, err := s.SendPost(ctx, chat.SendPostRequest{Principal: principal, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "k1", References: []chat.Reference{mention}}, chat.Post{AuthorID: "alice", AuthorHomeTenantID: c.TenantID, Body: "hi @bob"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.References) != 1 {
		t.Fatalf("sent references = %+v", p.References)
	}

	kept, err := s.EditPost(ctx, chat.EditPostRequest{Principal: principal, TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, Body: "hi again @bob", ExpectedRevision: p.Revision})
	if err != nil {
		t.Fatalf("edit without references: %v", err)
	}
	if len(kept.References) != 1 || kept.References[0].ID != "bob" {
		t.Fatalf("edit without references = %+v, want the stored mention kept", kept.References)
	}

	dropped, err := s.EditPost(ctx, chat.EditPostRequest{Principal: principal, TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, Body: "hi team", ExpectedRevision: kept.Revision, References: []chat.Reference{}})
	if err != nil {
		t.Fatalf("edit dropping references: %v", err)
	}
	if len(dropped.References) != 0 {
		t.Fatalf("edit with an empty set = %+v, want no references", dropped.References)
	}
	got, err := s.GetPost(ctx, c.TenantID, c.ID, p.ID)
	if err != nil || len(got.References) != 0 || got.Body != "hi team" {
		t.Fatalf("reread = %+v, %v; want the committed empty set", got, err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, c.TenantID); err != nil {
		t.Fatal(err)
	}
	var keptRefs, droppedRefs string
	if err = tx.QueryRow(ctx, `SELECT references_json::text FROM chat_post_revision WHERE tenant_id=$1 AND post_id=$2 AND revision=$3`, c.TenantID, p.ID, kept.Revision).Scan(&keptRefs); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT references_json::text FROM chat_post_revision WHERE tenant_id=$1 AND post_id=$2 AND revision=$3`, c.TenantID, p.ID, dropped.Revision).Scan(&droppedRefs); err != nil {
		t.Fatal(err)
	}
	if keptRefs == "[]" || droppedRefs != "[]" {
		t.Fatalf("revision ledger references = %q then %q, want the mention then an empty set", keptRefs, droppedRefs)
	}
}
