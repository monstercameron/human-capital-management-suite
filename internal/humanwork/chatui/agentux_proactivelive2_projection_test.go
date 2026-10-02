package chatui

import (
	"encoding/json"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"strings"
	"testing"
	"time"
)

func TestAgentUXProactiveLive_PostedMessage_Browser(t *testing.T) {
	message := AgentAnnouncementMessage{AgentName: "Assistant", OwnerName: "Walt Brennan", Text: "The remaining holidays are:\n- Thanksgiving — Nov 26\n- Day after Thanksgiving — Nov 27\n- Christmas — Dec 25", PostedAt: time.Date(2026, 10, 1, 20, 30, 0, 0, time.UTC), Sources: []AgentAnnouncementSource{{Title: "2026 holiday guide", Href: "/workspace/app/docs?document=holidays"}}}
	encoded, err := AnnouncementMessageBody(message)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded, "/workspace/app/docs") || strings.Contains(encoded, "doc:holidays") {
		t.Fatal("wire envelope exposes references to Chat scanners")
	}
	decoded, ok := DecodeAnnouncementMessageBody(encoded)
	if !ok || decoded.Text != message.Text || !decoded.PostedAt.Equal(message.PostedAt) {
		t.Fatalf("wire changed visible bytes %+v", decoded)
	}
	legacy, _ := json.Marshal(message)
	icon := agenticon.Generate(agenticon.Input{Name: "Assistant", Instructions: "Announce upcoming holidays"})
	for _, wire := range []string{encoded, AgentAnnouncementBodyPrefix + string(legacy)} {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			markup := render(t, Model{Locale: locale, State: StateReady, SelectedID: "general", Conversations: []Conversation{{ID: "general", Name: "general"}}, Messages: []Message{{ID: "posted", AuthorID: "assistant", Author: "Hcmnext Local Persona Assistant", Body: wire}}, ResolvedPersonaMentions: []ResolvedPersonaMention{{Reference: ChatReference{Kind: "AGENT_MENTION", ID: "assistant", Display: "Assistant"}, Icon: icon, IconRevision: 4, Version: "2"}}})
			for _, want := range []string{"agent-icon", "agent-badge", "Assistant", "Thanksgiving", "<li>", "Walt Brennan", "2026 holiday guide"} {
				if !strings.Contains(markup, want) {
					t.Fatalf("%s lacks %s: %s", locale, want, markup)
				}
			}
			attribution := strings.ReplaceAll(agentAnnouncementChatText(locale, "once"), "{owner}", "Walt Brennan")
			if !strings.Contains(markup, attribution) {
				t.Fatalf("missing localized attribution: %s", markup)
			}
			for _, bad := range []string{"Hcmnext Local Persona Assistant", "hcm_agent_announcement", "Linked document", "0001-01-01"} {
				if strings.Contains(markup, bad) {
					t.Fatalf("%s contains raw output %s: %s", locale, bad, markup)
				}
			}
			if strings.Count(markup, `href="/workspace/app/docs?document=holidays"`) != 1 {
				t.Fatalf("source link duplicated: %s", markup)
			}
		}
	}
	human := agentAnnouncementProjectedIdentity(Model{Members: []Member{{ID: "walt", Name: "Walt Brennan"}}, ResolvedPersonaMentions: []ResolvedPersonaMention{{Reference: ChatReference{Kind: "AGENT_MENTION", ID: "assistant", Display: "Assistant"}}}}, Message{AuthorID: "walt", Author: "Walt Brennan", Body: encoded})
	if human.PersonaActor != nil || human.Author != "Walt Brennan" {
		t.Fatal("human forged agent identity from body")
	}
	for _, bad := range []string{"plain message", AgentAnnouncementBodyPrefix + "v2:!", AgentAnnouncementBodyPrefix + `{"Text":"spoof"}`} {
		if _, ok := DecodeAnnouncementMessageBody(bad); ok {
			t.Fatalf("malformed wire accepted %q", bad)
		}
	}
}
