package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
)

func TestAgentUXAmbient_PreparationReceipt_Golden(t *testing.T) {
	summary := application.LocalAgentDemoSummary{AmbientAgents: []application.AgentUXAmbientPreparationReceipt{{Agent: "task-catcher", State: "PUBLISHED", Version: 1, PrivateOffers: 1}, {Agent: "reminder", State: "PUBLISHED", Version: 1, PublicOffers: 1}}}
	receipt := formatAgentDemoSummary(summary)
	for _, want := range []string{"Task Catcher v1: PUBLISHED; private offers=1 public offers=0.", "Reminder v1: PUBLISHED; private offers=0 public offers=1."} {
		if !strings.Contains(receipt, want) {
			t.Fatalf("receipt missing %q: %s", want, receipt)
		}
	}
	if strings.Contains(formatAgentDemoSummary(application.LocalAgentDemoSummary{}), "Task Catcher") {
		t.Fatal("unprepared agent claimed in receipt")
	}
}
