package main

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATSIDE-001: where a conversation lands when it is put in a section. The
// rule is the same wherever it comes from (a move, a drag, a deleted section's
// conversations going home): a section the person has ordered by hand takes it
// at the end, and any other section keeps its conversations sorted by name and
// takes it at its sorted place.

// chatside001ByHand reports whether a section's order is the person's own: they
// used Move up or Move down in it (Manual), or its conversations are not in
// name order, which only a hand ordering, kept from before the flag existed,
// leaves behind.
func chatside001ByHand(section chatui.SidebarSection) bool {
	if section.Manual {
		return true
	}
	for i := 1; i < len(section.Chats); i++ {
		if strings.ToLower(section.Chats[i-1].Name) > strings.ToLower(section.Chats[i].Name) {
			return true
		}
	}
	return false
}

// chatside001Place adds c to the section as the rule above says.
func chatside001Place(section *chatui.SidebarSection, c chatui.Conversation) {
	if chatside001ByHand(*section) {
		section.Chats = append(section.Chats, c)
		return
	}
	section.Chats = insertChannelInOrder(section.Chats, c)
}

// chatside001InsertSection puts a section the person has just made at the top of
// their list, under Favorites. Everything after that is their own order: they
// can move any section, built-in or their own, and a saved order is never
// rearranged.
func chatside001InsertSection(sections []chatui.SidebarSection, section chatui.SidebarSection) []chatui.SidebarSection {
	return append([]chatui.SidebarSection{section}, sections...)
}
