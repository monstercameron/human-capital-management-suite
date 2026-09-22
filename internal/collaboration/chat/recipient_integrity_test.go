package chat

import (
	"context"
	"errors"
	"testing"
)

func TestTodo_CHAT_034_Security_RevokedPreferences(t *testing.T) {
	f := &fakeStore{conversation: conversation()}
	s := newTestService(f, nil)
	p := principal()
	_, err := s.GetPreferences(context.Background(), GetPreferencesRequest{Principal: p, TenantID: "t1", ConversationID: "c1"})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("revoked preference read: %v", err)
	}
	_, err = s.UpdatePreferences(context.Background(), UpdatePreferencesRequest{Principal: p, Preferences: NotificationPreferences{TenantID: "t1", ConversationID: "c1", Muted: true}, ExpectedRevision: 1})
	if !errors.Is(err, ErrPermissionDenied) || f.mutations != 0 {
		t.Fatalf("revoked preference write: %v mutations=%d", err, f.mutations)
	}
}
