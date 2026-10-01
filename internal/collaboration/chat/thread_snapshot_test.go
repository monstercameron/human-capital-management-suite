package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

type threadSnapshotStore struct {
	*fakeStore
	snapshot ThreadSnapshot
	request  ThreadSnapshotRequest
	fenced   bool
}

func (s *threadSnapshotStore) CaptureThreadSnapshot(_ context.Context, request ThreadSnapshotRequest) (ThreadSnapshot, error) {
	s.request = request
	return s.snapshot, nil
}

func (s *threadSnapshotStore) WithThreadSnapshotFence(_ context.Context, _ ThreadSnapshot, fn func() error) error {
	s.fenced = true
	return fn()
}

func TestThreadSnapshotServiceAuthorizesCaptureAndFencesCanonicalImage(t *testing.T) {
	base := &fakeStore{conversation: Conversation{ID: "c1", TenantID: "t1", Kind: PrivateChannel, Revision: 1}, membership: Membership{SubjectID: "alice"}}
	store := &threadSnapshotStore{fakeStore: base, snapshot: ThreadSnapshot{
		TenantID: "t1", ConversationID: "c1", ThreadID: "root", InvokingPostID: "invoke", PrincipalTenantID: "t1", PrincipalID: "alice", Revision: 2, AuthorityRevision: 1,
		Posts: []Post{{ID: "invoke", TenantID: "t1", ConversationID: "c1", AuthorID: "alice", Revision: 1, Sequence: 2, Body: "hello"}},
	}}
	digest, err := ThreadSnapshotDigest(store.snapshot)
	if err != nil {
		t.Fatal(err)
	}
	store.snapshot.Digest = digest
	store.snapshot.SnapshotID = "chat-thread-" + digest
	service := NewService(store, func() time.Time { return time.Unix(1, 0).UTC() })
	service.SetAuthority(verifiedAuthority{store: base})
	request := ThreadSnapshotRequest{Principal: Principal{TenantID: "t1", SubjectID: "alice"}, TenantID: "t1", ConversationID: "c1", ThreadID: "root", InvokingPostID: "invoke", Limit: 20}
	got, err := service.CaptureThreadSnapshot(context.Background(), request)
	if err != nil || got.Digest != digest || store.request.InvokingPostID != "invoke" {
		t.Fatalf("capture = %+v, request=%+v, err=%v", got, store.request, err)
	}
	called := false
	if err := service.WithThreadSnapshotFence(context.Background(), got, func() error { called = true; return nil }); err != nil || !called || !store.fenced {
		t.Fatalf("fence err=%v called=%t storeFenced=%t", err, called, store.fenced)
	}
	forged := got
	forged.Posts = append([]Post(nil), got.Posts...)
	forged.Posts[0].Body = "forged"
	if err := service.WithThreadSnapshotFence(context.Background(), forged, func() error { t.Fatal("forged callback ran"); return nil }); !errors.Is(err, ErrThreadSnapshotUnavailable) {
		t.Fatalf("forged snapshot error = %v", err)
	}
}

func TestThreadSnapshotServiceFailsClosedWithoutAtomicStoreExtension(t *testing.T) {
	base := &fakeStore{conversation: Conversation{ID: "c1", TenantID: "t1", Kind: PublicChannel, Revision: 1}, membership: Membership{SubjectID: "alice"}}
	service := newTestService(base, func() time.Time { return time.Unix(1, 0).UTC() })
	_, err := service.CaptureThreadSnapshot(context.Background(), ThreadSnapshotRequest{Principal: Principal{TenantID: "t1", SubjectID: "alice"}, TenantID: "t1", ConversationID: "c1", ThreadID: "root", InvokingPostID: "invoke", Limit: 1})
	if !errors.Is(err, ErrThreadSnapshotUnavailable) {
		t.Fatalf("missing atomic store error = %v", err)
	}
}
