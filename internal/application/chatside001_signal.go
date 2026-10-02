package application

import (
	"context"
	"strconv"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
)

// PublishSidebarChanged tells every live watch the person holds that their
// sidebar layout was saved at revision, so a second tab reads it again at once
// instead of on its next periodic read. It reaches the person's own sessions
// only and returns how many it reached; with streaming disabled it reaches none
// and the periodic read stays the way a tab finds out.
func (r *ChatStreamRuntime) PublishSidebarChanged(ctx context.Context, p chatcore.Principal, revision uint64) int {
	if r == nil || r.stream == nil {
		return 0
	}
	return r.stream.PublishSignal(ctx, p.TenantID, p.SubjectID, chatstream.SignalSidebarLayoutChanged, []byte(strconv.FormatUint(revision, 10)))
}

// chatSidebarSignalEvent is the watch event a sidebar notice becomes: a
// recipient-only delivery with the signal's id, no thread and the revision as
// its body, which no message renderer accepts as a message.
func chatSidebarSignalEvent(event chatstream.Event, cursor string) chatcore.WatchEvent {
	return chatcore.WatchEvent{
		EphemeralDelivery: &chatcore.EphemeralDelivery{ID: chatrecipient.SidebarChangedDeliveryID, Body: string(event.Payload), OnlyVisibleToYou: true},
		ResumeCursor:      cursor,
	}
}
