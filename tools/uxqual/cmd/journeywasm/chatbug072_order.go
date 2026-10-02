package main

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// insertChannelInOrder puts a channel the rail has not seen before into its
// section at its alphabetical place: after the last row that sorts at or before
// its name. A list the person has ordered by hand keeps its order around the
// new row, and one left alphabetical stays alphabetical, which is what Browse
// channels already does (CHATBUG-072).
func insertChannelInOrder(chats []chatui.Conversation, c chatui.Conversation) []chatui.Conversation {
	key := strings.ToLower(c.Name)
	at := 0
	for i, existing := range chats {
		if strings.ToLower(existing.Name) <= key {
			at = i + 1
		}
	}
	out := make([]chatui.Conversation, 0, len(chats)+1)
	out = append(out, chats[:at]...)
	out = append(out, c)
	return append(out, chats[at:]...)
}
