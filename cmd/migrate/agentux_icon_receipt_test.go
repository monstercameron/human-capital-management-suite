package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
)

func TestAgentUXIcon_Generated(t *testing.T) {
	for _, tc := range []struct {
		count int
		want  string
	}{{3, "Agent icons set: 3."}, {0, "Agent icons set: 0."}} {
		summary := application.LocalAgentDemoSummary{Version: 1, State: "PUBLISHED", AssistantVersion: 1, AssistantState: "PUBLISHED", IconsSet: tc.count}
		if summary.Changed() != (tc.count > 0) {
			t.Fatal("icon backfill not reflected in changed receipt", summary)
		}
		if receipt := formatAgentDemoSummary(summary); !strings.Contains(receipt, tc.want) {
			t.Fatal("backfill count missing", receipt)
		}
	}
}
