package chat

import "testing"

// A word the workspace masks for readers must be masked in the sharer's note
// too, in every place a reader sees the forwarded message, without changing the
// stored attribution. A failed filter lookup must not expose the note.
func TestIssue3919_ReaderMaskCoversTheForwardNote_Security(t *testing.T) {
	ctx := t.Context()
	service, store, _ := maskFixture(t, "mask")
	forwarded := maskPost("p1", "sharer", "hello friends")
	forwarded.SourceAttribution = &SourceAttribution{TenantID: "t", ConversationID: "c0", PostID: "src", OriginalAuthorID: "writer", Note: "see the quartz part"}
	store.posts = []Post{forwarded}
	reader := Principal{TenantID: "t", SubjectID: "reader"}

	page, err := service.ListPosts(ctx, ListPostsRequest{Principal: reader, TenantID: "t", ConversationID: "c", Page: Page{PageSize: 50}})
	if err != nil || len(page.Posts) != 1 {
		t.Fatalf("list: %v %d", err, len(page.Posts))
	}
	got := page.Posts[0].SourceAttribution
	if got == nil || got.Note != "see the [removed word] part" || got.OriginalAuthorID != "writer" {
		t.Fatalf("attribution = %+v, want the note masked and the author kept", got)
	}
	if store.posts[0].SourceAttribution.Note != "see the quartz part" {
		t.Fatal("the stored attribution was altered")
	}

	event := service.MaskWatchEvent(ctx, reader, "t", "c", WatchEvent{Event: ConversationEvent{Post: &forwarded}})
	if n := event.Event.Post.SourceAttribution.Note; n != "see the [removed word] part" {
		t.Fatalf("live event note = %q", n)
	}
	if forwarded.SourceAttribution.Note != "see the quartz part" {
		t.Fatal("the event copy altered the original attribution")
	}
}
