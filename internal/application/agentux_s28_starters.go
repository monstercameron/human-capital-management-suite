package application

import "strings"

// localAgentDemoStarterEntry is one starter the demo workspace is meant to
// offer. A starter with a seed is prepared by the command (reviewed, evaluated,
// published and placed, each step idempotent); a starter without one is listed
// with the reason it is not, so the command's receipt says what it left out
// instead of leaving the omission to be found on the page (AGENTUX-049).
type localAgentDemoStarterEntry struct {
	Name   string
	seed   *localAgentDemoSeed
	Reason string
}

// LocalAgentDemoStarterReceipt is the command's line for one starter.
type LocalAgentDemoStarterReceipt struct {
	Name   string
	State  string
	Reason string
}

const (
	localAgentDemoStarterPrepared    = "PREPARED"
	localAgentDemoStarterNotPrepared = "NOT_PREPARED"
)

// localAgentDemoStarterList is the one list the preparation iterates. The
// order is the order they are prepared in: Policy Helper first, because the
// others copy its audience, owner and steward.
func localAgentDemoStarterList() []localAgentDemoStarterEntry {
	const notYet = "needs its evaluation suite and manifest registered with the local policy source, and its runtime composed, before it can be reviewed and published"
	return []localAgentDemoStarterEntry{
		{Name: "Policy Helper", seed: &localAgentDemoSeed{personaID: localAgentDemoPersonaID, starterID: "hcmnext.persona_template.policy_helper"}},
		{Name: "Assistant", seed: &localAgentDemoSeed{personaID: localAgentDemoAssistantPersonaID, starterID: localAgentDemoAssistantStarterID}},
		{Name: "Birthday Buddy", Reason: "its suite and the birthday source are libraries only: " + notYet},
		{Name: "Task Catcher", Reason: "reads messages through a run planner that does not exist yet, and: " + notYet},
		{Name: "Reminder", Reason: "reads messages through a run planner that does not exist yet, and: " + notYet},
		{Name: "Support Desk", Reason: "its inbox, object store, run queue, planner and alert delivery are libraries only: " + notYet},
	}
}

// localAgentDemoStarterNames lists the starters the preparation prepares.
func localAgentDemoStarterNames() []string {
	var names []string
	for _, entry := range localAgentDemoStarterList() {
		if entry.seed != nil {
			names = append(names, entry.Name)
		}
	}
	return names
}

// localAgentDemoStarterReceipts is the receipt for a run that prepared the
// starters named in prepared.
func localAgentDemoStarterReceipts(prepared []string) []LocalAgentDemoStarterReceipt {
	var receipts []LocalAgentDemoStarterReceipt
	for _, entry := range localAgentDemoStarterList() {
		receipt := LocalAgentDemoStarterReceipt{Name: entry.Name, State: localAgentDemoStarterNotPrepared, Reason: strings.TrimSpace(entry.Reason)}
		for _, name := range prepared {
			if name == entry.Name {
				receipt.State, receipt.Reason = localAgentDemoStarterPrepared, ""
			}
		}
		receipts = append(receipts, receipt)
	}
	return receipts
}
