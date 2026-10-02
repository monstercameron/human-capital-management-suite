package chatsearch

// CHATSEARCH-003: the agent pages and their records are found from the same
// search box. They are not Chat content, so they are not in Declarations();
// the composition that has the agent pages registers them with these
// declarations, and a deployment without agents never offers the kinds.
const (
	Agent             Kind = "agent"
	AgentTask         Kind = "agent_task"
	AgentAnnouncement Kind = "agent_announcement"
)

// AgentDeclarations is a fresh value, like Declarations.
func AgentDeclarations() []Declaration {
	return []Declaration{
		{Agent, "name and purpose; never instructions", "people who may use the agent now", "the agent on the Agents page"},
		{AgentTask, "task goal", "the task's owner only", "the task on the Agents page"},
		{AgentAnnouncement, "announcement instruction", "the announcement's owner only", "the announcement on the agent operations page"},
	}
}

// AgentDeclaration is the declaration of one agent kind.
func AgentDeclaration(kind Kind) (Declaration, bool) {
	for _, d := range AgentDeclarations() {
		if d.Kind == kind {
			return d, true
		}
	}
	return Declaration{}, false
}
