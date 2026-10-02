package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
)

// applyAmbientReads puts what the server says about "Reads every message here"
// into the model: the agents the administrator let read this conversation, and
// the viewer's own "Don't act on my messages". It touches nothing else, so a
// read that failed leaves the last good values in place (AGENTUX-066).
func applyAmbientReads(model *chatui.Model, snapshot ambientagents.Snapshot) bool {
	if model == nil {
		return false
	}
	reads := make([]chatui.AmbientRead, 0, len(snapshot.Grants))
	for _, grant := range snapshot.Grants {
		if grant.Enabled && grant.Agent != "" {
			reads = append(reads, chatui.AmbientRead{Agent: grant.Agent, Paused: grant.Paused})
		}
	}
	changed := model.AmbientOptOut != snapshot.OptOut || len(model.AmbientReads) != len(reads)
	if !changed {
		for i := range reads {
			changed = changed || reads[i] != model.AmbientReads[i]
		}
	}
	model.AmbientReads, model.AmbientOptOut = reads, snapshot.OptOut
	return changed
}

// clearAmbientReads is what a conversation change does: the next conversation
// shows nothing until its own answer arrives.
func clearAmbientReads(model *chatui.Model) {
	model.AmbientReads, model.AmbientOptOut = nil, false
	model.Callbacks.SetAmbientOptOut = nil
	clearAmbientState(model)
}
