package chat

import (
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestTodo_AGENTP_011_EphemeralWireProjection(t *testing.T) {
	now := time.Unix(10, 0).UTC()
	got := ephemeralDelivery(chatcore.EphemeralDelivery{ID: "e1", ThreadID: "root", Body: "private", OnlyVisibleToYou: true, CreatedAt: now, ExpiresAt: now.Add(time.Hour), ThreadLink: "/c/root"})
	if got == nil || !got.OnlyVisibleToYou || got.GetBody() != "private" {
		t.Fatalf("ephemeral delivery was not projected: %+v", got)
	}
}
