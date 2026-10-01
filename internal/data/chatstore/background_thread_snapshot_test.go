package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestBackgroundThreadSnapshotReadsCurrentMemberThreadWithoutPrincipal(t *testing.T) {
	store := adapterDB(t)
	ctx := context.Background()
	conversation := chat.Conversation{ID: "background-room", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "background-room", OwnerID: "alice", Revision: 1}
	members := []chat.Membership{
		{ConversationID: conversation.ID, TenantID: conversation.TenantID, HomeTenantID: conversation.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory},
		{ConversationID: conversation.ID, TenantID: conversation.TenantID, HomeTenantID: conversation.TenantID, SubjectID: "bob", Role: chat.Member, HistoryVisibility: chat.FullHistory},
	}
	if _, err := store.CreateConversation(ctx, conversation, members, ""); err != nil {
		t.Fatal(err)
	}
	root, err := store.SendPost(ctx, chat.SendPostRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "bob"}, TenantID: "tenant-a", ConversationID: conversation.ID, IdempotencyKey: "root"}, chat.Post{AuthorID: "bob", Body: "thread root"})
	if err != nil {
		t.Fatal(err)
	}
	invoking, err := store.SendPost(ctx, chat.SendPostRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}, TenantID: "tenant-a", ConversationID: conversation.ID, ParentID: root.ID, IdempotencyKey: "invoking"}, chat.Post{AuthorID: "alice", Body: "question"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.CaptureBackgroundThreadSnapshot(ctx, chat.BackgroundThreadSnapshotRequest{
		TenantID: "tenant-a", ReaderID: "alice", ConversationID: conversation.ID,
		ThreadID: root.ID, InvokingPostID: invoking.ID, Limit: chat.MaxThreadSnapshotPosts,
	})
	if err != nil {
		t.Fatalf("capture background thread: %v", err)
	}
	digest, err := chat.BackgroundThreadSnapshotDigest(snapshot)
	if err != nil || digest != snapshot.Digest || snapshot.SnapshotID != "chat-background-thread-"+digest ||
		snapshot.ReaderTenantID != "tenant-a" || snapshot.ReaderID != "alice" || len(snapshot.Posts) != 2 ||
		snapshot.Posts[0].ID != root.ID || snapshot.Posts[1].ID != invoking.ID || snapshot.Posts[1].Body != "question" || snapshot.AuthorityRevision == 0 {
		t.Fatalf("background thread snapshot = %+v, digest error = %v", snapshot, err)
	}
}

func TestBackgroundThreadSnapshotRejectsMissingMemberAndInvokingPost(t *testing.T) {
	store := adapterDB(t)
	ctx := context.Background()
	conversation := chat.Conversation{ID: "background-private", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "private", OwnerID: "alice", Revision: 1}
	members := []chat.Membership{{ConversationID: conversation.ID, TenantID: conversation.TenantID, HomeTenantID: conversation.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}
	if _, err := store.CreateConversation(ctx, conversation, members, ""); err != nil {
		t.Fatal(err)
	}
	post, err := store.SendPost(ctx, chat.SendPostRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}, TenantID: "tenant-a", ConversationID: conversation.ID, IdempotencyKey: "question"}, chat.Post{AuthorID: "alice", Body: "question"})
	if err != nil {
		t.Fatal(err)
	}
	request := chat.BackgroundThreadSnapshotRequest{TenantID: "tenant-a", ConversationID: conversation.ID, ThreadID: post.ID, InvokingPostID: post.ID, Limit: chat.MaxThreadSnapshotPosts}
	request.ReaderID = "mallory"
	if _, err := store.CaptureBackgroundThreadSnapshot(ctx, request); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("nonmember snapshot error = %v, want no visible rows", err)
	}
	request.ReaderID = "alice"
	request.InvokingPostID = "missing-post"
	if _, err := store.CaptureBackgroundThreadSnapshot(ctx, request); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("missing invoking post error = %v, want permission denied", err)
	}
}
