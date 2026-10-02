package main

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"

// agentControlsPersonaSource picks where Activity takes its list of agents
// from: the Agent setup snapshot this session already holds, else the one the
// page was served with. The server sends that snapshot only with the Agent
// setup page, so a person who opens Agent operations directly holds neither;
// needsRead then says the snapshot has to be read before the rows can be drawn.
// Activity used to draw no agent rows at all in that case, while its hint still
// said an agent could be paused "above".
func agentControlsPersonaSource(held, served *productui.PersonaAdminSnapshot) (source *productui.PersonaAdminSnapshot, needsRead bool) {
	if held != nil && held.Available {
		return held, false
	}
	if served != nil && served.Available {
		return served, false
	}
	return nil, true
}

// agentControlsPageSnapshot joins the two projections Activity is drawn from:
// the owner's runs and schedules, and the agents with what the viewer may do to
// them. When the agents could not be read the page is told so, which is not the
// same as a workspace with no agents.
func agentControlsPageSnapshot(controls productui.AgentControlsSnapshot, personas *productui.PersonaAdminSnapshot, readFailed bool) productui.AgentControlsSnapshot {
	if personas != nil {
		controls.Agents = append([]productui.PersonaAdminPersona(nil), personas.Personas...)
		controls.AllowedCommands = append([]string(nil), personas.AllowedCommands...)
		controls.AgentsUnavailable = false
		return controls
	}
	controls.AgentsUnavailable = readFailed && len(controls.Agents) == 0
	return controls
}
