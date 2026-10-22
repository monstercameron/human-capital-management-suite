package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
)

// The receipt says which agents were prepared and what was placed, and a second
// run says it changed nothing.
func TestTodo_AGENTUX_049_StartersReceipt(t *testing.T) {
	summary := application.LocalAgentDemoSummary{
		Version: 6, State: "PUBLISHED", AssistantVersion: 4, AssistantState: "PUBLISHED",
		Starters: []application.LocalAgentDemoStarterReceipt{
			{Name: "Policy Helper", State: "PREPARED", Version: 6}, {Name: "Assistant", State: "PREPARED", Version: 4},
			{Name: "Birthday Buddy", State: "PREPARED", Version: 1}, {Name: "Task Catcher", State: "PREPARED", Version: 1},
			{Name: "Reminder", State: "PREPARED", Version: 1}, {Name: "Support Desk", State: "PREPARED", Version: 1},
		},
		Placements: application.LocalAgentDemoPlacements{AmbientInstallations: 2, SupportAlertMemberships: 1, GeneralHistoryPosts: 8, SupportEmailsSeeded: 5},
	}
	receipt := formatAgentDemoSummary(summary)
	for _, want := range []string{"Birthday Buddy v1: prepared.", "Task Catcher v1: prepared.", "Reminder v1: prepared.", "Support Desk v1: prepared.", "ambient_installations=2", "general_history_posts=8", "support_emails_seeded=5"} {
		if !strings.Contains(receipt, want) {
			t.Fatalf("receipt %q is missing %q", receipt, want)
		}
	}
	if strings.Contains(receipt, "not prepared") || strings.Contains(receipt, "Policy Helper v6: prepared.") {
		t.Fatalf("receipt %q", receipt)
	}
	summary.Placements = application.LocalAgentDemoPlacements{}
	if second := formatAgentDemoSummary(summary); !strings.Contains(second, "already prepared; no changes") {
		t.Fatalf("a run that changed nothing reads %q", second)
	}
}
