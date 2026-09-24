package chatstore

import (
	"context"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestTodo_CHAT_026 is this todo's PRIMARY matrix test: an authorized delete
// against the real store records a tombstone (Deleted=true, empty body) in
// the ordinary post projection, clears the search fragment (the GIN index is
// a function of chat_post.body, so an emptied body drops out of it with no
// separate step), and hides the post's reactions and pins from ordinary
// listings — all without a records hold in play, so the deletion is genuinely
// destructive: chat_post_revision preserves nothing for a post nobody held.
func TestTodo_CHAT_026(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "c1", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "tombstones", OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	p, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "p1"}, chat.Post{AuthorID: "alice", AuthorHomeTenantID: c.TenantID, Body: "sensitive content"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PutReaction(ctx, chat.Reaction{TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, SubjectID: "alice", HomeTenantID: c.TenantID, Emoji: "+1"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PutPin(ctx, chat.Pin{TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, PinnedBy: "alice", PinnedByHomeTenantID: c.TenantID}); err != nil {
		t.Fatal(err)
	}

	deleted, err := s.DeletePost(ctx, chat.DeletePostRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, ExpectedRevision: p.Revision})
	if err != nil {
		t.Fatalf("authorized delete: %v", err)
	}
	if !deleted.Deleted || deleted.Body != "" {
		t.Fatalf("tombstone = %+v, want Deleted=true and empty body", deleted)
	}

	page, err := s.ListPosts(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, c.TenantID, c.ID, 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(page.Posts) != 1 || !page.Posts[0].Deleted || page.Posts[0].Body != "" {
		t.Fatalf("ordinary listing kept evidence: posts=%+v err=%v", page.Posts, err)
	}

	reactions, err := s.ListReactions(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, c.TenantID, c.ID, p.ID, chat.Page{PageSize: 10})
	if err != nil || len(reactions.Reactions) != 0 {
		t.Fatalf("reactions survived delete in ordinary view: %+v err=%v", reactions, err)
	}
	pins, err := s.ListPins(ctx, c.TenantID, c.ID)
	if err != nil || len(pins) != 0 {
		t.Fatalf("pins survived delete in ordinary view: %+v err=%v", pins, err)
	}

	// No hold was ever placed on this post's record: the immutable revision
	// ledger must not hold a recoverable copy either.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, c.TenantID); err != nil {
		t.Fatal(err)
	}
	var revisionBody string
	if err = tx.QueryRow(ctx, `SELECT body FROM chat_post_revision WHERE tenant_id=$1 AND post_id=$2 AND revision=$3`, c.TenantID, p.ID, deleted.Revision).Scan(&revisionBody); err != nil {
		t.Fatal(err)
	}
	if revisionBody != "" {
		t.Fatalf("unheld delete preserved a recoverable copy: %q", revisionBody)
	}
	var kind string
	if err = tx.QueryRow(ctx, `SELECT kind FROM chat_record_inventory WHERE tenant_id=$1 AND record_id=$2`, c.TenantID, "post:"+p.ID).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != "TOMBSTONE" {
		t.Fatalf("record inventory kind = %q, want TOMBSTONE", kind)
	}
}

// TestTodo_CHAT_026_Security proves an unauthorized caller (not the author,
// not the conversation manager) cannot tombstone someone else's post, and
// that the store never mutates on a refused delete.
func TestTodo_CHAT_026_Security(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "c1", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "tombstones", OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	bob := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "bob", Role: chat.Member, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m, bob}, ""); err != nil {
		t.Fatal(err)
	}
	p, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "p1"}, chat.Post{AuthorID: "alice", AuthorHomeTenantID: c.TenantID, Body: "alice only"})
	if err != nil {
		t.Fatal(err)
	}
	// bob is a current member of the conversation but did not author the
	// post: the store's own author-scoped WHERE clause must refuse the
	// delete regardless of what a higher layer decides, matching the same
	// attribution guard CHAT_025's author check exercises for edits.
	if _, err = s.DeletePost(ctx, chat.DeletePostRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "bob"}, TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, ExpectedRevision: p.Revision}); err == nil {
		t.Fatal("bob deleted alice's post")
	}
	live, err := s.GetPost(ctx, c.TenantID, c.ID, p.ID)
	if err != nil || live.Deleted || live.Body != "alice only" {
		t.Fatalf("refused delete mutated the post: %+v err=%v", live, err)
	}

	// A records hold changes nothing about who may delete: it only changes
	// what is preserved once an authorized delete happens.
	if err = s.PlaceRecordHold(ctx, c.TenantID, "hold-1", "matter-1", "litigation", "records-admin", []string{"post:" + p.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DeletePost(ctx, chat.DeletePostRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "bob"}, TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, ExpectedRevision: p.Revision}); err == nil {
		t.Fatal("bob deleted alice's held post")
	}
}

