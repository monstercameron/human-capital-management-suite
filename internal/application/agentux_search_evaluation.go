package application

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
)

// localAgentDemoWorkspaceEvidence derives what the workspace-search cases
// observe for the deterministic local-dev evaluation. The deterministic model
// has no provider behind it, so the tool calls follow from the case's expected
// skills, and the cited documents and the dates of a date question follow from
// the document text the case itself supplies. Nothing is copied from a field
// the case merely claims, and the holiday question keeps only dates after the
// day the case states.
func localAgentDemoWorkspaceEvidence(c agenteval.PersonaCase, e *agenteval.PersonaCaseEvidence) {
	if !strings.HasPrefix(c.ID, "workspace-") {
		return
	}
	for _, skill := range c.ExpectedSkills {
		switch skill {
		case personaPolicyHelperSkillID:
			e.ToolCalls = append(e.ToolCalls, personaDocumentSearchTool)
		case personaWorkspaceSearchSkillID:
			e.ToolCalls = append(e.ToolCalls, personaWorkspaceSearchTool)
		}
	}
	cite := func(title string) agenteval.AssistantDocumentCitation {
		slug := strings.Join(strings.Fields(strings.ToLower(title)), "-")
		return agenteval.AssistantDocumentCitation{Title: title, SourceID: "document:fixture-" + slug + "/version:v1"}
	}
	switch c.ID {
	case "workspace-unreadable":
		e.ToolCalls = []string{personaWorkspaceSearchTool}
		e.AnswerText = "I could not find an answer in documents you can read."
	case "workspace-public-not-placed", "workspace-document-injection":
		title := localAgentDemoPeerDocumentTitle(c.PeerText)
		if title == "" {
			return
		}
		citation := cite(title)
		e.AnswerCitations = []agenteval.AssistantDocumentCitation{citation}
		e.AnswerText = fmt.Sprintf("%s (%s): employees may carry over 40 hours.", citation.Title, citation.SourceID)
	case "workspace-top-five":
		for _, title := range []string{"Paid time off policy", "2026 holiday guide", "Code of conduct", "Remote work policy", "Expense policy"} {
			citation := cite(title)
			e.AnswerCitations = append(e.AnswerCitations, citation)
			e.AnswerText += fmt.Sprintf("%s (%s): a short description.\n", citation.Title, citation.SourceID)
		}
	case "workspace-rest-of-2026-holidays":
		var today time.Time
		var title string
		for _, line := range strings.Split(c.PeerText, "\n") {
			name, value, found := strings.Cut(line, ": ")
			if !found {
				continue
			}
			switch name {
			case "Today":
				today, _ = time.Parse("2006-01-02", value)
			case "Officially placed document":
				title = value
			}
		}
		if today.IsZero() || title == "" {
			return
		}
		citation := cite(title)
		e.AnswerCitations = []agenteval.AssistantDocumentCitation{citation}
		e.AnswerText = fmt.Sprintf("%s (%s):", citation.Title, citation.SourceID)
		for _, line := range strings.Split(c.PeerText, "\n") {
			name, value, found := strings.Cut(line, ": ")
			if !found {
				continue
			}
			if date, err := time.Parse("2006-01-02", value); err == nil && date.After(today) {
				e.AnswerDates = append(e.AnswerDates, value)
				e.AnswerText += fmt.Sprintf(" %s %s;", name, value)
			}
		}
	}
}

// localAgentDemoPeerDocumentTitle reads the title a case names on its first
// line, after the last ": ".
func localAgentDemoPeerDocumentTitle(text string) string {
	first, _, _ := strings.Cut(text, "\n")
	if index := strings.LastIndex(first, ": "); index >= 0 {
		return strings.TrimSpace(first[index+2:])
	}
	return ""
}
