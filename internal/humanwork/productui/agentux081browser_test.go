package productui

import (
	"strings"
	"testing"
)

// TestTodo_AGENTUX_081_Browser re-checks the composed Agents surfaces in every
// locale: Activity states each agent's real state in words, a failed Cost or
// Connections read ends in the shared placeholder with Try again, and the Ask
// choices carry exactly the agents the kind filter let through. The live
// behaviors (opening the agent profile, Edit focus, retry clicks) are
// specified in tools/uxqual/browser/agentux081_agent_surfaces.spec.mjs, which
// needs a running server and browser and is not executed here.
func TestTodo_AGENTUX_081_Browser(t *testing.T) {
	states := map[string]map[string]string{
		"en-US": {"answering": "Answering", "ready": "Ready", "unplaced": "Not placed", "paused": "Paused", "retry": "Try again"},
		"de-DE": {"answering": "Antwortet", "ready": "Bereit", "unplaced": "Nicht platziert", "paused": "Pausiert", "retry": "Erneut versuchen"},
		"ar":    {"answering": "يجيب", "ready": "جاهز", "unplaced": "غير موضوع", "paused": "متوقف مؤقتًا", "retry": "حاول مجدداً"},
	}
	for localeID, words := range states {
		t.Run(localeID, func(t *testing.T) {
			locale := ResolveProductLocale(localeID)
			placed := []PersonaAdminInstallation{{InstallationID: "i1", ConversationID: "general", Conversation: "general", Kind: "channel"}}
			activity := agentUXR7Render(t, RenderAgentControls(locale, AgentControlsSnapshot{Available: true, AllowedCommands: []string{"SUSPEND", "PUBLISH"}, Agents: []PersonaAdminPersona{
				{ID: "working", Name: "Working", Version: "1", Lifecycle: PersonaPublished, Installations: placed, RecentRuns: []AgentControlRun{{ID: "r1", State: "running"}}},
				{ID: "quiet", Name: "Quiet", Version: "1", Lifecycle: PersonaPublished, Installations: placed},
				{ID: "nowhere", Name: "Nowhere", Version: "1", Lifecycle: PersonaPublished},
				{ID: "paused", Name: "Paused", Version: "1", Lifecycle: PersonaSuspended, Installations: placed},
			}}, "done"))
			for id, word := range map[string]string{"working": words["answering"], "quiet": words["ready"], "nowhere": words["unplaced"], "paused": words["paused"]} {
				row := activity[strings.Index(activity, `data-persona-id="`+id+`"`):]
				if next := strings.Index(row[len(id)+18:], "data-persona-id="); next >= 0 {
					row = row[:len(id)+18+next]
				}
				if !strings.Contains(row, ">"+word+"<") {
					t.Fatalf("%s: agent %q does not state %q: %s", localeID, id, word, row)
				}
			}
			cost := agentUXR7Render(t, AgentCostPanel(AgentCostPanelProps{I18nProps: I18nProps{Locale: locale}, State: AgentAccessStateUnavailable}))
			if !strings.Contains(cost, `data-agent-cost-action="retry"`) || !strings.Contains(cost, ">"+words["retry"]+"<") {
				t.Fatalf("%s: failed Cost read lost its retry: %s", localeID, cost)
			}
			admin := agentUXR7Render(t, AgentAdminAccessPage(AgentAdminAccessPageProps{I18nProps: I18nProps{Locale: locale}, State: AgentAccessStateUnavailable, Embedded: true}))
			if !strings.Contains(admin, `data-agent-admin-action="retry"`) || !strings.Contains(admin, ">"+words["retry"]+"<") {
				t.Fatalf("%s: failed Connections read lost its retry: %s", localeID, admin)
			}
			ask := agentUXR7Render(t, tagAgentComposer(locale, AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true,
				Agents: []AgentSummary{{ID: "policy-helper", Name: "Policy Helper", Description: "Answers policy questions."}}}))
			if !strings.Contains(ask, "Policy Helper") || strings.Contains(ask, "Birthday Buddy") {
				t.Fatalf("%s: Ask choices are wrong: %s", localeID, ask)
			}
		})
	}
}
