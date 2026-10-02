package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AGENTUX-026: the row under a question says, in the agent's own name, that it
// is working on this. The line under it says what it is doing now
// (AGENTUX-075) and, after ten seconds, for how long.

// agentUX026WorkingHead is the head of a working row: the agent's icon, name
// and badge, then "is working on this" in the reader's language. The words
// follow the name so the row reads as one sentence; they are not the live
// region, which is the line below and is announced once per change of step.
func agentUX026WorkingHead(model Model, agent string) ui.Node {
	return html.Div(html.Props{Class: "agent-reply-working-head"},
		renderAgentReplyIdentity(model, agent),
		html.Span(html.Props{Class: "agent-reply-working", Text: personaProgressText(model, "chat.agent.working", "is working on this")}))
}

const agentUX026WorkingStyles = `.agent-reply-working-head{display:flex;flex-wrap:wrap;align-items:center;gap:2px 8px;min-width:0}.agent-reply-working-head .agent-reply-identity{flex:0 1 auto}.agent-reply-working{color:var(--hcm-color-text-muted);font-size:.8125rem;overflow-wrap:anywhere}`
