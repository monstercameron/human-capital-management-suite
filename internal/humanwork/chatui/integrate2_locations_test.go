package chatui

import (
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"strings"
	"testing"
	"time"
)

func TestIntegrate2LocationsReachTimelineAndThread_Browser(t *testing.T) {
	now := time.Now()
	msg := Message{ID: "post", Revision: 2, Author: "Alex", Body: "Location note"}
	share := chat.LocationShare{ID: "share", TenantID: "tenant", ConversationID: "room", PostID: msg.ID, PostRevision: 1, Place: chat.LocationPlace{Source: chat.LocationDevice, Precision: "exact", Label: "Meeting point", Position: &chat.LocationPosition{Latitude: 1, Longitude: 2}}}
	m := Model{Locale: "en-US", CurrentUser: "reader", CurrentTenantID: "tenant", MessageLocations: map[string][]ChatmapEmbed{msg.ID: {{Share: share, Now: now, MessageRevision: 2}}}, ThreadParent: &msg, ThreadMessages: []Message{msg}}
	timeline := renderNode(t, message(m, handlers{}, msg, false))
	thread := renderNode(t, threadPane(m, handlers{}))
	for _, markup := range []string{timeline, thread} {
		if !strings.Contains(markup, "Meeting point") || !strings.Contains(markup, "chatmap-card") {
			t.Fatal("location projection missing", markup)
		}
	}
	stale := msg
	stale.Revision++
	if len(integrate2MessageLocations(m, stale)) != 0 {
		t.Fatal("stale location survived an edit")
	}
}
