package main

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
)

// formatAgentDemoChatTaskModels is the agent-demo receipt sentence for the
// three model-backed Chat approval documents: each file and whether this run
// wrote it, or the plain reason they were not derived.
func formatAgentDemoChatTaskModels(summary application.LocalAgentDemoSummary) string {
	if len(summary.ChatTaskModels) == 0 {
		if summary.ChatTaskModelsNote == "" {
			return ""
		}
		return " Chat model deployments (translation, writing style, ambient screen) " + summary.ChatTaskModelsNote + "."
	}
	parts := make([]string, 0, len(summary.ChatTaskModels))
	for _, doc := range summary.ChatTaskModels {
		state := "already current"
		if doc.Written {
			state = "written"
		}
		parts = append(parts, fmt.Sprintf("%s %s (%s)", doc.Task, doc.Path, state))
	}
	return " Chat model deployments (evaluation stated by you; nothing was measured here): " + strings.Join(parts, "; ") + "."
}
