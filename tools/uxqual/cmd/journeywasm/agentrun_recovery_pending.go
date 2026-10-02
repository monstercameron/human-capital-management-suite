package main

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// agentPendingWindow is how long a question's waiting card may stand on the
// client's word alone. The server admits a mention within a minute and the
// invocation stream replaces this card with the server's own row almost at
// once; a card still unconfirmed after this has no run behind it (the server
// restarted between committing the post and admitting it) and stops waiting.
const agentPendingWindow = 90 * time.Second

// agentPendingProgress is the provisional waiting state shown for a mention the
// server has committed but not yet reported on. It carries its own deadline so
// that it can never count seconds for ever.
func agentPendingProgress(user, agent string) *chatui.PersonaProgressProps {
	return &chatui.PersonaProgressProps{InvokerID: user, AgentName: agent, Visible: true, Deadline: time.Now().Add(agentPendingWindow), Provisional: true}
}

// agentProgressExpired is whether a waiting card has passed its deadline. Such a
// card is drawn as an interrupted answer, and no longer counts.
func agentProgressExpired(progress *chatui.PersonaProgressProps, now time.Time) bool {
	return progress != nil && !progress.Deadline.IsZero() && !now.Before(progress.Deadline)
}
