package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chatsaveNoticeFixture struct{ notices map[string]PrivateSavedNotice }

func (s *chatsaveNoticeFixture) DeliverPrivateSavedNotice(_ context.Context, n PrivateSavedNotice) error {
	if s.notices == nil {
		s.notices = map[string]PrivateSavedNotice{}
	}
	s.notices[n.IdempotencyKey] = n
	return nil
}

func TestTodo_CHATSAVE_001_Reminder(t *testing.T) {
	ctx := chatsaveHTTPContext(t, trust.SubjectKindHuman)
	p := chat.Principal{TenantID: "tenant-a", SubjectID: "person"}
	due := time.Now().Add(-time.Minute)
	item := chat.SavedItem{TenantID: "host", HomeTenantID: p.TenantID, PersonID: p.SubjectID, ConversationID: "room", PostID: "post", DueAt: &due, Note: "private", Post: &chat.Post{Body: "message"}}
	fixture := &chatsaveNoticeFixture{}
	sink := SavedReminderNotifications{Delivery: fixture}
	for i := 0; i < 2; i++ {
		if err := sink.NotifySaved(ctx, p, item, "same-key"); err != nil {
			t.Fatal(err)
		}
	}
	if len(fixture.notices) != 1 || fixture.notices["same-key"].RecipientSubjectID != "person" || fixture.notices["same-key"].HostTenantID != "host" {
		t.Fatalf("private recipient=%+v", fixture.notices)
	}
	if err := (SavedReminderNotifications{}).NotifySaved(ctx, p, item, "key"); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatal("missing notification path acknowledged")
	}
	item.PersonID = "other"
	if err := sink.NotifySaved(ctx, p, item, "forged"); !errors.Is(err, chat.ErrPermissionDenied) || len(fixture.notices) != 1 {
		t.Fatal("forged recipient delivered")
	}
}
