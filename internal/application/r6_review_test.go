package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type r6ReportedFixture struct{ *chatremoveHTTPFixture }

func (r6ReportedFixture) HasOpenReport(context.Context, chat.Principal, string, string, string) (bool, error) {
	return true, nil
}

// Pressing Report again on a message the person already reported, and nobody has
// decided, answers success and adds no second queue item (lane R6).
func TestR6_ReportingAgainAddsNoSecondItem(t *testing.T) {
	f := &chatremoveHTTPFixture{}
	again := r6ReportedFixture{f}
	h := ChatModerationHTTP{Service: &chat.ModerationService{Store: f}, Reports: f, Permissions: again}
	w := chatremoveHTTPRequest(t, h, "/report", `{"ConversationID":"room","PostID":"post","ReasonCode":"spam"}`, true)
	if w.Code != 200 || f.calls != 0 {
		t.Fatalf("status=%d reports written=%d", w.Code, f.calls)
	}
	h.Permissions = f
	w = chatremoveHTTPRequest(t, h, "/report", `{"ConversationID":"room","PostID":"post","ReasonCode":"spam"}`, true)
	if w.Code != 200 || f.calls != 1 {
		t.Fatalf("a first report must still be written: status=%d calls=%d", w.Code, f.calls)
	}
}
