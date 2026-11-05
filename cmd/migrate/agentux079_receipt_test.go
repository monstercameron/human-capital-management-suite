package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
)

// TestAgentUX079_SupportProjectReceipt states the Customer support project's
// line in the preparation receipt: prepared, already prepared, or not
// prepared with the plain reason. A run that never reached the step says
// nothing about it.
func TestAgentUX079_SupportProjectReceipt(t *testing.T) {
	base := application.LocalAgentDemoSummary{DisplayName: "Policy Helper", Version: 4, State: "PUBLISHED", AssistantVersion: 1, AssistantState: "PUBLISHED"}
	if line := formatAgentDemoSummary(base); strings.Contains(line, "Customer support project") {
		t.Fatalf("zero receipt says %q, want no support line", line)
	}
	prepared := base
	prepared.SupportProject = application.AgentUX079SupportProjectReceipt{Checked: true, Created: true, GrantRecorded: true}
	if line := formatAgentDemoSummary(prepared); !strings.Contains(line, "Customer support project: prepared.") {
		t.Fatalf("created receipt says %q", line)
	}
	idle := base
	idle.SupportProject = application.AgentUX079SupportProjectReceipt{Checked: true}
	if line := formatAgentDemoSummary(idle); !strings.Contains(line, "Customer support project: already prepared.") {
		t.Fatalf("idle receipt says %q", line)
	}
	missing := base
	missing.SupportProject = application.AgentUX079SupportProjectReceipt{Checked: true, Reason: application.AgentUX079SupportProjectReasonUnconfigured}
	line := formatAgentDemoSummary(missing)
	if !strings.Contains(line, "Customer support project: not prepared (the project database is not configured).") {
		t.Fatalf("unconfigured receipt says %q", line)
	}
	if strings.Contains(line, "hcmnext.local") || strings.Contains(line, "AGENTUX") {
		t.Fatalf("receipt exposes internal identifiers: %q", line)
	}
}

// TestAgentUX079_BirthdayScheduleReceipt states the Birthday Buddy schedule's
// line in the preparation receipt: filed, active, or already filed. A run
// that never reached the step says nothing about it.
func TestAgentUX079_BirthdayScheduleReceipt(t *testing.T) {
	base := application.LocalAgentDemoSummary{DisplayName: "Policy Helper", Version: 4, State: "PUBLISHED", AssistantVersion: 1, AssistantState: "PUBLISHED"}
	if line := formatAgentDemoSummary(base); strings.Contains(line, "Birthday schedule") {
		t.Fatalf("zero receipt says %q, want no birthday line", line)
	}
	active := base
	active.BirthdaySchedule = application.AgentUX079BirthdayScheduleReceipt{Checked: true, Created: true, Resumed: true}
	if line := formatAgentDemoSummary(active); !strings.Contains(line, "Birthday schedule: filed and active.") {
		t.Fatalf("created receipt says %q", line)
	}
	paused := base
	paused.BirthdaySchedule = application.AgentUX079BirthdayScheduleReceipt{Checked: true, Created: true}
	if line := formatAgentDemoSummary(paused); !strings.Contains(line, "Birthday schedule: filed, paused until its model route exists.") {
		t.Fatalf("paused receipt says %q", line)
	}
	resumed := base
	resumed.BirthdaySchedule = application.AgentUX079BirthdayScheduleReceipt{Checked: true, Resumed: true}
	if line := formatAgentDemoSummary(resumed); !strings.Contains(line, "Birthday schedule: active.") {
		t.Fatalf("resumed receipt says %q", line)
	}
	idle := base
	idle.BirthdaySchedule = application.AgentUX079BirthdayScheduleReceipt{Checked: true}
	if line := formatAgentDemoSummary(idle); !strings.Contains(line, "Birthday schedule: already filed.") {
		t.Fatalf("idle receipt says %q", line)
	}
}
