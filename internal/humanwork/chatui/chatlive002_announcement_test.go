package chatui

import (
	"encoding/json"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"strings"
	"testing"
	"time"
)

// TestTodo_CHATLIVE_002_Browser renders the announcement exactly as the review
// cell stored it: the original unencoded envelope, authored under the agent's
// persona id, in a room where the agent is not in the mention catalog. The
// server attests the author through the room's post actors; the page must show
// the agent's message and never the tag or the data.
func TestTodo_CHATLIVE_002_Browser(t *testing.T) {
	stored, _ := json.Marshal(AgentAnnouncementMessage{AgentName: "Agent", OwnerName: "Walt Brennan", Text: "Upcoming company holidays remaining in 2026:\n- Labor Day — Sep 7\n- Thanksgiving Day — Nov 26", Sources: []AgentAnnouncementSource{{Title: "2026 holiday guide", Href: "/workspace/app/docs?document=doc-10c773e5"}}})
	body := AgentAnnouncementBodyPrefix + string(stored)
	sent := time.Date(2026, 10, 1, 20, 30, 26, 0, time.UTC)
	message := Message{ID: "announced", AuthorID: "hcmnext.local.persona.assistant", Author: "Hcmnext Local Persona Assistant", Body: body, SentAt: sent}
	model := Model{Locale: "en-US", State: StateReady, SelectedID: "general", Conversations: []Conversation{{ID: "general", Name: "general"}}, Messages: []Message{message},
		PersonaPostActors: map[string]PersonaPostActor{"announced": {Display: "Assistant", Actor: PersonaActor{PersonaID: "hcmnext.local.persona.assistant", AgentID: "assistant", PersonaVersion: "2", Trusted: true, Icon: agenticon.Generate(agenticon.Input{Name: "Assistant", Instructions: "Announce"}), IconRevision: 3}}}}
	markup := render(t, model)
	for _, want := range []string{"agent-badge", "Assistant", "Labor Day", "<li>", "Thanksgiving Day", "2026 holiday guide", `href="/workspace/app/docs?document=doc-10c773e5"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("announcement lacks %q: %s", want, markup)
		}
	}
	for _, bad := range []string{"hcm_agent_announcement", "AgentName", "OwnerName", "Hcmnext Local Persona Assistant", "0001-01-01", "&#34;Text&#34;", "{&#34;"} {
		if strings.Contains(markup, bad) {
			t.Fatalf("announcement prints %q: %s", bad, markup)
		}
	}

	// First paint, or a directory that never answers: the author is not a
	// person the model knows, so the body is still drawn as an announcement,
	// under a neutral agent label, never as the tag and the data.
	model.PersonaPostActors = nil
	first := render(t, model)
	for _, want := range []string{"Labor Day", "<li>", "2026 holiday guide", "data-agent-announcement"} {
		if !strings.Contains(first, want) {
			t.Fatalf("first paint lacks %q: %s", want, first)
		}
	}
	for _, bad := range []string{"hcm_agent_announcement", "AgentName", "Hcmnext Local Persona Assistant", "0001-01-01", "{&#34;"} {
		if strings.Contains(first, bad) {
			t.Fatalf("first paint prints %q: %s", bad, first)
		}
	}

	// A person who types the same body is never drawn as an announcement.
	for name, person := range map[string]Model{
		"member":    {Members: []Member{{ID: "hcmnext.local.persona.assistant", Name: "Walt Brennan"}}},
		"directory": {SearchDirectory: []SearchPerson{{ID: "hcmnext.local.persona.assistant", Name: "Walt Brennan"}}},
		"reader":    {CurrentUser: "hcmnext.local.persona.assistant"},
	} {
		forged := model
		forged.Members, forged.SearchDirectory, forged.CurrentUser = person.Members, person.SearchDirectory, person.CurrentUser
		markup := render(t, forged)
		if strings.Contains(markup, "data-agent-announcement") {
			t.Fatalf("%s: a person's post rendered as an announcement: %s", name, markup)
		}
	}
}