// TestTodo_CHAT_026_Integration proves the records-policy gate end to end
// against the real store: a post under an active hold keeps its pre-deletion
// body recoverable in the immutable revision ledger after an authorized
// delete, while a post with no hold is destroyed exactly like
// TestTodo_CHAT_026 shows, and releasing the hold stops covering future
// deletes.
func TestTodo_CHAT_026_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "c1", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "records", OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	principal := chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}

	held, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "held"}, chat.Post{AuthorID: "alice", AuthorHomeTenantID: c.TenantID, Body: "held evidence"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PlaceRecordHold(ctx, c.TenantID, "matter-hold", "matter-42", "pending litigation", "records-admin", []string{"post:" + held.ID}); err != nil {
		t.Fatal(err)
	}
	deletedHeld, err := s.DeletePost(ctx, chat.DeletePostRequest{Principal: principal, TenantID: c.TenantID, ConversationID: c.ID, PostID: held.ID, ExpectedRevision: held.Revision})
	if err != nil {
		t.Fatalf("delete held post: %v", err)
	}
	// Ordinary projection still shows a clean tombstone: nobody reading
	// through the normal API can tell this post was held.
	if !deletedHeld.Deleted || deletedHeld.Body != "" {
		t.Fatalf("held tombstone leaked into the ordinary projection: %+v", deletedHeld)
	}
	live, err := s.GetPost(ctx, c.TenantID, c.ID, held.ID)
	if err != nil || live.Body != "" {
		t.Fatalf("live post exposed held body: %+v err=%v", live, err)
	}

	notHeld, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "not-held"}, chat.Post{AuthorID: "alice", AuthorHomeTenantID: c.TenantID, Body: "ordinary content"})
	if err != nil {
		t.Fatal(err)
	}
	deletedOrdinary, err := s.DeletePost(ctx, chat.DeletePostRequest{Principal: principal, TenantID: c.TenantID, ConversationID: c.ID, PostID: notHeld.ID, ExpectedRevision: notHeld.Revision})
	if err != nil {
		t.Fatalf("delete ordinary post: %v", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, c.TenantID); err != nil {
		t.Fatal(err)
	}
	var heldRevisionBody, ordinaryRevisionBody string
	if err = tx.QueryRow(ctx, `SELECT body FROM chat_post_revision WHERE tenant_id=$1 AND post_id=$2 AND revision=$3`, c.TenantID, held.ID, deletedHeld.Revision).Scan(&heldRevisionBody); err != nil {
		t.Fatal(err)
	}
	if heldRevisionBody != "held evidence" {
		t.Fatalf("held delete did not preserve its records copy: got %q", heldRevisionBody)
	}
	if err = tx.QueryRow(ctx, `SELECT body FROM chat_post_revision WHERE tenant_id=$1 AND post_id=$2 AND revision=$3`, c.TenantID, notHeld.ID, deletedOrdinary.Revision).Scan(&ordinaryRevisionBody); err != nil {
		t.Fatal(err)
	}
	if ordinaryRevisionBody != "" {
		t.Fatalf("ordinary delete kept a copy it was never entitled to: got %q", ordinaryRevisionBody)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	// Release the hold, then prove a fresh post's delete is unaffected by a
	// hold that no longer covers it.
	releaseTx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = tenant(ctx, releaseTx, c.TenantID); err != nil {
		t.Fatal(err)
	}
	if _, err = releaseTx.Exec(ctx, `UPDATE chat_record_hold SET released_at=now() WHERE tenant_id=$1 AND hold_id=$2`, c.TenantID, "matter-hold"); err != nil {
		t.Fatal(err)
	}
	if err = releaseTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	afterRelease, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "after-release"}, chat.Post{AuthorID: "alice", AuthorHomeTenantID: c.TenantID, Body: "after release"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PlaceRecordHold(ctx, c.TenantID, "matter-hold", "matter-42", "pending litigation", "records-admin", nil); err != nil {
		t.Fatal(err)
	}
	deletedAfterRelease, err := s.DeletePost(ctx, chat.DeletePostRequest{Principal: principal, TenantID: c.TenantID, ConversationID: c.ID, PostID: afterRelease.ID, ExpectedRevision: afterRelease.Revision})
	if err != nil {
		t.Fatal(err)
	}
	checkTx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer checkTx.Rollback(ctx)
	if err = tenant(ctx, checkTx, c.TenantID); err != nil {
		t.Fatal(err)
	}
	var releasedBody string
	if err = checkTx.QueryRow(ctx, `SELECT body FROM chat_post_revision WHERE tenant_id=$1 AND post_id=$2 AND revision=$3`, c.TenantID, afterRelease.ID, deletedAfterRelease.Revision).Scan(&releasedBody); err != nil {
		t.Fatal(err)
	}
	if releasedBody != "" {
		t.Fatalf("released hold still preserved a copy: got %q", releasedBody)
	}
}
