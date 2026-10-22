package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
)

// The preparation command iterates one list of starters, in order, and says in
// its receipt which of them it did not prepare and why (AGENTUX-049). Policy
// Helper comes first, and every seeded entry names a starter the one template
// mechanism (the persona starters) actually offers. The agents added after
// Assistant are built from their own specs and every one of them is prepared.
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
		case entry.prepared() && entry.Reason != "":
			t.Fatalf("%s is both prepared and excused", entry.Name)
		case !entry.prepared() && entry.Reason == "":
			t.Fatalf("%s is left out without a reason", entry.Name)
		case entry.seed != nil:
			if starter, ok := agenttemplate.PersonaStarterFor(entry.seed.starterID, 1); !ok || starter.DisplayName != entry.Name {
				t.Fatalf("%s is seeded from %q, which the persona starters do not offer under that name", entry.Name, entry.seed.starterID)
			}
		case entry.spec != nil:
			spec, err := entry.spec()
			if err != nil || spec.starter.DisplayName != entry.Name || spec.plan.PersonaID == "" || spec.plan.Instructions == "" || spec.manifest == nil {
				t.Fatalf("%s has no usable spec: %v", entry.Name, err)
			}
			if entry.Name == "Support Desk" != spec.unplaced || entry.Name != "Support Desk" && len(spec.rooms) != 1 {
				t.Fatalf("%s is placed in %v (unplaced=%t)", entry.Name, spec.rooms, spec.unplaced)
			}
		}
	}
	prepared := localAgentDemoStarterNames()
	if len(prepared) != 6 || prepared[0] != "Policy Helper" || prepared[1] != "Assistant" {
		t.Fatalf("prepared starters = %v", prepared)
	}
	receipts := localAgentDemoStarterReceipts(prepared[:2], map[string]int64{"Assistant": 3})
	if len(receipts) != 6 || receipts[0].State != localAgentDemoStarterPrepared || receipts[1].Version != 3 || receipts[2].State != localAgentDemoStarterNotPrepared {
		t.Fatalf("receipts = %+v", receipts)
	}
}
