package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
)

// The preparation command iterates one list of starters, in order, and says in
// its receipt which of them it did not prepare and why (AGENTUX-049). Policy
// Helper comes first, and every seeded entry names a starter the one template
// mechanism (the persona starters) actually offers.
func TestTodo_AGENTUX_049_StarterList(t *testing.T) {
	list := localAgentDemoStarterList()
	var names []string
	for _, entry := range list {
		names = append(names, entry.Name)
	}
	if got := strings.Join(names, ", "); got != "Policy Helper, Assistant, Birthday Buddy, Task Catcher, Reminder, Support Desk" {
		t.Fatalf("the starter list is %s", got)
	}
	for _, entry := range list {
		switch {
		case entry.seed != nil && entry.Reason != "":
			t.Fatalf("%s is both prepared and excused", entry.Name)
		case entry.seed == nil && entry.Reason == "":
			t.Fatalf("%s is left out without a reason", entry.Name)
		case entry.seed != nil:
			if starter, ok := agenttemplate.PersonaStarterFor(entry.seed.starterID, 1); !ok || starter.DisplayName != entry.Name {
				t.Fatalf("%s is seeded from %q, which the persona starters do not offer under that name", entry.Name, entry.seed.starterID)
			}
		}
	}
	prepared := localAgentDemoStarterNames()
	if len(prepared) != 2 || prepared[0] != "Policy Helper" || prepared[1] != "Assistant" {
		t.Fatalf("prepared starters = %v", prepared)
	}
	receipts := localAgentDemoStarterReceipts(prepared)
	if len(receipts) != 6 || receipts[0].State != localAgentDemoStarterPrepared || receipts[2].State != localAgentDemoStarterNotPrepared || receipts[2].Reason == "" {
		t.Fatalf("receipts = %+v", receipts)
	}
}
