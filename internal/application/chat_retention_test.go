package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestTodo_CHAT_048_Security_AdminRetentionAuthority(t *testing.T) {
	a := ChatRecordAuthority{}
	ctx := context.WithValue(context.Background(), recordAuthorizationKey{}, recordAuthorization{tenant: "tenant-a", actor: "admin", manager: true})
	evidence, err := a.Authorize(ctx, "admin", "tenant-a", "retention.configure", "")
	if err != nil || evidence == "" {
		t.Fatalf("admin retention evidence=%q err=%v", evidence, err)
	}
	for _, tc := range []struct{ actor, tenant, action string }{
		{"other", "tenant-a", "retention.configure"},
		{"admin", "tenant-b", "retention.configure"},
		{"admin", "tenant-a", "records.dispose"},
	} {
		if _, err := a.Authorize(ctx, tc.actor, tc.tenant, tc.action, ""); !errors.Is(err, chat.ErrPermissionDenied) {
			t.Fatalf("forged authority %+v err=%v", tc, err)
		}
	}
	withoutAdmin := context.WithValue(context.Background(), recordAuthorizationKey{}, recordAuthorization{tenant: "tenant-a", actor: "admin", revision: 1})
	if _, err := a.Authorize(withoutAdmin, "admin", "tenant-a", "retention.read", ""); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("conversation authority granted tenant retention: %v", err)
	}
}
