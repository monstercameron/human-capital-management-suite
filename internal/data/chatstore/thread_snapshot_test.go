package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestTodo_AGENT_015_ThreadSnapshotIsAtomicTenantScopedAndFenced(t *testing.T) {
	store, _ := chatFixture(t)
	seedConversationRow(t, store, Conversation{ID: "thread-room", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{
		{MemberID: "alice", HomeTenantID: "tenant-a", Role: "manager", State: "active"},
		{MemberID: "bob", HomeTenantID: "tenant-a", Role: "member", State: "active"},
	})
	root, err := store.sendPostRaw(context.Background(), SendRequest{TenantID: "tenant-a", ConversationID: "thread-room", AuthorID: "bob", ClientKey: "root", Body: "root"})
	if err != nil {
		t.Fatal(err)
	}
	invoking, err := store.sendPostRaw(context.Background(), SendRequest{TenantID: "tenant-a", ConversationID: "thread-room", AuthorID: "alice", ClientKey: "invoke", ParentID: root.ID, Body: "question"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.sendPostRaw(context.Background(), SendRequest{TenantID: "tenant-a", ConversationID: "thread-room", AuthorID: "bob", ClientKey: "other-thread", Body: "unrelated"})
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapter(store)
	request := chat.ThreadSnapshotRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}, TenantID: "tenant-a", ConversationID: "thread-room", ThreadID: root.ID, InvokingPostID: invoking.ID, Limit: 50}
	snapshot, err := adapter.CaptureThreadSnapshot(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Posts) != 2 || snapshot.Digest == "" || snapshot.SnapshotID != "chat-thread-"+snapshot.Digest || snapshot.AuthorityRevision == 0 {
		t.Fatalf("snapshot = %+v; want only the two thread posts and server identity", snapshot)
	}
	called := false
	if err := adapter.WithThreadSnapshotFence(context.Background(), snapshot, func() error { called = true; return nil }); err != nil || !called {
		t.Fatalf("unchanged snapshot fence = %v, callback called=%t", err, called)
	}
	// A new post advances the conversation sequence and changes the exact
	// bounded thread image before admission can use the stale snapshot.
	_, err = store.sendPostRaw(context.Background(), SendRequest{TenantID: "tenant-a", ConversationID: "thread-room", AuthorID: "bob", ClientKey: "new-reply", ParentID: root.ID, Body: "new reply"})
	if err != nil {
		t.Fatal(err)
	}
	called = false
	if err := adapter.WithThreadSnapshotFence(context.Background(), snapshot, func() error { called = true; return nil }); !errors.Is(err, ErrThreadSnapshotChanged) || called {
		t.Fatalf("stale snapshot fence = %v, callback called=%t; want changed and no callback", err, called)
	}
	foreign := request
	foreign.TenantID = "tenant-b"
	foreign.Principal.TenantID = "tenant-b"
	if _, err := adapter.CaptureThreadSnapshot(context.Background(), foreign); err == nil {
		t.Fatal("foreign tenant obtained a thread snapshot")
	}
}

func TestTodo_AGENT_015_ThreadSnapshotRejectsForgedInvokingPost(t *testing.T) {
	store, _ := chatFixture(t)
	seedConversationRow(t, store, Conversation{ID: "private", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "alice", HomeTenantID: "tenant-a", Role: "manager", State: "active"}})
	post, err := store.sendPostRaw(context.Background(), SendRequest{TenantID: "tenant-a", ConversationID: "private", AuthorID: "alice", ClientKey: "post", Body: "visible"})
	if err != nil {
		t.Fatal(err)
	}
	request := chat.ThreadSnapshotRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}, TenantID: "tenant-a", ConversationID: "private", ThreadID: post.ID, InvokingPostID: "forged-post", Limit: 50}
	if _, err := NewAdapter(store).CaptureThreadSnapshot(context.Background(), request); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("forged invoking post error = %v; want permission denied", err)
	}
}

func TestTodo_AGENT_015_ThreadSnapshotFenceSerializesConcurrentThreadWrite(t *testing.T) {
	store, _ := chatFixture(t)
	seedConversationRow(t, store, Conversation{ID: "fenced", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{
		{MemberID: "alice", HomeTenantID: "tenant-a", Role: "manager", State: "active"},
		{MemberID: "bob", HomeTenantID: "tenant-a", Role: "member", State: "active"},
	})
	root, err := store.sendPostRaw(context.Background(), SendRequest{TenantID: "tenant-a", ConversationID: "fenced", AuthorID: "alice", ClientKey: "fence-root", Body: "root"})
	if err != nil {
		t.Fatal(err)
	}
	invoking, err := store.sendPostRaw(context.Background(), SendRequest{TenantID: "tenant-a", ConversationID: "fenced", AuthorID: "alice", ClientKey: "fence-invoking", ParentID: root.ID, Body: "question"})
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapter(store)
	snapshot, err := adapter.CaptureThreadSnapshot(context.Background(), chat.ThreadSnapshotRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}, TenantID: "tenant-a", ConversationID: "fenced", ThreadID: root.ID, InvokingPostID: invoking.ID, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	fenceDone := make(chan error, 1)
	go func() {
		fenceDone <- adapter.WithThreadSnapshotFence(context.Background(), snapshot, func() error { close(entered); <-release; return nil })
	}()
	<-entered
	writeStarted, writeDone := make(chan struct{}), make(chan error, 1)
	go func() {
		close(writeStarted)
		_, err := store.sendPostRaw(context.Background(), SendRequest{TenantID: "tenant-a", ConversationID: "fenced", AuthorID: "bob", ClientKey: "fence-reply", ParentID: root.ID, Body: "new reply"})
		writeDone <- err
	}()
	<-writeStarted
	select {
	case err := <-writeDone:
		t.Fatalf("thread write committed during context fence: %v", err)
	default:
	}
	close(release)
	if err := <-fenceDone; err != nil {
		t.Fatalf("fence callback: %v", err)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("thread write after fence release: %v", err)
	}
}
