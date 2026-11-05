package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// agentUX074GeneralSeed is the id the general agent's icon is derived from. It
// has no stored icon and no persona id, so the Ask choice and the tasks it
// answered share this seed.
const agentUX074GeneralSeed = "agents-choice-general"

// agentUX074IconValue is the one answer to "which icon does this agent wear"
// on the Agents page, Agent setup and Agent operations. It is the function
// Chat ends in (agenticon.ValueFor): the agent's stored icon when it has one,
// else the fallback taken from its id among the agents listed with it. Chat
// passes the ids of the agents its model knows; these pages pass the ids of
// the agents they list, so one agent has one picture wherever it appears.
func agentUX074IconValue(stored agenticon.Value, id string, peers []string) agenticon.Value {
	return agenticon.ValueFor(stored, strings.TrimSpace(id), peers)
}

func agentUX074Icon(stored agenticon.Value, id string, peers []string) ui.Node {
	return agentUX074IconImage(stored, "", id, peers)
}

// agentUX074IconImage draws the agent's own picture on the Agents page, Agent
// setup and Agent operations: its uploaded image when one is stored, over its
// generated icon so a failed image still leaves the agent recognisable.
func agentUX074IconImage(stored agenticon.Value, imageURL, id string, peers []string) ui.Node {
	value := agentUX074IconValue(stored, id, peers)
	if url := strings.TrimSpace(imageURL); url != "" {
		return html.Span(html.Props{Class: "agent-icon agent-icon-photo", Aria: map[string]string{"hidden": "true"}}, agenticon.Node(value),
			html.Img(html.Props{Class: "agent-icon-photo-img", Src: url, Loading: "lazy", Raw: map[string]any{"alt": "", "decoding": "async"}}))
	}
	return agenticon.Node(value)
}

func agentUX074AgentIDs(agents []AgentSummary) []string {
	ids := make([]string, 0, len(agents))
	for _, agent := range agents {
		if id := strings.TrimSpace(agent.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// agentUX074TaskIcon is the icon of the agent that answered a task: the icon
// that agent wears in the Ask choices above the list. A task answered by an
// agent the viewer can no longer use keeps the icon the task itself carries.
func agentUX074TaskIcon(agents []AgentSummary, task AgentTask) agenticon.Value {
	id := strings.TrimSpace(task.AnsweringAgentID)
	if id == "" || strings.EqualFold(id, "general-agent") {
		return agentUX074IconValue(agenticon.Value{}, agentUX074GeneralSeed, nil)
	}
	peers := agentUX074AgentIDs(agents)
	for _, agent := range agents {
		if strings.TrimSpace(agent.ID) == id {
			return agentUX074IconValue(agent.Icon, id, peers)
		}
	}
	// A task recorded under another id for the same agent is matched by the
	// agent's name, so it still wears the picture of the choice above it.
	if name := strings.TrimSpace(task.AnsweringAgentDisplayName); name != "" {
		for _, agent := range agents {
			if strings.EqualFold(strings.TrimSpace(agent.Name), name) {
				return agentUX074IconValue(agent.Icon, agent.ID, peers)
			}
		}
	}
	return agentUX074IconValue(task.Icon, id, peers)
}
