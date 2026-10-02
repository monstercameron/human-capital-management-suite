package chatui

import (
	"strings"
	"testing"
)

func TestTodo_CHAT_032_ManualOrder(t *testing.T) {
	rooms := []Conversation{{ID: "first", Name: "First"}, {ID: "second", Name: "Second"}, {ID: "third", Name: "Third"}}
	var movedID string
	var movedDelta int
	m := Model{
		State:         StateReady,
		Conversations: rooms,
		Sections:      []SidebarSection{{ID: "channels", Name: "Channels", Chats: rooms}},
		RailMenuID:    "second",
		Callbacks: Callbacks{
			OpenRailMenu: func(string) {},
			MoveConversationOrder: func(id string, delta int) {
				movedID, movedDelta = id, delta
			},
		},
	}

	markup := render(t, m)
	for _, want := range []string{
		`data-action="rail-chat-up" data-id="second"`,
		`data-action="rail-chat-down" data-id="second"`,
		"Move conversation up",
		"Move conversation down",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("manual order menu missing %q", want)
		}
	}
	button := func(markup, action string) string {
		marker := `data-action="` + action + `"`
		index := strings.Index(markup, marker)
		if index < 0 {
			return ""
		}
		start := strings.LastIndex(markup[:index], "<button")
		end := strings.Index(markup[index:], ">")
		if start < 0 || end < 0 {
			return ""
		}
		return markup[start : index+end+1]
	}
	if strings.Contains(button(markup, "rail-chat-up"), " disabled") || strings.Contains(button(markup, "rail-chat-down"), " disabled") {
		t.Fatal("middle conversation should be movable in both directions")
	}
	// CHATUX-020: a move with nowhere to go is not offered at all.
	m.RailMenuID = "first"
	if first := render(t, m); button(first, "rail-chat-up") != "" || button(first, "rail-chat-down") == "" {
		t.Fatal("first conversation must not offer a move above its section, only below")
	}
	m.RailMenuID = "third"
	if last := render(t, m); button(last, "rail-chat-down") != "" || button(last, "rail-chat-up") == "" {
		t.Fatal("last conversation must not offer a move below its section, only above")
	}

	m.actWith("rail-chat-up", "second", "")
	if movedID != "second" || movedDelta != -1 {
		t.Fatalf("move up callback = %q, %d", movedID, movedDelta)
	}
	m.actWith("rail-chat-down", "second", "")
	if movedID != "second" || movedDelta != 1 {
		t.Fatalf("move down callback = %q, %d", movedID, movedDelta)
	}
}
