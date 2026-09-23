package chat

import (
	"context"
	"errors"
	"testing"
)

type parentLookupFaultStore struct {
	*fakeStore
	err error
}

func (s parentLookupFaultStore) GetPost(context.Context, string, string, string) (Post, error) {
	return Post{}, s.err
}

func TestSendPostValidatesThreadParentInSameConversation(t *testing.T) {
	store := &fakeStore{
		conversation: Conversation{ID: "room-a", TenantID: "tenant-a", Kind: PublicChannel, Revision: 1},
		post:         Post{ID: "parent-a", TenantID: "tenant-a", ConversationID: "room-a", AuthorID: "alice"},
	}
	service := newTestService(store, nil)
	request := SendPostRequest{Principal: Principal{TenantID: "tenant-a", SubjectID: "alice"}, TenantID: "tenant-a", ConversationID: "room-a", Body: "reply", ParentID: "parent-a", IdempotencyKey: "reply-1"}
	created, err := service.SendPost(context.Background(), request)
	if err != nil || store.sent.ParentID != "parent-a" {
		t.Fatalf("created=%+v stored=%+v err=%v", created, store.sent, err)
	}

	store.sent = Post{}
	request.ParentID = "parent-from-another-room"
	store.post = Post{ID: request.ParentID, TenantID: "tenant-a", ConversationID: "room-b", AuthorID: "alice"}
	_, err = service.SendPost(context.Background(), request)
	if !errors.Is(err, ErrInvalidArgument) || store.sent.ID != "" {
		t.Fatalf("cross-room parent err=%v stored=%+v, want invalid argument and no write", err, store.sent)
	}

	request.ParentID = "missing-parent"
	store.post = Post{ID: "another-post", TenantID: "tenant-a", ConversationID: "room-a"}
	_, err = service.SendPost(context.Background(), request)
	if !errors.Is(err, ErrInvalidArgument) || store.sent.ID != "" {
		t.Fatalf("missing parent err=%v stored=%+v, want invalid argument and no write", err, store.sent)
	}
}

func TestSendPostParentLookupFailureIsUnavailable(t *testing.T) {
	base := &fakeStore{conversation: Conversation{ID: "room-a", TenantID: "tenant-a", Kind: PublicChannel, Revision: 1}}
	store := parentLookupFaultStore{fakeStore: base, err: errors.New("database connection reset")}
	service := NewService(store, nil)
	service.SetAuthority(verifiedAuthority{store: base})
	request := SendPostRequest{Principal: Principal{TenantID: "tenant-a", SubjectID: "alice"}, TenantID: "tenant-a", ConversationID: "room-a", Body: "reply", ParentID: "parent-a", IdempotencyKey: "reply-2"}
	_, err := service.SendPost(context.Background(), request)
	if !errors.Is(err, ErrUnavailable) || base.mutations != 0 {
		t.Fatalf("parent lookup err=%v mutations=%d, want unavailable and no write", err, base.mutations)
	}
}
