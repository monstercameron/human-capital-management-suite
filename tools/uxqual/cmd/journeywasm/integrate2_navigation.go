package main

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"

// An explicit address remains selected while the server resolves its access.
// Retained conversation state is only a fallback when the address names no room.
func integrate2AddressSelection(rooms []chatui.Conversation, address, previous string) string {
	if address != "" {
		selected, _ := resolveChatChannelFragment(rooms, address)
		return selected
	}
	for _, room := range rooms {
		if room.ID == previous {
			return previous
		}
	}
	if len(rooms) > 0 {
		return rooms[0].ID
	}
	return ""
}

func integrate2RetryAllowed(trusted bool, action, id string, disabled bool) bool {
	return trusted && action == "retry" && id != "" && !disabled
}
