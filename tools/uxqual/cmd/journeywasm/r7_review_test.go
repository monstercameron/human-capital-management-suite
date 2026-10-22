package main

import (
	"reflect"
	"testing"
)

func r7Room(id string) sideMergeRoom { return sideMergeRoom{Host: "h", ID: id} }

func r7Sec(id string, rooms ...string) sideMergeSection {
	s := sideMergeSection{ID: id, Name: id}
	for _, r := range rooms {
		s.Rooms = append(s.Rooms, r7Room(r))
	}
	return s
}

func r7IDs(l sideMergeLayout) []string { return sideMergeIDs(l) }

func r7Where(l sideMergeLayout, room string) []string {
	var out []string
	for _, s := range l.Sections {
		for _, r := range s.Rooms {
			if r.ID == room {
				out = append(out, s.ID)
			}
		}
	}
	return out
}

// Tab B starred a conversation while tab A had added a section and moved a
// room into it. B's retry must keep A's section and A's move.
func TestR7_SidebarConflictKeepsTheOtherTabsSection(t *testing.T) {
	base := sideMergeLayout{Sections: []sideMergeSection{r7Sec("channels", "c1", "c2", "c3")}}
	mine := sideMergeLayout{Sections: []sideMergeSection{r7Sec("channels", "c1", "c2", "c3")}, Starred: []string{"c1"}}
	server := sideMergeLayout{Sections: []sideMergeSection{r7Sec("custom-a", "c3"), r7Sec("channels", "c1", "c2")}}

	got := sideMerge(base, mine, server)
	if !reflect.DeepEqual(r7IDs(got), []string{"custom-a", "channels"}) {
		t.Fatalf("sections = %v, want the other tab's section kept", r7IDs(got))
	}
	if where := r7Where(got, "c3"); !reflect.DeepEqual(where, []string{"custom-a"}) {
		t.Fatalf("c3 sits in %v, want only the section the other tab moved it to", where)
	}
	if !reflect.DeepEqual(got.Starred, []string{"c1"}) {
		t.Fatalf("starred = %v, want this tab's favorite kept", got.Starred)
	}
}

// Both tabs starred something: the retry holds both favorites, and an unstar
// here removes only the one this tab unstarred.
func TestR7_SidebarConflictMergesFavorites(t *testing.T) {
	base := sideMergeLayout{Sections: []sideMergeSection{r7Sec("channels", "c1", "c2", "c3")}, Starred: []string{"c1"}}
	mine := sideMergeLayout{Sections: []sideMergeSection{r7Sec("channels", "c1", "c2", "c3")}, Starred: []string{"c3"}}
	server := sideMergeLayout{Sections: []sideMergeSection{r7Sec("channels", "c1", "c2", "c3")}, Starred: []string{"c1", "c2"}}

	got := sideMerge(base, mine, server)
	if !reflect.DeepEqual(got.Starred, []string{"c2", "c3"}) {
		t.Fatalf("starred = %v, want c2 (other tab) and c3 (this tab), c1 unstarred here", got.Starred)
	}
}

// This tab made a section; the other tab starred a room. Neither is lost, and
// a room that both moved ends in the section this tab chose.
func TestR7_SidebarConflictKeepsThisTabsSectionAndMove(t *testing.T) {
	base := sideMergeLayout{Sections: []sideMergeSection{r7Sec("channels", "c1", "c2")}}
	mine := sideMergeLayout{Sections: []sideMergeSection{r7Sec("custom-mine", "c1"), r7Sec("channels", "c2")}}
	server := sideMergeLayout{Sections: []sideMergeSection{r7Sec("custom-theirs", "c1"), r7Sec("channels", "c2")}, Starred: []string{"c2"}}

	got := sideMerge(base, mine, server)
	if where := r7Where(got, "c1"); !reflect.DeepEqual(where, []string{"custom-mine"}) {
		t.Fatalf("c1 sits in %v, want exactly the section this tab chose", where)
	}
	if len(got.Sections) != 3 || !reflect.DeepEqual(got.Starred, []string{"c2"}) {
		t.Fatalf("merged layout lost a section or a favorite: %v / %v", r7IDs(got), got.Starred)
	}
}

// A conversation this tab left, or one the other tab left, is not written back
// into a section: the server would refuse the whole layout for it.
func TestR7_SidebarConflictDropsLeftConversations(t *testing.T) {
	base := sideMergeLayout{Sections: []sideMergeSection{r7Sec("channels", "c1", "c2", "c3")}, Starred: []string{"c2"}}
	// This tab left c1; the other tab left c3 and starred c1 before that.
	mine := sideMergeLayout{Sections: []sideMergeSection{r7Sec("channels", "c2", "c3")}, Starred: []string{"c2"}}
	server := sideMergeLayout{Sections: []sideMergeSection{r7Sec("channels", "c1", "c2")}, Starred: []string{"c1", "c2"}}

	got := sideMerge(base, mine, server)
	if !reflect.DeepEqual(r7Where(got, "c1"), []string(nil)) || !reflect.DeepEqual(r7Where(got, "c3"), []string(nil)) {
		t.Fatalf("a left conversation was kept: %+v", got.Sections)
	}
	if !reflect.DeepEqual(got.Starred, []string{"c2"}) {
		t.Fatalf("starred = %v, want only the conversation still held", got.Starred)
	}
}

// A tab that changed nothing in the sidebar document takes the server's
// layout whole, and a tab that never held one writes its own.
func TestR7_SidebarConflictWithNoLocalChangeTakesTheServer(t *testing.T) {
	base := sideMergeLayout{Sections: []sideMergeSection{r7Sec("channels", "c1", "c2")}}
	server := sideMergeLayout{Sections: []sideMergeSection{r7Sec("custom-a", "c2"), r7Sec("channels", "c1")}, Starred: []string{"c2"}, FavoritesCollapsed: true}
	if got := sideMerge(base, base, server); !reflect.DeepEqual(got, server) {
		t.Fatalf("unchanged tab = %+v, want the server's layout %+v", got, server)
	}
	mine := sideMergeLayout{Sections: []sideMergeSection{r7Sec("channels", "c1")}}
	if got := sideMerge(sideMergeLayout{}, mine, server); !reflect.DeepEqual(got, mine) {
		t.Fatalf("a tab with no base = %+v, want its own layout", got)
	}
}
