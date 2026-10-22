//go:build js && wasm

package main

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"

// R7: the wasm side of sideMerge (r7_sidebar_merge.go): the conversion to and
// from the layout document, and the parts of the document a retry must not
// overwrite with this tab's cached copy.

func sideMergeOf(l recipientLayout) sideMergeLayout {
	out := sideMergeLayout{Starred: l.Starred, FavoritesCollapsed: l.FavoritesCollapsed}
	for _, s := range l.Sections {
		section := sideMergeSection{ID: s.ID, Name: s.Name, Collapsed: s.Collapsed, Manual: s.Manual}
		for _, c := range s.Chats {
			section.Rooms = append(section.Rooms, sideMergeRoom{Host: c.HostTenantID, ID: c.ConversationID})
		}
		out.Sections = append(out.Sections, section)
	}
	return out
}

// rebaseSidebarLayout puts this tab's change (base to mine) on top of the
// layout another tab saved first, and reports whether the result differs from
// what this tab was about to write.
func rebaseSidebarLayout(base, mine, server recipientLayout) (recipientLayout, bool) {
	if len(base.Sections) == 0 {
		return mine, false
	}
	merged := sideMerge(sideMergeOf(base), sideMergeOf(mine), sideMergeOf(server))
	next := mine
	next.Sections = make([]recipientSection, 0, len(merged.Sections))
	for _, s := range merged.Sections {
		section := recipientSection{ID: s.ID, Name: s.Name, Collapsed: s.Collapsed, Manual: s.Manual}
		for _, r := range s.Rooms {
			section.Chats = append(section.Chats, recipientChatRef{HostTenantID: r.Host, ConversationID: r.ID})
		}
		next.Sections = append(next.Sections, section)
	}
	next.Starred = merged.Starred
	next.FavoritesCollapsed = merged.FavoritesCollapsed
	// This tab only carries its cached copy of these; the server's is newer.
	next.Filters = server.Filters
	next.PanesByDevice = make(map[string]chatui.PaneSizes, len(mine.PanesByDevice)+len(server.PanesByDevice))
	for device, pane := range server.PanesByDevice {
		next.PanesByDevice[device] = pane
	}
	device := recipientDeviceClass()
	if pane, ok := mine.PanesByDevice[device]; ok {
		next.PanesByDevice[device] = pane
	}
	known := make(map[string]bool, len(mine.DismissedJoinPrompts))
	for _, id := range mine.DismissedJoinPrompts {
		known[id] = true
	}
	next.DismissedJoinPrompts = append([]string(nil), mine.DismissedJoinPrompts...)
	for _, id := range server.DismissedJoinPrompts {
		if !known[id] {
			next.DismissedJoinPrompts = append(next.DismissedJoinPrompts, id)
		}
	}
	return next, !sideMergeEqual(sideMergeOf(next), sideMergeOf(mine))
}

func sideMergeEqual(a, b sideMergeLayout) bool {
	if a.FavoritesCollapsed != b.FavoritesCollapsed || len(a.Sections) != len(b.Sections) || len(a.Starred) != len(b.Starred) {
		return false
	}
	for i := range a.Sections {
		x, y := a.Sections[i], b.Sections[i]
		if x.ID != y.ID || x.Name != y.Name || x.Collapsed != y.Collapsed || x.Manual != y.Manual || !sideMergeSameRooms(x.Rooms, y.Rooms) {
			return false
		}
	}
	for i := range a.Starred {
		if a.Starred[i] != b.Starred[i] {
			return false
		}
	}
	return true
}
