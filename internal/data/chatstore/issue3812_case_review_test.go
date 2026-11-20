package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Another workspace cannot bind a conversation it does not own, and learns
// nothing from the refusal.
func TestIssue3812_CaseStoreRefusesAnotherWorkspace_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	caseSeed(t, s, "host", "thread", "GROUP", "mgr", "hr")
	caseSeed(t, s, "other", "thread-other", "GROUP", "mgr", "hr")
	_, err := s.BindCase(ctx, "other", "x1", chat.CaseBinding{ConversationID: "thread", CaseRef: "ER-1", OwnerID: "hr", Restricted: true, BoundBy: "admin"})
	if !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("cross-tenant bind: err = %v; want not found", err)
	}
	if _, found, _ := s.ActiveCase(ctx, "host", "thread"); found {
		t.Fatal("another workspace bound the conversation")
	}
}
