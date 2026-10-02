package productui

import (
	"regexp"
	"strings"
	"testing"
)

var (
	agentUX018Persona   = regexp.MustCompile(`(?i)\bpersonas?\b`)
	agentUX018Internal  = regexp.MustCompile(`(?i)\b(installations?|placements?|agent definitions?|specialized agents?|specialised agents?)\b`)
	agentUX018AgentDot  = regexp.MustCompile(`(?i)\bagent\s+·\s+\d`)
	agentUX018TodoOrKey = regexp.MustCompile(`\b(AGENTUX|AGENTP|AGENTDOC|UXBLIND)-\d+\b|hcmnext\.|persona_admin\.|agents\.[a-z_]+\b`)
)

// agentUX018Surfaces renders the four surfaces that show an agent, as a person
// who manages agents sees them, and returns what is read on each.
func agentUX018Surfaces(t *testing.T, language string) map[string]string {
	t.Helper()
	locale := ResolveProductLocale(language)
	persona := agentUXSetup2Persona()
	persona.Lifecycle = PersonaPublished
	persona.Audience = "employees"
	persona.Skills = []PersonaAdminSkill{{ID: "hcmnext.skill.knowledge_search_with_citations", Tier: "T0", DataClasses: []string{"POLICY_DOCUMENT"}}, {ID: "persona.chat_reply", Tier: "T2"}}
	persona.Installations = []PersonaAdminInstallation{{InstallationID: "installation-1", Version: "4", ConversationID: "general", Conversation: "general", Kind: "PUBLIC_CHANNEL"}}
	draft := agentUXSetup2Persona()
	draft.ID, draft.Handle, draft.Name = "assistant", "assistant", "Assistant"
	snapshot := agentUXSetup2Snapshot(persona)
	snapshot.Personas = []PersonaAdminPersona{persona, draft}

	operations := ApplyLocale(NewView(PageAgentOperations, "tenant", "owner", ""), locale)
	operations.AgentsProjection = &AgentsAvailabilityProjection{ViewerIsAdmin: true}
	controls := agentUX073Snapshot()
	controls.Agents = []PersonaAdminPersona{persona}
	rollout := AgentRolloutSnapshot{Available: true, CanPreview: true, Personas: []AgentRolloutPersona{{ID: "policy-helper", Name: "Policy Helper"}},
		Versions:      []AgentRolloutVersion{{PersonaID: "policy-helper", Version: 3}, {PersonaID: "policy-helper", Version: 4, Current: true}},
		Installations: []AgentRolloutInstallation{{ID: "general", PersonaID: "policy-helper", Version: 4, Visible: true, Name: "general", ConversationKind: "PUBLIC_CHANNEL", CanaryEligible: true}}}
	announcement := AgentAnnouncementRow{ID: "a1", AgentName: "Policy Helper", ConversationName: "general", OwnerName: "Walt Brennan", State: "ACTIVE", Instruction: "Tell employees about holidays.", ResultCode: "POSTED", Editor: AgentAnnouncementEditorValue{Cadence: "DAILY", Time: "09:00"}}

	mention := personaMentionFixture()
	mention.Locale = locale
	return map[string]string{
		"Agents page": agentUX055Text(renderAgentUXPage(t, language, AgentsAvailabilityProjection{Enabled: true, ViewerIsAdmin: true, Snapshot: agentUX055EmployeeSnapshot()})),
		"Agent setup": agentUX055Text(personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: locale}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{}, ViewerSubject: "ir-001-walt-brennan"}))),
		"Agent operations": agentUX055Text(agentUXR7Render(t, BuildAgentOperationsPage(operations)) +
			agentUXR7Render(t, RenderAgentControls(locale, controls, "done")) +
			agentUXR7Render(t, RenderAgentRolloutForm(locale, rollout)) +
			agentUXR7Render(t, RenderAgentAnnouncements(locale, AgentAnnouncementsSnapshot{Available: true, CanCreate: true, Rows: []AgentAnnouncementRow{announcement}}))),
		"Chat mention menu": agentUX055Text(renderPersonaMention(t, PersonaMentionMenu(mention))),
	}
}

// One noun, "agent", on every surface: no visible "persona", no "installation"
// or "placement" for an agent added to a conversation, no "agent · 2", and no
// todo id or copy key printed where a sentence should be.
func TestTodo_AGENTUX_018_VisibleVocabulary(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		for surface, text := range agentUX018Surfaces(t, language) {
			if found := agentUX018Persona.FindString(text); found != "" {
				t.Errorf("%s %s shows the internal word %q: …%s…", language, surface, found, agentUX018Around(text, found))
			}
			if found := agentUX018AgentDot.FindString(text); found != "" {
				t.Errorf("%s %s names an agent by position: %q", language, surface, found)
			}
			if found := agentUX018TodoOrKey.FindString(text); found != "" {
				t.Errorf("%s %s prints an internal id or copy key: %q …%s…", language, surface, found, agentUX018Around(text, found))
			}
			if language == "en-US" {
				if found := agentUX018Internal.FindString(text); found != "" {
					t.Errorf("%s %s uses %q for an agent in a conversation: …%s…", language, surface, found, agentUX018Around(text, found))
				}
			}
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s %s rendered nothing", language, surface)
			}
		}
	}

	// The two administration pages are named as one pair and link to each other.
	english := ResolveProductLocale("en-US")
	if personaAdminText(english, "title") != "Agent setup" || agentOperationsText(english, "title") != "Agent operations" {
		t.Fatalf("the administration pages are named %q and %q", personaAdminText(english, "title"), agentOperationsText(english, "title"))
	}
	operations := ApplyLocale(NewView(PageAgentOperations, "tenant", "owner", ""), english)
	operations.AgentsProjection = &AgentsAvailabilityProjection{ViewerIsAdmin: true}
	if page := agentUXR7Render(t, BuildAgentOperationsPage(operations)); !strings.Contains(page, Path(PagePersonaAdmin)) || !strings.Contains(page, Path(PageAgents)) {
		t.Fatal("Agent operations does not link to Agent setup and the Agents page in its header")
	}
	setup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: english}, State: PersonaAdminReady, Snapshot: agentUXSetup2Snapshot(agentUXSetup2Persona()), Client: &personaAdminTestClient{}}))
	if !strings.Contains(setup, Path(PageAgentOperations)) || !strings.Contains(setup, Path(PageAgents)) {
		t.Fatal("Agent setup does not link to Agent operations and the Agents page in its header")
	}
	// An agent is named by its display name and version.
	if got := agentControlNameVersion(english, "Policy Helper", "2"); got != "Policy Helper, version 2" {
		t.Fatalf("an agent is named %q", got)
	}
}

func agentUX018Around(text, found string) string {
	index := strings.Index(text, found)
	start, end := max(0, index-60), min(len(text), index+len(found)+60)
	return strings.Join(strings.Fields(text[start:end]), " ")
}
