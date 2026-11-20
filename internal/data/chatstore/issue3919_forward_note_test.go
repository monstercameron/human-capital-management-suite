package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// A forward into a direct or group conversation keeps the sharer's note beside
// the original message through a real store, and a conversation the sharer is
// not in refuses the forward and keeps the note out of it.
func TestIssue3919_ForwardNoteSurvivesTheStore_Integration(t *testing.T) {
	store := adapterDB(t)
	ctx := context.Background()
	principal := chat.Principal{TenantID: "tenant-a", SubjectID: "carol"}
	rooms := []struct {
		id, owner string
		kind      chat.ConversationKind
	}{
		{"source", "alice", chat.PublicChannel},
		{"dm", "alice", chat.Direct},
		{"group", "alice", chat.Group},
		{"others-dm", "bob", chat.Direct},
	}
	for _, fixture := range rooms {
		c := chat.Conversation{ID: fixture.id, TenantID: principal.TenantID, Kind: fixture.kind, Name: fixture.id, OwnerID: fixture.owner, Revision: 1}
		m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: fixture.owner, Role: chat.Manager, HistoryVisibility: chat.FullHistory}
		members := []chat.Membership{m}
		if fixture.id != "others-dm" {
			members = append(members, chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "carol", Role: chat.Member, HistoryVisibility: chat.FullHistory})
		}
		if _, err := store.CreateConversation(ctx, c, members, ""); err != nil {
			t.Fatal(err)
		}
	}
	source, err := store.SendPost(ctx, chat.SendPostRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}, TenantID: principal.TenantID, ConversationID: "source", IdempotencyKey: "original"}, chat.Post{AuthorID: "alice", Body: "launch moved to Friday"})
	if err != nil {
		t.Fatal(err)
	}
	service := chat.NewService(store, func() time.Time { return time.Now().UTC() })
	service.SetAuthority(forwardingAuthority{store: store})
	forward := func(dest, note string) (chat.Post, error) {
		return service.ForwardPost(ctx, chat.ForwardPostRequest{Principal: principal, SourceTenantID: principal.TenantID, SourceConversationID: "source", SourcePostID: source.ID, DestinationTenantID: principal.TenantID, DestinationConversationID: dest, IdempotencyKey: "fwd-" + dest, Note: note})
	}
	for _, dest := range []string{"dm", "group"} {
		first, err := forward(dest, "  worth a look  ")
		if err != nil {
			t.Fatalf("%s: %v", dest, err)
		}
		retry, err := forward(dest, "  worth a look  ")
		if err != nil || retry.ID != first.ID {
			t.Fatalf("%s retry = %+v, %v", dest, retry, err)
		}
		posts, err := store.ListPosts(ctx, principal, principal.TenantID, dest, 0, chat.Page{PageSize: 10}, chat.PostWindow{})
		if err != nil || len(posts.Posts) != 1 {
			t.Fatalf("%s posts = %+v, %v", dest, posts, err)
		}
		got := posts.Posts[0]
		if got.AuthorID != "carol" || got.Body != "launch moved to Friday" || got.SourceAttribution == nil || got.SourceAttribution.Note != "worth a look" || got.SourceAttribution.OriginalAuthorID != "alice" {
			t.Fatalf("%s forwarded post = %+v attribution %+v", dest, got, got.SourceAttribution)
		}
	}
	if _, err := forward("others-dm", "should not land"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("forward into a conversation the sharer is not in = %v", err)
	}
	posts, err := store.ListPosts(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "bob"}, principal.TenantID, "others-dm", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(posts.Posts) != 0 {
		t.Fatalf("refused destination posts = %+v, %v", posts, err)
	}
}
