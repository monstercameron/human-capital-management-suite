package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// chatbugMarkedText is the locale lookup the served page hands the model: a key
// with no entry comes back wrapped, not empty and not equal to the key.
func chatbugMarkedText(key string) string {
	if strings.HasPrefix(key, "chat.status.") {
		return "⟦" + key + "⟧"
	}
	return ""
}

func chatbugStatusModel(locale string, status chatpolicy.ChannelStatus) Model {
	room := Conversation{ID: "general", Name: "general", Kind: PublicChannel, Joined: true}
	view := ChannelStatusView{Status: chat.ChannelStatus{TenantID: "tenant", ConversationID: "general", Status: status}}
	return Model{State: StateReady, Locale: locale, Text: chatbugMarkedText, SelectedID: "general", Conversations: []Conversation{room},
		ChannelStatuses: map[string]ChannelStatusView{"general": view}}
}

func TestTodo_CHATBUG_001(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, status := range []chatpolicy.ChannelStatus{chatpolicy.StatusOpen, chatpolicy.StatusAnnouncements, chatpolicy.StatusLocked, chatpolicy.StatusArchived} {
			m := chatbugStatusModel(locale, status)
			key := chatstateKey(status)
			got := chatstateText(m, key)
			if strings.Contains(got, "⟦") || got == "" {
				t.Fatalf("%s %s: status text = %q", locale, key, got)
			}
			chip := ChannelStatusChip(m, m.ChannelStatuses["general"])
			if status == chatpolicy.StatusOpen {
				if chip != nil {
					t.Fatalf("%s: an Open channel carries a status chip", locale)
				}
				continue
			}
			markup := chatstateRender(t, chip)
			if !strings.Contains(markup, got) || !strings.Contains(markup, `aria-hidden="true"`) || strings.Contains(markup, "⟦") {
				t.Fatalf("%s %s: chip = %s", locale, key, markup)
			}
		}
	}
	for _, want := range []string{".chatstate-label", "@container chatmain (max-width:560px)", "max-width:560px"} {
		if !strings.Contains(ChannelStatusStyles, want) {
			t.Fatalf("styles do not give the channel name priority over the label: missing %q", want)
		}
	}
}

func TestTodo_CHATBUG_001_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		open := render(t, chatbugStatusModel(locale, chatpolicy.StatusOpen))
		if strings.Contains(open, "⟦") || strings.Contains(open, `class="chatstate-badge"`) {
			t.Fatalf("%s: an Open channel shows a status label or a copy key", locale)
		}
		for _, status := range []chatpolicy.ChannelStatus{chatpolicy.StatusAnnouncements, chatpolicy.StatusLocked, chatpolicy.StatusArchived} {
			m := chatbugStatusModel(locale, status)
			markup := render(t, m)
			word := chatstateText(m, chatstateKey(status))
			if strings.Contains(markup, "⟦") {
				t.Fatalf("%s %v: a copy key is rendered", locale, status)
			}
			// One chip in the sidebar row and one in the header.
			if strings.Count(markup, `class="chatstate-badge"`) != 2 || strings.Count(markup, ">"+word+"<") < 2 {
				t.Fatalf("%s %v: want the word %q and the icon in the header and the sidebar: %s", locale, status, word, markup)
			}
		}
	}
}

func TestTodo_CHATBUG_011(t *testing.T) {
	id := "673214ec-4402-5f09-9a5c-000000000001"
	m := Model{State: StateReady}
	for _, c := range []Conversation{
		{ID: id, Name: id, Kind: DirectMessage},
		{ID: id, Name: "", Kind: DirectMessage},
		{ID: id, Name: id, Kind: DirectMessage, Agent: true, AgentID: "agent-1"},
		{ID: id, Name: "agent-1", Kind: DirectMessage, Agent: true, AgentID: "agent-1"},
	} {
		if got := displayName(m, c); got != "" {
			t.Fatalf("%+v: unknown name shown as %q", c, got)
		}
	}
	known := Conversation{ID: id, Name: "Planner Agent", Kind: DirectMessage, Agent: true, AgentID: "agent-1"}
	if got := displayName(m, known); got != "Planner Agent" {
		t.Fatalf("agent conversation name = %q, want the record's own name", got)
	}
}

func TestTodo_CHATBUG_011_Browser(t *testing.T) {
	id := "673214ec-4402-5f09-9a5c-000000000001"
	for _, c := range []Conversation{{ID: id, Name: id, Kind: DirectMessage, Joined: true}, {ID: id, Name: id, Kind: DirectMessage, Joined: true, Agent: true, AgentID: "agent-1"}} {
		m := Model{State: StateReady, SelectedID: id, Conversations: []Conversation{c}}
		markup := render(t, m)
		if strings.Contains(markup, ">673214ec") || strings.Contains(markup, ">6<") || strings.Contains(markup, `title="673214ec`) {
			t.Fatalf("an identifier or its initial is rendered: %s", markup)
		}
		if !strings.Contains(markup, `title="Conversation"`) {
			t.Fatalf("the neutral placeholder row is missing: %s", markup)
		}
	}
	named := Model{State: StateReady, SelectedID: id, Conversations: []Conversation{{ID: id, Name: "Planner Agent", Kind: DirectMessage, Joined: true, Agent: true, AgentID: "agent-1"}}}
	if markup := render(t, named); !strings.Contains(markup, "Planner Agent") {
		t.Fatal("the agent's own name is not shown")
	}
}
