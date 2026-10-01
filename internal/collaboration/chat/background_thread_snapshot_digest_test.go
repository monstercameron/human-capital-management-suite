package chat

import (
	"errors"
	"testing"
)

func TestBackgroundThreadSnapshotDigestBindsReaderRevisionAndPosts(t *testing.T) {
	snapshot := BackgroundThreadSnapshot{
		TenantID: "tenant-a", ConversationID: "room-a", ThreadID: "root-a", InvokingPostID: "post-a",
		ReaderTenantID: "tenant-a", ReaderID: "alice", Revision: 12, AuthorityRevision: 4,
		Posts: []Post{{ID: "root-a", TenantID: "tenant-a", ConversationID: "room-a", AuthorID: "bob", AuthorHomeTenantID: "tenant-a", Sequence: 8, Revision: 1, Body: "root"},
			{ID: "post-a", TenantID: "tenant-a", ConversationID: "room-a", AuthorID: "alice", AuthorHomeTenantID: "tenant-a", Sequence: 9, Revision: 2, ParentID: "root-a", Body: "question"}},
	}
	first, err := BackgroundThreadSnapshotDigest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BackgroundThreadSnapshotDigest(snapshot)
	if err != nil || first != second || len(first) != len("sha256:")+64 {
		t.Fatalf("digest = %q, second = %q, err = %v", first, second, err)
	}
	snapshot.ReaderID = "mallory"
	changedReader, err := BackgroundThreadSnapshotDigest(snapshot)
	if err != nil || changedReader == first {
		t.Fatalf("reader change digest = %q, err = %v; want a different digest", changedReader, err)
	}
	snapshot.ReaderID = "alice"
	snapshot.Posts[1].Revision++
	changedPost, err := BackgroundThreadSnapshotDigest(snapshot)
	if err != nil || changedPost == first {
		t.Fatalf("post revision digest = %q, err = %v; want a different digest", changedPost, err)
	}
}

func TestBackgroundThreadSnapshotDigestRejectsIncompleteOrForeignImages(t *testing.T) {
	base := BackgroundThreadSnapshot{
		TenantID: "tenant-a", ConversationID: "room-a", ThreadID: "root-a", InvokingPostID: "post-a",
		ReaderTenantID: "tenant-a", ReaderID: "alice", Posts: []Post{{ID: "post-a", TenantID: "tenant-a", ConversationID: "room-a", AuthorID: "alice", Revision: 1}},
	}
	tests := []struct {
		name   string
		mutate func(*BackgroundThreadSnapshot)
	}{
		{name: "missing reader", mutate: func(snapshot *BackgroundThreadSnapshot) { snapshot.ReaderID = "" }},
		{name: "foreign reader tenant", mutate: func(snapshot *BackgroundThreadSnapshot) { snapshot.ReaderTenantID = "tenant-b" }},
		{name: "foreign post", mutate: func(snapshot *BackgroundThreadSnapshot) { snapshot.Posts[0].TenantID = "tenant-b" }},
		{name: "deleted post", mutate: func(snapshot *BackgroundThreadSnapshot) { snapshot.Posts[0].Deleted = true }},
		{name: "unversioned post", mutate: func(snapshot *BackgroundThreadSnapshot) { snapshot.Posts[0].Revision = 0 }},
		{name: "empty thread", mutate: func(snapshot *BackgroundThreadSnapshot) { snapshot.Posts = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := base
			snapshot.Posts = append([]Post(nil), base.Posts...)
			tc.mutate(&snapshot)
			if _, err := BackgroundThreadSnapshotDigest(snapshot); !errors.Is(err, ErrBackgroundThreadSnapshotUnavailable) {
				t.Fatalf("digest error = %v", err)
			}
		})
	}
}
