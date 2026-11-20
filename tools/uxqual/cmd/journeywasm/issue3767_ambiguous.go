package main

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// resolveChatChannelAddress is resolveChatChannelFragment for opening an
// address: when a name matches several channels (resolveChatChannelFragment
// leaves it unresolved so the address bar keeps ids for them), one of the
// matches is opened rather than the name being sent to the server as an id,
// which answered "This channel is no longer available" (issue 3767). The match
// whose name is spelled exactly like the address wins, then one the reader has
// joined, then the first in list order.
func resolveChatChannelAddress(conversations []chatui.Conversation, value string) (string, bool) {
	if resolved, ok := resolveChatChannelFragment(conversations, value); ok {
		return resolved, true
	}
	best, bestScore := "", 0
	for _, conversation := range conversations {
		if conversation.Kind != chatui.PublicChannel && conversation.Kind != chatui.PrivateChannel {
			continue
		}
		if !strings.EqualFold(conversation.Name, value) {
			continue
		}
		score := 1
		if conversation.Name == value {
			score += 2
		}
		if conversation.Joined {
			score++
		}
		if score > bestScore {
			best, bestScore = conversation.ID, score
		}
	}
	if best == "" {
		return value, false
	}
	return best, true
}
