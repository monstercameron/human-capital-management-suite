package chat_test

import (
	"context"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestTodo_CHAT_016_ConsecutiveDeletesUseEachPostsOwnRevision is the browser
// session's failure: the first delete in a room succeeded and every later one
// came back ABORTED chat.conflict. The client holds one ListPosts snapshot and
// deletes from it, so a revision check that is per post must not be disturbed by
// a sibling's tombstone.
func TestTodo_CHAT_016_ConsecutiveDeletesUseEachPostsOwnRevision(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := principal("company-a", "alice")
	c, err := f.service.CreateConversation(ctx, chatcore.CreateConversationRequest{Principal: p, TenantID: p.TenantID, Kind: chatcore.PublicChannel, Name: "deletes", OwnerID: p.SubjectID, IdempotencyKey: "delete-room"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"one", "two", "three"} {
		if _, err := f.service.SendPost(ctx, chatcore.SendPostRequest{Principal: p, TenantID: p.TenantID, ConversationID: c.ID, Body: key, IdempotencyKey: key}); err != nil {
			t.Fatal(err)
		}
	}
	// One snapshot, exactly as a client takes it, and the revisions it carries.
	listed, err := f.service.ListPosts(ctx, chatcore.ListPostsRequest{Principal: p, TenantID: p.TenantID, ConversationID: c.ID, Page: chatcore.Page{PageSize: 50}})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Posts) != 3 {
		t.Fatalf("listed %d posts, want 3", len(listed.Posts))
	}
	for _, post := range listed.Posts {
		deleted, deleteErr := f.service.DeletePost(ctx, chatcore.DeletePostRequest{Principal: p, TenantID: p.TenantID, ConversationID: c.ID, PostID: post.ID, ExpectedRevision: post.Revision})
		if deleteErr != nil {
			t.Fatalf("delete %s at revision %d = %v", post.ID, post.Revision, deleteErr)
		}
		if !deleted.Deleted || deleted.Revision != post.Revision+1 {
			t.Fatalf("deleted post = %+v, want tombstoned at revision %d", deleted, post.Revision+1)
		}
	}
	// A repeated delete is idempotent: it asked for a tombstone and a tombstone
	// is what exists. It used to answer ABORTED chat.conflict, indistinguishable
	// from a stale revision, which is how a client that re-sent a delete filled
	// the log with conflicts.
	again, err := f.service.DeletePost(ctx, chatcore.DeletePostRequest{Principal: p, TenantID: p.TenantID, ConversationID: c.ID, PostID: listed.Posts[0].ID, ExpectedRevision: listed.Posts[0].Revision})
	if err != nil {
		t.Fatalf("repeated delete = %v, want the existing tombstone", err)
	}
	if !again.Deleted || again.Revision != listed.Posts[0].Revision+1 {
		t.Fatalf("repeated delete = %+v, want the tombstone unchanged", again)
	}
	// An edit of a tombstoned post is still a conflict; only delete is idempotent.
	if _, err := f.service.EditPost(ctx, chatcore.EditPostRequest{Principal: p, TenantID: p.TenantID, ConversationID: c.ID, PostID: listed.Posts[0].ID, Body: "resurrect", ExpectedRevision: again.Revision}); err == nil {
		t.Fatal("editing a tombstoned post succeeded")
	}
}
