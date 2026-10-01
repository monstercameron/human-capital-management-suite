package chatresource

import (
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestTodo_AGENTP_011_EphemeralSSEProjection(t *testing.T) {
	now := time.Unix(10, 0).UTC()
	got := projectEphemeral(chatcore.EphemeralDelivery{ID: "e1", ThreadID: "root", Body: "private", OnlyVisibleToYou: true, CreatedAt: now, ExpiresAt: now.Add(time.Hour), ThreadLink: "/c/root"})
	if got.Ephemeral == nil || !got.Ephemeral.OnlyVisibleToYou || got.Ephemeral.Body != "private" {
		t.Fatalf("ephemeral delivery was not projected: %+v", got)
	}
}
