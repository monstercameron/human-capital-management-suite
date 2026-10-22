package main

// R7: two tabs and one sidebar layout. A tab that loses the revision race
// (the server answers Aborted) used to read the newer layout only to take its
// revision and then write its own whole layout over it, so a section, a
// favorite or a moved conversation saved by the other tab was lost. The retry
// now carries only what this tab changed since it last held the server's
// layout: sideMerge applies the change from base to mine onto the server's
// layout.

type sideMergeRoom struct{ Host, ID string }

type sideMergeSection struct {
	ID, Name          string
	Collapsed, Manual bool
	Rooms             []sideMergeRoom
}

type sideMergeLayout struct {
	Sections           []sideMergeSection
	Starred            []string
	FavoritesCollapsed bool
}

func sideMergeSameRooms(a, b []sideMergeRoom) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}
	}
	return true
}

func sideMergeIndex(l sideMergeLayout) map[string]sideMergeSection {
	out := make(map[string]sideMergeSection, len(l.Sections))
	for _, s := range l.Sections {
		out[s.ID] = s
	}
	return out
}

func sideMergeRoomIDs(l sideMergeLayout) map[string]bool {
	out := map[string]bool{}
	for _, s := range l.Sections {
		for _, r := range s.Rooms {
			out[r.ID] = true
		}
	}
	return out
}

// sideMergePick keeps this tab's value when the tab changed it, otherwise the
// server's.
func sideMergePick[T comparable](base, mine, server T) T {
	if mine != base {
		return mine
	}
	return server
}

func sideMergeIDs(l sideMergeLayout) []string {
	ids := make([]string, 0, len(l.Sections))
	for _, s := range l.Sections {
		ids = append(ids, s.ID)
	}
	return ids
}

// sideMerge returns the layout to write: the server's layout with this tab's
// own changes (base to mine) applied. A base with no sections means this tab
// never held the server's layout, so there is no change to isolate and mine is
// returned as it is.
func sideMerge(base, mine, server sideMergeLayout) sideMergeLayout {
	if len(base.Sections) == 0 {
		return mine
	}
	baseIdx, mineIdx, serverIdx := sideMergeIndex(base), sideMergeIndex(mine), sideMergeIndex(server)
	baseRooms, mineRooms, serverRooms := sideMergeRoomIDs(base), sideMergeRoomIDs(mine), sideMergeRoomIDs(server)

	// Which sections survive, and which of them are this tab's version.
	type chosen struct {
		section  sideMergeSection
		fromMine bool
	}
	picked := map[string]chosen{}
	for id, srv := range serverIdx {
		b, inBase := baseIdx[id]
		m, inMine := mineIdx[id]
		switch {
		case inBase && !inMine:
			// This tab removed the section.
		case !inBase:
			picked[id] = chosen{section: srv}
		default:
			section := sideMergeSection{ID: id,
				Name:      sideMergePick(b.Name, m.Name, srv.Name),
				Collapsed: sideMergePick(b.Collapsed, m.Collapsed, srv.Collapsed),
				Manual:    sideMergePick(b.Manual, m.Manual, srv.Manual)}
			fromMine := !sideMergeSameRooms(b.Rooms, m.Rooms)
			if !fromMine {
				section.Rooms = append([]sideMergeRoom(nil), srv.Rooms...)
			} else {
				section.Rooms = append([]sideMergeRoom(nil), m.Rooms...)
				// What the other tab put here meanwhile stays.
				for _, r := range srv.Rooms {
					if !baseRooms[r.ID] && !mineRooms[r.ID] {
						section.Rooms = append(section.Rooms, r)
					}
				}
			}
			picked[id] = chosen{section: section, fromMine: fromMine}
		}
	}
	for id, m := range mineIdx {
		if _, there := picked[id]; there {
			continue
		}
		_, inBase := baseIdx[id]
		_, inServer := serverIdx[id]
		// A section this tab made, or one the other tab removed that this tab
		// had changed itself.
		if !inBase || (!inServer && !sideMergeSameSection(baseIdx[id], m)) {
			picked[id] = chosen{section: m, fromMine: true}
		}
	}

	// Order: the server's, unless this tab reordered the sections it knew.
	var order []string
	commonBase, commonMine := []string{}, []string{}
	for _, id := range sideMergeIDs(base) {
		if _, ok := mineIdx[id]; ok {
			commonBase = append(commonBase, id)
		}
	}
	for _, id := range sideMergeIDs(mine) {
		if _, ok := baseIdx[id]; ok {
			commonMine = append(commonMine, id)
		}
	}
	reordered := len(commonBase) != len(commonMine)
	for i := 0; !reordered && i < len(commonBase); i++ {
		reordered = commonBase[i] != commonMine[i]
	}
	inOrder := map[string]bool{}
	if reordered {
		for _, id := range sideMergeIDs(mine) {
			if _, ok := picked[id]; ok {
				order, inOrder[id] = append(order, id), true
			}
		}
		for _, id := range sideMergeIDs(server) {
			if _, ok := picked[id]; ok && !inOrder[id] {
				order, inOrder[id] = append(order, id), true
			}
		}
	} else {
		for _, id := range sideMergeIDs(server) {
			if _, ok := picked[id]; ok {
				order, inOrder[id] = append(order, id), true
			}
		}
		mineIDs := sideMergeIDs(mine)
		for i, id := range mineIDs {
			if inOrder[id] {
				continue
			}
			if _, ok := picked[id]; !ok {
				continue
			}
			// After the section that came before it in this tab, else first.
			at := 0
			if i > 0 {
				for j, other := range order {
					if other == mineIDs[i-1] {
						at = j + 1
					}
				}
			}
			order = append(order[:at], append([]string{id}, order[at:]...)...)
			inOrder[id] = true
		}
	}

	// Rooms: one place each, this tab's choice first. A room this tab left, or
	// one the other tab left, is not kept.
	placed := map[string]bool{}
	rooms := map[string][]sideMergeRoom{}
	for _, mineFirst := range []bool{true, false} {
		for _, id := range order {
			c := picked[id]
			if c.fromMine != mineFirst {
				continue
			}
			for _, r := range c.section.Rooms {
				gone := (baseRooms[r.ID] && !mineRooms[r.ID]) || (baseRooms[r.ID] && !serverRooms[r.ID])
				if placed[r.ID] || gone {
					continue
				}
				placed[r.ID] = true
				rooms[id] = append(rooms[id], r)
			}
		}
	}
	out := sideMergeLayout{FavoritesCollapsed: sideMergePick(base.FavoritesCollapsed, mine.FavoritesCollapsed, server.FavoritesCollapsed)}
	for _, id := range order {
		section := picked[id].section
		section.Rooms = rooms[id]
		out.Sections = append(out.Sections, section)
	}

	// Favorites: the server's, less what this tab unstarred, plus what it starred.
	baseStar, mineStar := sideMergeSet(base.Starred), sideMergeSet(mine.Starred)
	seen := map[string]bool{}
	keep := func(id string) {
		if placed[id] && !seen[id] {
			seen[id] = true
			out.Starred = append(out.Starred, id)
		}
	}
	for _, id := range server.Starred {
		if baseStar[id] && !mineStar[id] {
			continue
		}
		keep(id)
	}
	for _, id := range mine.Starred {
		if !baseStar[id] {
			keep(id)
		}
	}
	return out
}

func sideMergeSameSection(a, b sideMergeSection) bool {
	return a.Name == b.Name && a.Collapsed == b.Collapsed && a.Manual == b.Manual && sideMergeSameRooms(a.Rooms, b.Rooms)
}

func sideMergeSet(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}
