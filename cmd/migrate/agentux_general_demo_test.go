package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"strings"
	"testing"
)

func TestAgentUXGeneral_CommandReceipt(t *testing.T) {
	summary := application.LocalAgentDemoSummary{Version: 6, State: "PUBLISHED", AssistantVersion: 1, AssistantState: "PUBLISHED", HolidayDocumentPlacements: 1}
	first := formatAgentDemoSummary(summary)
	for _, want := range []string{"Policy Helper v6: PUBLISHED", "Assistant v1: PUBLISHED", "holiday_guide_placements=1", "Documents: Paid time off policy; 2026 holiday guide."} {
		if !strings.Contains(first, want) {
			t.Fatalf("receipt %q missing %q", first, want)
		}
	}
	summary.HolidayDocumentPlacements = 0
	second := formatAgentDemoSummary(summary)
	if !strings.Contains(second, "already prepared; no changes") || !strings.Contains(second, "2026 holiday guide") {
		t.Fatalf("replay receipt %q", second)
	}
	t.Log(first)
	t.Log(second)
}
