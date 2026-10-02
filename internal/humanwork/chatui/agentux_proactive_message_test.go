package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestAgentUXProactive_RenderedPublicMessage(t *testing.T) {
	for _, tc := range []struct{ locale string }{{"en-US"}, {"de-DE"}, {"ar"}} {
		markup, err := ui.RenderToString(RenderAgentAnnouncementMessage(Model{Locale: tc.locale, ResolvedPersonaMentions: []ResolvedPersonaMention{{Reference: ChatReference{Kind: "AGENT_MENTION", ID: "policy-helper", Display: "Policy Helper"}, Initials: "PH"}}}, AgentAnnouncementMessage{AgentName: "Policy Helper", OwnerName: "Walt Brennan", Text: "Thanksgiving is the next company holiday.", Scheduled: true, PostedAt: time.Now(), Sources: []AgentAnnouncementSource{{Title: "2026 holiday guide", Href: "/workspace/app/docs?document=holiday-guide"}}}))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Policy Helper", "agent-badge", "Thanksgiving is the next company holiday.", agentReplyFallback(tc.locale, "chat.agent.sources", "Sources"), "2026 holiday guide", `href="/workspace/app/docs?document=holiday-guide"`, `data-agent-announcement="true"`} {
			if !strings.Contains(markup, want) {
				t.Errorf("%s public message missing %q: %s", tc.locale, want, markup)
			}
		}
		if strings.Contains(markup, "Only visible to you") || strings.Contains(markup, "Nur für Sie sichtbar") || strings.Contains(markup, "مرئي لك فقط") {
			t.Fatalf("%s public announcement retained a private marker: %s", tc.locale, markup)
		}
	}
}

func TestAgentUXProactive_OneTimeMessageSafeSources(t *testing.T) {
	markup, err := ui.RenderToString(RenderAgentAnnouncementMessage(Model{Locale: "en-US"}, AgentAnnouncementMessage{AgentName: "Policy Helper", OwnerName: "Alex Example", Text: "The next holiday is Thanksgiving.", Sources: []AgentAnnouncementSource{{Title: "Holiday guide", Href: "javascript:alert(1)"}, {Title: "Staff policy", Href: "https://untrusted.invalid/private"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "javascript:") || strings.Contains(markup, "untrusted.invalid") {
		t.Fatalf("one-time attribution or source link escaped the boundary: %s", markup)
	}
}

func TestAgentUXProactive_NormalMessageProjection(t *testing.T) {
	body, err := AnnouncementMessageBody(AgentAnnouncementMessage{AgentName: "Agent", OwnerName: "Alex Example", Text: "Thanksgiving is the next company holiday.", Sources: []AgentAnnouncementSource{{Title: "2026 holiday guide", Href: "/workspace/app/docs?document=holiday-guide"}}})
	if err != nil {
		t.Fatal(err)
	}
	markup := render(t, Model{Locale: "en-US", State: StateReady, SelectedID: "general", Conversations: []Conversation{{ID: "general", Name: "General"}}, Messages: []Message{{ID: "announcement", AuthorID: "policy-helper", Author: "Policy Helper", Body: body, PersonaActor: &PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", Trusted: true}}}})
	for _, want := range []string{"agent-dm-avatar", "agent-badge", "Thanksgiving is the next company holiday.", "2026 holiday guide", `data-agent-announcement="true"`, `data-message-id="announcement"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("normal public message missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "agent-reply-identity") || strings.Contains(markup, "Only visible to you") || strings.Contains(markup, AgentAnnouncementBodyPrefix) || strings.Count(markup, `href="/workspace/app/docs?document=holiday-guide"`) != 1 {
		t.Fatalf("normal message duplicated identity or sources, or leaked its envelope: %s", markup)
	}
}
