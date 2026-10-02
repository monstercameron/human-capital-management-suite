package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

func chatbug084Model() Model {
	m := chatux002Model("en-US")
	m.ChatFeatures = &ChatFeatures{Status: true}
	m.ChangeChannelStatus = func(chat.ChangeChannelStatusRequest) {}
	m.Conversations = append(m.Conversations, Conversation{ID: "old", Name: "old-project", Kind: PublicChannel, Joined: true})
	m.ChannelStatuses = map[string]ChannelStatusView{
		"old": {Status: chat.ChannelStatus{ConversationID: "old", Name: "old-project", Status: chatpolicy.StatusArchived}, Transitions: []chat.StatusTransition{{Status: chatpolicy.StatusOpen}}},
	}
	return m
}

// chatbug084Rows is the names of the sidebar rows that sit in the sections,
// not in the Archived list at the foot of the rail.
func chatbug084Rows(t *testing.T, m Model) (inSections, inArchived string) {
	t.Helper()
	markup := renderNode(t, rail(m, handlers{}))
	foot := strings.Index(markup, "chatstate-archived")
	if foot < 0 {
		t.Fatalf("no Archived list in the rail: %s", markup)
	}
	return markup[:foot], markup[foot:]
}

// TestTodo_CHATBUG_084: an archived channel is listed only under Archived,
// except while it is the open conversation.
func TestTodo_CHATBUG_084(t *testing.T) {
	m := chatbug084Model()
	sections, archived := chatbug084Rows(t, m)
	if strings.Contains(sections, "old-project") {
		t.Fatalf("the archived channel is still under Channels: %s", sections)
	}
	if !strings.Contains(archived, "old-project") || !strings.Contains(sections, "people-ops") {
		t.Fatalf("Archived lost it or Channels lost an open one: %s", archived)
	}
	// Channels counts what it lists.
	if !strings.Contains(sections, `class="section-count">2<`) && !strings.Contains(sections, ">2</span>") {
		t.Errorf("Channels count does not match its rows: %s", sections)
	}
	// While it is open, the row stays where the person is reading.
	m.SelectedID = "old"
	sections, _ = chatbug084Rows(t, m)
	if !strings.Contains(sections, "old-project") {
		t.Fatalf("the open archived channel vanished from the list: %s", sections)
	}
}

// TestTodo_CHATBUG_082_Restore: the archive notice carries a Restore button for
// people who may restore, and the status history is panel-style rows.
func TestTodo_CHATBUG_082_Restore(t *testing.T) {
	m := chatbug084Model()
	view := m.ChannelStatuses["old"]
	notice := renderNode(t, ChannelStatusComposerNotice(m, view))
	if !strings.Contains(notice, "This channel is archived") || !strings.Contains(notice, `data-action="channel-restore"`) || !strings.Contains(notice, ">Restore<") {
		t.Fatalf("notice without a Restore button: %s", notice)
	}
	// Without the transition, or the right to send it, the notice is only text.
	noRight := view
	noRight.Transitions = nil
	if got := renderNode(t, ChannelStatusComposerNotice(m, noRight)); strings.Contains(got, "channel-restore") || !strings.Contains(got, "workspace administrator") {
		t.Fatalf("a person who may not restore is offered it: %s", got)
	}
	m.ChangeChannelStatus = nil
	if got := renderNode(t, ChannelStatusComposerNotice(m, view)); strings.Contains(got, "channel-restore") {
		t.Fatalf("restore offered without a way to send it: %s", got)
	}

	// History: rows in the panel's style, not a definition list in large type.
	history := renderNode(t, chatstateHistory(m, ChannelStatusView{Status: chat.ChannelStatus{Reason: "No longer needed", ChangedBy: "system"}}))
	if strings.Count(history, `class="chatstate-history-row"`) < 2 || !strings.Contains(history, "No longer needed") {
		t.Fatalf("history rows: %s", history)
	}
	if !strings.Contains(ChatDetailsStyles, ".details-about .chatstate-history-row{display:flex") {
		t.Error("the About history has no row style")
	}
}

// TestTodo_CHATBUG_084_SavedLayout: a saved layout that lists the archived
// channel (in a section of the person's own, in Channels after it came back from
// a deleted section, or in Favorites) never draws it outside Archived, whatever
// the layout says, unless it is the open conversation; and the layout is not
// refused for omitting it.
func TestTodo_CHATBUG_084_SavedLayout(t *testing.T) {
	old := Conversation{ID: "old", Name: "old-project", Kind: PublicChannel, Joined: true}
	for name, arrange := range map[string]func(m *Model){
		"a section of their own": func(m *Model) {
			m.Sections = []SidebarSection{{ID: "custom-1", Name: "Projects", Chats: []Conversation{old}}, {ID: "channels", Name: "Channels", Chats: m.Conversations[:2]}, {ID: "direct", Name: "Direct messages"}}
		},
		"back in Channels at the end": func(m *Model) {
			m.Sections = []SidebarSection{{ID: "channels", Name: "Channels", Chats: append(append([]Conversation(nil), m.Conversations[:2]...), old)}, {ID: "direct", Name: "Direct messages"}}
		},
		"starred": func(m *Model) {
			for i := range m.Conversations {
				if m.Conversations[i].ID == "old" {
					m.Conversations[i].Starred = true
				}
			}
			m.Sections = []SidebarSection{{ID: "channels", Name: "Channels", Chats: append(append([]Conversation(nil), m.Conversations[:2]...), old)}, {ID: "direct", Name: "Direct messages"}}
		},
	} {
		m := chatbug084Model()
		arrange(&m)
		sections, archived := chatbug084Rows(t, m)
		if strings.Contains(sections, "old-project") {
			t.Errorf("%s: the archived channel is drawn in a section: %s", name, sections)
		}
		if !strings.Contains(archived, "old-project") {
			t.Errorf("%s: Archived lost it", name)
		}
		m.SelectedID = "old"
		if open, _ := chatbug084Rows(t, m); !strings.Contains(open, "old-project") {
			t.Errorf("%s: the open archived channel vanished", name)
		}
	}
}
