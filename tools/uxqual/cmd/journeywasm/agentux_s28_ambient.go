package main

import (
	"reflect"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
)

// applyAmbientSnapshot puts the whole ambient answer into the model: the agents
// that read here and the viewer's opt-out (applyAmbientReads), the installed
// agents the administrator may switch, and the offers shown under their
// messages. A failed read passes failed and keeps the last good offers, so the
// timeline shows a retry beside them instead of going blank (AGENTUX-066,
// -067, -068). It reports whether anything the page draws changed.
func applyAmbientSnapshot(model *chatui.Model, snapshot ambientagents.Snapshot, failed bool) bool {
	if model == nil {
		return false
	}
	changed := applyAmbientReads(model, snapshot)
	next := chatui.AmbientState{Manages: snapshot.Manages, Cards: snapshot.Cards, Failed: failed}
	for _, grant := range snapshot.Grants {
		if grant.Agent != "" {
			next.Grants = append(next.Grants, chatui.AmbientGrant{Agent: grant.Agent, Enabled: grant.Enabled, Paused: grant.Paused})
		}
	}
	if !reflect.DeepEqual(model.Ambient, next) {
		model.Ambient, changed = next, true
	}
	return changed
}

// clearAmbientState is what a conversation change does to the part of the
// answer applyAmbientSnapshot added.
func clearAmbientState(model *chatui.Model) {
	model.Ambient = chatui.AmbientState{}
}

// keepAmbientEditing carries the "editing this card" mark from the cards the
// page already shows onto the same cards of a fresh answer, when the card has
// not changed on the server. The mark is the page's own: the server never sends
// it, and a read that arrives while a person types must not close the editor.
func keepAmbientEditing(previous, fresh []chatui.AgentUXAmbientCard) []chatui.AgentUXAmbientCard {
	editing := map[string]uint64{}
	for _, card := range previous {
		if card.Editing {
			editing[card.ID] = card.Revision
		}
	}
	if len(editing) == 0 {
		return fresh
	}
	merged := append([]chatui.AgentUXAmbientCard(nil), fresh...)
	for i := range merged {
		if revision, ok := editing[merged[i].ID]; ok && revision == merged[i].Revision {
			merged[i].Editing = true
		}
	}
	return merged
}
