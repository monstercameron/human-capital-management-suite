package chatui

// Issue 3821: the Browse button of a gated channel says "Answer N questions to
// join", so it opens the gate form (the page's gate hook reads data-gate-open)
// and puts Browse away. It no longer starts the plain "Join #channel?"
// confirmation, which only delayed the form and stayed in the page behind it.

// actionGateJoin is the Browse action of a gated row.
const actionGateJoin = "gate-join"

// issue3821JoinData returns data with the gate-join action and the gate-open
// marker when the channel is joined through a gate; otherwise data unchanged.
func issue3821JoinData(m Model, c Conversation, data map[string]string) map[string]string {
	if _, gated := chatgateJoin(m, c); !gated || c.Joined || (data["action"] != "join" && data["action"] != actionGateJoin) {
		return data
	}
	data["action"] = actionGateJoin
	data["gate-open"] = c.ID
	return data
}
