package main

import (
	"strconv"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

// CHATSIDE-001 part 5: a second tab picks a layout change up from the event
// stream. The server tells every watch the person holds, in any conversation,
// that the layout was saved at some revision (an ephemeral delivery with the
// signal's id and the revision as its body). The tab that saved it already
// holds that revision; any other tab reads the layout again. The periodic read
// stays the fallback for a tab with no open watch or a notice that was dropped.

// chatside001SignalRevision is the revision a layout-changed delivery carries,
// and false for any other delivery.
func chatside001SignalRevision(delivery *chatv1.EphemeralDelivery) (uint64, bool) {
	if delivery == nil || delivery.GetId() != chatrecipient.SidebarChangedDeliveryID {
		return 0, false
	}
	revision, err := strconv.ParseUint(delivery.GetBody(), 10, 64)
	return revision, err == nil && revision > 0
}

// chatside001ShouldReread decides what a notice means for this tab: read again
// when it announces a revision newer than the one held, and not while a change
// of this tab's own is still on its way to the server (that write either lands
// or conflicts, and a conflict is already rebased on the newer layout).
func chatside001ShouldReread(known, announced uint64, pending bool) bool {
	return announced > known && !pending
}

// chatside001SignalStream hands every layout-changed notice to onSignal and
// passes every message on unchanged, so the conversation drain behind it does
// not know the notice exists.
type chatside001SignalStream struct {
	next     chatEventStream
	onSignal func(revision uint64)
}

func (s *chatside001SignalStream) Recv() (*chatv1.WatchConversationResponse, error) {
	message, err := s.next.Recv()
	if err == nil && message != nil && s.onSignal != nil {
		if revision, ok := chatside001SignalRevision(message.GetEphemeralDelivery()); ok {
			s.onSignal(revision)
		}
	}
	return message, err
}
