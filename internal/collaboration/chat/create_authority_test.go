package chat

import (
	"context"
	"errors"
	"testing"
)

func TestTodo_CHAT_010_Security_CreateRequiresCurrentAuthority(t *testing.T) {
	store := &fakeStore{}
	s := NewService(store, nil)
	r := CreateConversationRequest{Principal: principal(), TenantID: "t1", Kind: PublicChannel, Name: "general"}
	if err := s.ValidateCreate(context.Background(), r); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unconfigured authority preflight=%v", err)
	}
	s.SetAuthority(rejectingAuthority{})
	if _, err := s.CreateConversation(context.Background(), r); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("revoked caller created channel: %v", err)
	}
	if store.mutations != 0 {
		t.Fatalf("denied create mutated store %d times", store.mutations)
	}
}
