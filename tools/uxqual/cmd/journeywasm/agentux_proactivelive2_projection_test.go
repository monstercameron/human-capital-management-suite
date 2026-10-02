//go:build !(js && wasm)

package main

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"google.golang.org/protobuf/types/known/timestamppb"
	"strings"
	"testing"
	"time"
)

func TestAgentUXProactiveLive_PostedMessage_Browser(t *testing.T) {
	now := time.Date(2026, 10, 1, 20, 30, 0, 0, time.UTC)
	body, err := chatui.AnnouncementMessageBody(chatui.AgentAnnouncementMessage{AgentName: "Assistant", OwnerName: "Walt Brennan", Text: "Company holidays still to come:\n- Thanksgiving — Nov 26\n- Christmas — Dec 25", PostedAt: now, Sources: []chatui.AgentAnnouncementSource{{Title: "2026 holiday guide", Href: "/workspace/app/docs?document=holidays"}}})
	if err != nil {
		t.Fatal(err)
	}
	// This is the unmodified browser projection used for initial pages, streams,
	// catch-up, and threads. No receipt or PersonaActor is fabricated by the test.
	post := &chatv1.Post{Id: "posted", AuthorId: "assistant", Body: body, CreatedAt: timestamppb.New(now), Sequence: 7, Revision: 1}
	projected := chatMessage(post, "en-US", map[string]string{"assistant": "Hcmnext Local Persona Assistant"}, now)
	if projected.Body != body || projected.PersonaActor != nil {
		t.Fatalf("Chat changed body bytes: %+v", projected)
	}
	icon := agenticon.Generate(agenticon.Input{Name: "Assistant", Instructions: "Upcoming holiday announcements"})
	model := chatui.Model{Locale: "en-US", State: chatui.StateReady, SelectedID: "general", Conversations: []chatui.Conversation{{ID: "general", Name: "general"}}, Messages: []chatui.Message{projected}, ResolvedPersonaMentions: []chatui.ResolvedPersonaMention{{Reference: chatui.ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant-a", ID: "assistant", Display: "Assistant"}, Icon: icon, IconRevision: 2}}}
	markup, err := ui.RenderToString(chatui.Build(model))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"agent-icon", "agent-badge", "Assistant", "Posted for Walt Brennan", "2026 holiday guide", "<li>"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("normal client projection missing %s: %s", want, markup)
		}
	}
	for _, bad := range []string{"hcm_agent_announcement", "Hcmnext Local Persona Assistant", "Linked document"} {
		if strings.Contains(markup, bad) {
			t.Fatalf("normal client projection leaked %s: %s", bad, markup)
		}
	}
	if strings.Count(markup, `href="/workspace/app/docs?document=holidays"`) != 1 {
		t.Fatal("Chat added a duplicate document preview")
	}
}
