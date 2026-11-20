package chat

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// brokenAuthority fails the way an unavailable directory does, not as a refusal.
type brokenAuthority struct{}

func (brokenAuthority) Authorize(context.Context, Principal, Conversation, chatpolicy.Action, time.Time) (chatpolicy.Input, error) {
	return chatpolicy.Input{}, ErrUnavailable
}

// TestIssue3735_ReminderNeedsNoSourceAccess: a reminder is due whatever state
// the saved message's channel is in. The scheduler must not depend on reading
// the message, and the sink must never be handed its text or the private note.
func TestIssue3735_ReminderNeedsNoSourceAccess(t *testing.T) {
	s, f, r := chatsaveService()
	ctx := context.Background()
	if _, err := s.SaveForLater(ctx, r); err != nil {
		t.Fatal(err)
	}
	due := time.Unix(900, 0)
	f.items[0].DueAt = &due
	f.items[0].Note = "private follow up"
	s.SetAuthority(brokenAuthority{})
	sink := &chatsaveSink{}
	if err := s.DispatchSavedReminders(ctx, r.Principal, r.TenantID, sink); err != nil || sink.calls != 1 {
		t.Fatalf("reminders=%d err=%v", sink.calls, err)
	}
}
