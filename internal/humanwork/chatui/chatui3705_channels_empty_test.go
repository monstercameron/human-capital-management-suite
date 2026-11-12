package chatui

import (
	"strings"
	"testing"
)

// TestTodo_3705_ChannelsEmptyHint: a person in no channel sees a hint under the
// Channels heading with a Browse channels control, not an empty section.
func TestTodo_3705_ChannelsEmptyHint(t *testing.T) {
	m := chatux020Model()
	m.Locale = "en-US"
	m.Callbacks.OpenBrowse = func() {}
	m.Conversations = []Conversation{{ID: "dm-loretta", Name: "Loretta Haynes", Kind: DirectMessage, Joined: true}}
	m.Sections = []SidebarSection{{ID: "channels", Name: "Channels"}, {ID: "direct", Name: "Direct messages", Chats: m.Conversations}}
	markup := renderNode(t, railSection(m, SidebarSection{ID: "channels", Name: "Channels"}, nil))
	if !strings.Contains(markup, `class="rail-empty"`) || !strings.Contains(markup, "You are not in any channel yet.") {
		t.Errorf("the empty Channels section has no hint under its heading: %s", markup)
	}
	if !strings.Contains(markup, `data-action="open-browse"`) || !strings.Contains(markup, ">Browse channels<") {
		t.Errorf("the empty Channels section has no Browse channels control: %s", markup)
	}
	// A section with channels keeps its rows and no hint.
	general := Conversation{ID: "general", Name: "general", Kind: PublicChannel, Joined: true}
	filled := renderNode(t, railSection(m, SidebarSection{ID: "channels", Name: "Channels", Chats: []Conversation{general}}, nil))
	if strings.Contains(filled, "You are not in any channel yet.") {
		t.Errorf("a Channels section with a channel still shows the empty hint: %s", filled)
	}
}
