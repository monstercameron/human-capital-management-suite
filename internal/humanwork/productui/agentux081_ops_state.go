package productui

// agentUX081ActivityState states what an agent is doing in words an owner can
// act on: only a published agent placed in a conversation and working right
// now is Answering. A quiet placed agent is Ready, a published agent in no
// conversation is Not placed, and a suspended agent stays Paused.
func agentUX081ActivityState(locale LocaleContext, persona PersonaAdminPersona, runs []AgentControlRun) (string, string) {
	if persona.Lifecycle == PersonaSuspended {
		return agentUX073Text(locale, "state_paused"), "warning"
	}
	if len(persona.Installations) == 0 {
		return agentUX073Text(locale, "state_unplaced"), "warning"
	}
	for _, run := range persona.RecentRuns {
		if !agentOpsRunFinished(run.State) {
			return agentUX073Text(locale, "state_answering"), "success"
		}
	}
	for _, run := range runs {
		if run.AgentID != "" && run.AgentID != persona.ID {
			continue
		}
		if !agentOpsRunFinished(run.State) {
			return agentUX073Text(locale, "state_answering"), "success"
		}
	}
	return agentUX073Text(locale, "state_ready"), "success"
}
