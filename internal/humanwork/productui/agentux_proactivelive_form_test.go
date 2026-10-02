package productui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	dom "golang.org/x/net/html"
)

func TestAgentUXProactiveLive_ConversationsAndForm_Browser(t *testing.T) {
	agents := []AgentAnnouncementAgent{
		{PersonaID: "assistant", Name: "Assistant", Conversations: []AgentAnnouncementConversation{{ID: "general", Name: "#general", InstallationID: "assistant-install"}, {ID: "general", Name: "#general", InstallationID: "duplicate"}, {ID: "team", Name: "#team", InstallationID: "team-install"}}},
		{PersonaID: "policy", Name: "Policy Helper", Conversations: []AgentAnnouncementConversation{{ID: "general", Name: "#general", InstallationID: "policy-install"}}},
	}
	for _, persona := range []string{"assistant", "policy", "unknown"} {
		choices := AgentAnnouncementConversations(agents, persona)
		want := map[string]int{"assistant": 2, "policy": 1, "unknown": 0}[persona]
		if len(choices) != want {
			t.Fatalf("%s choices=%+v", persona, choices)
		}
		if persona == "assistant" && choices[0].InstallationID != "assistant-install" {
			t.Fatal("duplicate replaced installation")
		}
		if persona == "policy" && choices[0].InstallationID != "policy-install" {
			t.Fatal("other agent's installation leaked")
		}
	}
	duplicateNames := []AgentAnnouncementAgent{{PersonaID: "assistant", InstallationID: "install", Conversations: []AgentAnnouncementConversation{{ID: "one", Name: "general"}, {ID: "two", Name: "general"}, {ID: "three", Name: "general · 2"}, {ID: "four", Name: "general"}}}}
	labels := map[string]bool{}
	for _, choice := range AgentAnnouncementConversations(duplicateNames, "assistant") {
		if labels[choice.Name] || choice.InstallationID != "install" {
			t.Fatalf("duplicate label or lost installation: %+v", choice)
		}
		labels[choice.Name] = true
	}
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		markup, err := ui.RenderToString(RenderAgentAnnouncements(locale, AgentAnnouncementsSnapshot{Available: true, CanCreate: true, Agents: agents, Rows: []AgentAnnouncementRow{{ID: "saved", AgentName: "Assistant", ConversationName: "#general", OwnerName: "Alex Example", LastRunAt: "2026-10-01T15:20:00Z", ResultCode: "FAILED", Reason: "The agent could not write this announcement."}}}))
		if err != nil {
			t.Fatal(err)
		}
		root, err := dom.Parse(strings.NewReader(markup))
		if err != nil {
			t.Fatal(err)
		}
		ids := map[string]*dom.Node{}
		errors := map[string]bool{}
		primary := 0
		var walk func(*dom.Node)
		walk = func(n *dom.Node) {
			id := proactiveFormAttr(n, "id")
			if id != "" {
				ids[id] = n
			}
			if key := proactiveFormAttr(n, "data-announcement-error"); key != "" {
				errors[key] = true
			}
			if n.Data == "button" && strings.Contains(proactiveFormAttr(n, "class"), "primary") && proactiveFormAttr(n, "data-announcement-action") != "" && !proactiveFormHasAttr(n, "hidden") {
				primary++
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(root)
		if primary != 1 {
			t.Fatalf("%s primary actions=%d", language, primary)
		}
		textarea := ids["announcement-instruction"]
		if textarea == nil || proactiveFormAttr(textarea, "rows") != "4" || proactiveFormHasAttr(textarea, "value") {
			t.Fatal("instruction is not a four-line editable control")
		}
		for _, field := range []string{"agent", "conversation", "instruction", "documents", "when", "time", "zone", "weekdays", "month-day"} {
			if !errors[field] || AgentAnnouncementText(locale, "error_"+field) == "error_"+field {
				t.Fatalf("%s missing localized inline error %s", language, field)
			}
		}
		for _, id := range []string{"announcement-agent", "announcement-conversation", "announcement-instruction", "announcement-time", "announcement-zone", "announcement-month-day"} {
			if !strings.Contains(proactiveFormAttr(ids[id], "aria-describedby"), "-error") {
				t.Fatalf("unassociated error: %s", id)
			}
		}
		conversations := ids["announcement-conversation"]
		count := 0
		for child := conversations.FirstChild; child != nil; child = child.NextSibling {
			if child.Data == "option" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("initial conversation select contains other agents: %d", count)
		}
		var find func(*dom.Node)
		find = func(n *dom.Node) {
			if n.Data == "option" && proactiveFormAttr(n, "value") == "assistant" {
				var values []AgentAnnouncementConversation
				if err := json.Unmarshal([]byte(proactiveFormAttr(n, "data-announcement-conversations")), &values); err != nil || len(values) != 2 {
					t.Fatalf("agent choices=%+v %v", values, err)
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				find(c)
			}
		}
		find(root)
		for _, text := range []string{AgentAnnouncementText(locale, "retry"), "Alex Example", "2026-10-01 15:20 UTC", AgentAnnouncementText(locale, "write_failed")} {
			if !strings.Contains(markup, text) {
				t.Fatalf("%s missing outcome %q", language, text)
			}
		}
	}
}

func TestAgentUXProactiveLive_PostedRowAndDirectTab_Browser(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		markup, err := ui.RenderToString(RenderAgentAnnouncements(locale, AgentAnnouncementsSnapshot{Available: true, Rows: []AgentAnnouncementRow{{AgentName: "Assistant", ConversationName: "#general", OwnerName: "Alex Example", ResultCode: "POSTED", LastRun: "October 1, 2026, 11:20 AM", MessageHref: "/workspace/app/chat?message=announcement"}}}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, "October 1, 2026, 11:20 AM") || !strings.Contains(markup, "Alex Example") || !strings.Contains(markup, `href="/workspace/app/chat?message=announcement"`) {
			t.Fatal("posted row lost time, owner or message")
		}
		for range 2 {
			view := ApplyLocale(NewView(PageAgentOperations, "tenant", "owner", ""), locale)
			view.Query = "tab=announcements"
			view.AgentsProjection = &AgentsAvailabilityProjection{ViewerIsAdmin: true}
			rendered, err := ui.RenderToString(BuildAgentOperationsPage(view))
			if err != nil {
				t.Fatal(err)
			}
			root, err := dom.Parse(strings.NewReader(rendered))
			if err != nil {
				t.Fatal(err)
			}
			var panel *dom.Node
			var walk func(*dom.Node)
			walk = func(n *dom.Node) {
				if proactiveFormAttr(n, "id") == "agent-announcements" {
					panel = n
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c)
				}
			}
			walk(root)
			if panel == nil || proactiveFormHasAttr(panel, "hidden") {
				t.Fatal("direct/reloaded URL hides Announcements")
			}
		}
	}
}
func proactiveFormAttr(n *dom.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func proactiveFormHasAttr(n *dom.Node, key string) bool {
	if n == nil {
		return false
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}
