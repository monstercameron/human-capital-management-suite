package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func chatside001Rooms(names ...string) []chatui.Conversation {
	var out []chatui.Conversation
	for _, name := range names {
		out = append(out, chatui.Conversation{ID: name, Name: name, Kind: chatui.PublicChannel, Joined: true})
	}
	return out
}

func chatside001Names(chats []chatui.Conversation) string {
	var out []string
	for _, c := range chats {
		out = append(out, c.Name)
	}
	return strings.Join(out, ",")
}

// TestTodo_CHATSIDE_001_Place: a conversation that comes back to a section that
// sorts itself takes its sorted place; only a section the person ordered by hand
// takes it at the end.
func TestTodo_CHATSIDE_001_Place(t *testing.T) {
	onboarding := chatui.Conversation{ID: "onboarding", Name: "onboarding", Kind: chatui.PublicChannel, Joined: true}

	sorted := chatui.SidebarSection{ID: "channels", Chats: chatside001Rooms("general", "incident-review", "payroll-close", "sales")}
	chatside001Place(&sorted, onboarding)
	if got := chatside001Names(sorted.Chats); got != "general,incident-review,onboarding,payroll-close,sales" {
		t.Errorf("a sorted section: %s", got)
	}

	// Ordered by hand, flagged: at the end, even when the rooms happen to be in name order.
	flagged := chatui.SidebarSection{ID: "channels", Manual: true, Chats: chatside001Rooms("general", "incident-review", "payroll-close", "sales")}
	chatside001Place(&flagged, onboarding)
	if got := chatside001Names(flagged.Chats); got != "general,incident-review,payroll-close,sales,onboarding" {
		t.Errorf("a section ordered by hand: %s", got)
	}

	// Ordered by hand before the flag existed: the order is not alphabetical, so it is theirs.
	legacy := chatui.SidebarSection{ID: "channels", Chats: chatside001Rooms("sales", "general", "payroll-close")}
	chatside001Place(&legacy, onboarding)
	if got := chatside001Names(legacy.Chats); got != "sales,general,payroll-close,onboarding" {
		t.Errorf("a hand-ordered section without the flag: %s", got)
	}

	// An empty section, and a name that sorts first or last, in a sorted section.
	empty := chatui.SidebarSection{ID: "custom-1"}
	chatside001Place(&empty, onboarding)
	first := chatui.SidebarSection{ID: "channels", Chats: chatside001Rooms("payroll-close", "sales")}
	chatside001Place(&first, onboarding)
	last := chatui.SidebarSection{ID: "channels", Chats: chatside001Rooms("a-team", "b-team")}
	chatside001Place(&last, onboarding)
	if chatside001Names(empty.Chats) != "onboarding" || chatside001Names(first.Chats) != "onboarding,payroll-close,sales" || chatside001Names(last.Chats) != "a-team,b-team,onboarding" {
		t.Errorf("edges: %s | %s | %s", chatside001Names(empty.Chats), chatside001Names(first.Chats), chatside001Names(last.Chats))
	}
}

// A new section starts at the top, under Favorites, and every other section
// keeps the place the person gave it.
func TestTodo_CHATSIDE_001_NewSectionAtTop(t *testing.T) {
	sections := []chatui.SidebarSection{{ID: "custom-a"}, {ID: "channels"}, {ID: "direct"}}
	got := chatside001InsertSection(sections, chatui.SidebarSection{ID: "custom-new"})
	var ids []string
	for _, s := range got {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "custom-new,custom-a,channels,direct" {
		t.Errorf("order after a new section: %v", ids)
	}
	if len(sections) != 3 || sections[0].ID != "custom-a" {
		t.Errorf("the saved order was edited in place: %v", sections)
	}
}
