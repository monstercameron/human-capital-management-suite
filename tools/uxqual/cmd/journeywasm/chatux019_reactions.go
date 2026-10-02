package main

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"

// notePersonOnChip records who else reacted with an emoji, so the chip can be
// spoken as "You and Sam reacted with eyes" (CHATUX-019). The viewer is not
// listed: the chip's Mine flag says it, and the page puts "You" first.
func notePersonOnChip(chips []chatui.ReactionChip, emoji, subject string, viewer bool) {
	if viewer || subject == "" {
		return
	}
	for i := range chips {
		if chips[i].Emoji != emoji {
			continue
		}
		for _, known := range chips[i].PeopleIDs {
			if known == subject {
				return
			}
		}
		chips[i].PeopleIDs = append(chips[i].PeopleIDs, subject)
		return
	}
}
