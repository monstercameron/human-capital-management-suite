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
	m.RailMenuID = "first"
	if !strings.Contains(button(render(t, m), "rail-chat-up"), " disabled") {
		t.Fatal("first conversation must not move above its section")
	}
	m.RailMenuID = "third"
	if !strings.Contains(button(render(t, m), "rail-chat-down"), " disabled") {
		t.Fatal("last conversation must not move below its section")
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
