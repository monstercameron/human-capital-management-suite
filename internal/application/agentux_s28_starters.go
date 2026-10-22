package application

import "strings"

// localAgentDemoStarterEntry is one starter the demo workspace is meant to
// offer. A starter with a seed (Policy Helper, Assistant) or a spec (the agents
// added after them) is prepared by the command (reviewed, evaluated, published
// and placed, each step idempotent); a starter with neither is listed with the
// reason it is not, so the command's receipt says what it left out instead of
// leaving the omission to be found on the page (AGENTUX-049).
type localAgentDemoStarterEntry struct {
	Name   string
	seed   *localAgentDemoSeed
	spec   func() (localAgentDemoStarterSpec, error)
	Reason string
}

// prepared reports whether the command prepares this starter.
func (e localAgentDemoStarterEntry) prepared() bool { return e.seed != nil || e.spec != nil }

// LocalAgentDemoStarterReceipt is the command's line for one starter.
type LocalAgentDemoStarterReceipt struct {
	Name    string
	State   string
	Reason  string
	Version int64
}

const (
	localAgentDemoStarterPrepared    = "PREPARED"
	localAgentDemoStarterNotPrepared = "NOT_PREPARED"
)

// localAgentDemoStarterList is the one list the preparation iterates. The
// order is the order they are prepared in: Policy Helper first, because the
// others copy its audience, owner and steward.
func localAgentDemoStarterList() []localAgentDemoStarterEntry {
	return []localAgentDemoStarterEntry{
		{Name: "Policy Helper", seed: &localAgentDemoSeed{personaID: localAgentDemoPersonaID, starterID: "hcmnext.persona_template.policy_helper"}},
		{Name: "Assistant", seed: &localAgentDemoSeed{personaID: localAgentDemoAssistantPersonaID, starterID: localAgentDemoAssistantStarterID}},
		{Name: "Birthday Buddy", spec: localAgentDemoBirthdaySpec},
		{Name: "Task Catcher", spec: func() (localAgentDemoStarterSpec, error) { return localAgentDemoAmbientSpec("task-catcher") }},
		{Name: "Reminder", spec: func() (localAgentDemoStarterSpec, error) { return localAgentDemoAmbientSpec("reminder") }},
		{Name: "Support Desk", spec: localAgentDemoSupportSpec},
	}
}

// localAgentDemoStarterNames lists the starters the preparation prepares.
func localAgentDemoStarterNames() []string {
	var names []string
	for _, entry := range localAgentDemoStarterList() {
		if entry.prepared() {
			names = append(names, entry.Name)
		}
	}
	return names
}

// localAgentDemoStarterReceipts is the receipt for a run that prepared the
// starters named in prepared; versions maps a starter's name to the version it
// stands at.
func localAgentDemoStarterReceipts(prepared []string, versions ...map[string]int64) []LocalAgentDemoStarterReceipt {
	var receipts []LocalAgentDemoStarterReceipt
	for _, entry := range localAgentDemoStarterList() {
		receipt := LocalAgentDemoStarterReceipt{Name: entry.Name, State: localAgentDemoStarterNotPrepared, Reason: strings.TrimSpace(entry.Reason)}
		for _, name := range prepared {
			if name == entry.Name {
				receipt.State, receipt.Reason = localAgentDemoStarterPrepared, ""
			}
		}
		for _, byName := range versions {
			receipt.Version = byName[entry.Name]
		}
		receipts = append(receipts, receipt)
	}
	return receipts
}
