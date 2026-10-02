package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// The ambient snapshot reaches the model as the agents that read here and the
// viewer's own opt-out; an agent without a grant is not named; the same answer
// again changes nothing; and a conversation change clears it.
func TestTodo_AGENTUX_066_ReadsState(t *testing.T) {
	model := chatui.Model{State: chatui.StateReady, SelectedID: "general", Locale: "en-US", Conversations: []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true}}}
	snapshot := ambientagents.Snapshot{Grants: []ambientagents.Grant{{Agent: "task-catcher", Enabled: true}, {Agent: "reminder", Enabled: true, Paused: true}, {Agent: "idle", Enabled: false}}, OptOut: true}
	if !applyAmbientReads(&model, snapshot) {
		t.Fatal("a first answer reported no change")
	}
	if len(model.AmbientReads) != 2 || model.AmbientReads[0].Agent != "task-catcher" || !model.AmbientReads[1].Paused || !model.AmbientOptOut {
		t.Fatalf("model reads %+v optout %v", model.AmbientReads, model.AmbientOptOut)
	}
	if applyAmbientReads(&model, snapshot) {
		t.Fatal("the same answer reported a change")
	}
	model.Callbacks.SetAmbientOptOut = func(bool) {}
	model.ShowDetails = true
	page, err := ui.RenderToString(chatui.Build(model))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Task Catcher and Reminder read messages here", "Reminder: paused, daily limit reached", "ambient-optout"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the page lacks %q", want)
		}
	}
	if strings.Contains(page, "Idle") {
		t.Fatal("an agent without a grant is named as reading here")
	}
	clearAmbientReads(&model)
	if len(model.AmbientReads) != 0 || model.AmbientOptOut || model.Callbacks.SetAmbientOptOut != nil {
		t.Fatalf("a conversation change left %+v", model)
	}
}

// CHATBUG-088: a stored announcement is drawn from its author's identity, never
// from the envelope's own AgentName. Before the room's agent directory arrives
// the row has a neutral mark and an empty icon; after it, the agent's name and
// stored icon, whatever the envelope says.
func TestTodo_CHATBUG_088_DirectoryArrival(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "alice"}
	stored, _ := json.Marshal(chatui.AgentAnnouncementMessage{AgentName: "Agent", OwnerName: "Walt Brennan", Text: "Upcoming company holidays remaining in 2026:\n- Labor Day"})
	message := chatui.Message{ID: "announced", AuthorID: "hcmnext.local.persona.assistant", Author: "Hcmnext Local Persona Assistant", Body: chatui.AgentAnnouncementBodyPrefix + string(stored), SentAt: time.Date(2026, 10, 1, 20, 30, 0, 0, time.UTC)}
	model := chatui.Model{State: chatui.StateReady, Locale: "en-US", SelectedID: "general", CurrentTenantID: "tenant", CurrentUser: "alice", Messages: []chatui.Message{message},
		Conversations: []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true}}}
	authorLine := func(m chatui.Model) string {
		page, err := ui.RenderToString(chatui.Build(m))
		if err != nil {
			t.Fatal(err)
		}
		at := strings.Index(page, `message-author`)
		if at < 0 {
			t.Fatalf("no author line in %s", page)
		}
		rest := page[at:]
		return rest[strings.Index(rest, ">")+1 : strings.Index(rest, "</strong>")]
	}
	if got := authorLine(model); got != "…" {
		t.Fatalf("before the directory the author line is %q", got)
	}
	payload := personaChatDirectory{Personas: []personaChatProfile{}, PostActors: []personaChatPostActor{{PostID: "announced", PersonaID: "hcmnext.local.persona.assistant", AgentID: "assistant", PersonaVersion: "2", Display: "Assistant"}}}
	applyPersonaDirectoryResult(&model, payload, cfg, "general", nil, nil)
	if got := authorLine(model); got != "Assistant" {
		t.Fatalf("after the directory the author line is %q", got)
	}
	// A directory that says nothing of the post ends the wait with the generic
	// label, not the name inside the envelope and not another agent's.
	empty := chatui.Model{State: chatui.StateReady, Locale: "en-US", SelectedID: "general", CurrentTenantID: "tenant", CurrentUser: "alice", Messages: []chatui.Message{message}, Conversations: model.Conversations}
	applyPersonaDirectoryResult(&empty, personaChatDirectory{}, cfg, "general", nil, nil)
	if got := authorLine(empty); got != "Agent" {
		t.Fatalf("after an empty directory the author line is %q", got)
	}
}
