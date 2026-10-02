package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
)

// The whole answer reaches the model: the offers, the installed agents the
// administrator may switch (on or off), and whether the viewer administers the
// room; the same answer again changes nothing; a failed read keeps the offers
// and marks the failure; a conversation change clears all of it (AGENTUX-066,
// -067, -068).
func TestTodo_AGENTUX_067_AmbientSnapshotState(t *testing.T) {
	model := chatui.Model{SelectedID: "general"}
	snapshot := ambientagents.Snapshot{
		Manages: true,
		Grants:  []ambientagents.Grant{{Agent: "reminder", Enabled: true}, {Agent: "task-catcher", Enabled: false}},
		Cards:   []chatui.AgentUXAmbientCard{{ID: "one", Source: "post", Title: "Send the deck", Revision: 2}},
	}
	if !applyAmbientSnapshot(&model, snapshot, false) {
		t.Fatal("a first answer reported no change")
	}
	if !model.Ambient.Manages || len(model.Ambient.Grants) != 2 || model.Ambient.Grants[1].Enabled || len(model.Ambient.Cards) != 1 || len(model.AmbientReads) != 1 {
		t.Fatalf("model %+v reads %+v", model.Ambient, model.AmbientReads)
	}
	if applyAmbientSnapshot(&model, snapshot, false) {
		t.Fatal("the same answer reported a change")
	}
	if !applyAmbientSnapshot(&model, snapshot, true) || !model.Ambient.Failed || len(model.Ambient.Cards) != 1 {
		t.Fatalf("a failed read lost the offers or the mark: %+v", model.Ambient)
	}
	clearAmbientReads(&model)
	if model.Ambient.Manages || len(model.Ambient.Cards) != 0 || len(model.Ambient.Grants) != 0 {
		t.Fatalf("a conversation change left %+v", model.Ambient)
	}
}

// A card a person has open for editing stays open when a fresh answer arrives
// for the same revision, and closes when the card changed on the server.
func TestTodo_AGENTUX_067_AmbientKeepsEditing(t *testing.T) {
	previous := []chatui.AgentUXAmbientCard{{ID: "a", Revision: 1, Editing: true}, {ID: "b", Revision: 1, Editing: true}, {ID: "c", Revision: 1}}
	fresh := []chatui.AgentUXAmbientCard{{ID: "a", Revision: 1}, {ID: "b", Revision: 2}, {ID: "c", Revision: 1}}
	merged := keepAmbientEditing(previous, fresh)
	if !merged[0].Editing || merged[1].Editing || merged[2].Editing {
		t.Fatalf("merged %+v", merged)
	}
	if fresh[0].Editing {
		t.Fatal("the fresh answer was modified in place")
	}
}
