package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATBUG-068. The conversation a tab shows changes only through that tab: a
// click or a key in it, or a change of its own address. A re-read of the
// conversation list (started by a stream event, a membership change, a
// reconnect or another session's work) is none of these, so it keeps the open
// conversation. One that stopped being readable says so where it is.

// chatListingSelection is the conversation a re-read list leaves open.
//
//   - previous is the conversation open before the read, "" on the first load.
//   - address is the conversation the tab's address names, "" when it names none.
//   - addressRooms is what the address is resolved against.
//   - readable and preview say whether the read lists previous as one the viewer
//     is in, or as a public channel the viewer may look into.
//   - wasListed is true when the rail held previous before this read.
//   - complete is true when the read returned the whole list, not a first page.
//
// unreadable is true when previous was in the viewer's list and is not in a
// complete one now: the conversation stays on screen, and the caller says why
// in place. Nothing else moves the selection once there is one: the address is
// read here only for the first load, because a later change of the address is
// handled when it happens (openChatChannelFragment), and reading it again on
// every list read took the tab back to a conversation it had already left.
func chatListingSelection(previous, address string, addressRooms []chatui.Conversation, readable, preview, wasListed, complete bool) (selected string, unreadable bool) {
	if previous != "" {
		if readable || preview {
			return previous, false
		}
		return previous, wasListed && complete
	}
	if address != "" {
		resolved, _ := resolveChatChannelFragment(addressRooms, address)
		return resolved, false
	}
	return "", false
}
