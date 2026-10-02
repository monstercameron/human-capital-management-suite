package productui

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

var agentUX055Tag = regexp.MustCompile(`<[^>]*>`)

// agentUX055Text is what a person reads on a page: the markup with its tags
// and attributes removed.
func agentUX055Text(markup string) string {
	return agentUX055Tag.ReplaceAllString(markup, " ")
}

func agentUX055EmployeeSnapshot() AgentSnapshot {
	at := time.Now().Add(-10 * time.Minute)
	return AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true,
		Agents: []AgentSummary{{ID: "policy-helper", Name: "Policy Helper", Description: "Answers policy questions from the documents it may read."}},
		Tasks: []AgentTask{
			{ID: "answered", Title: "How many days of leave do I have?", State: AgentTaskCompleted, AnswerText: "You have 12 days left.", AnsweringAgentID: "policy-helper", AnsweringAgentDisplayName: "Policy Helper", AnsweringAgentVersion: "6", CreatedAt: at, UpdatedAt: at},
			{ID: "stopped", Title: "Summarize the travel policy", State: AgentTaskFailed, FailureReason: "It took too long.", Retryable: true, AnsweringAgentID: "policy-helper", AnsweringAgentDisplayName: "Policy Helper", AnsweringAgentVersion: "6", CreatedAt: at, UpdatedAt: at},
		},
	}
}

// The Agents page is an employee's page; Agent setup and Agent operations are
// an owner's. Each says what its reader came for, in that reader's words.
func TestTodo_AGENTUX_055(t *testing.T) {
	// An employee: who can help me, ask, see my answers, recover from a failure.
	employee := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: agentUX055EmployeeSnapshot()})
	for _, want := range []string{
		"Who should answer?", "Policy Helper", "Answers policy questions from the documents it may read.", // who can help
		`id="agents-composer-input"`, `data-agent-action="quick-answer"`, // ask
		"How many days of leave do I have?", "You have 12 days left.", "Answered by Policy Helper", // see my answers
		"Summarize the travel policy", "It took too long.", `data-agent-ask-again="true"`, // recover
	} {
		if !strings.Contains(employee, want) {
			t.Fatalf("the Agents page does not give an employee %q: %s", want, employee)
		}
	}
	read := strings.ToLower(agentUX055Text(employee))
	for _, ownerWord := range []string{"version", "rollout", "owner", "persona", "installation", "operations", "setup", "publish", "lifecycle", "evaluation"} {
		if strings.Contains(read, ownerWord) {
			t.Errorf("the Agents page speaks to an employee in an owner's terms: %q", ownerWord)
		}
	}
	if strings.Contains(employee, `class="agents-page-nav"`) || strings.Contains(employee, Path(PageAgentOperations)) || strings.Contains(employee, Path(PagePersonaAdmin)) {
		t.Fatal("an employee is offered the owner pages")
	}

	// The same page for someone who manages agents: the owner pages are behind
	// the page switcher, and the page itself is unchanged.
	owner := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, ViewerIsAdmin: true, Snapshot: agentUX055EmployeeSnapshot()})
	if !strings.Contains(owner, `class="agents-page-nav"`) || !strings.Contains(owner, Path(PageAgentOperations)) || !strings.Contains(owner, Path(PagePersonaAdmin)) {
		t.Fatalf("owner controls are not behind the page switcher: %s", owner)
	}
	if strings.Count(owner, `data-agent-action=`) != strings.Count(employee, `data-agent-action=`) {
		t.Fatal("the page gives an owner different controls from an employee")
	}

	// An owner on Agent setup: what each agent may do and who can use it.
	persona := agentUXSetup2Persona()
	persona.Lifecycle = PersonaPublished
	persona.Audience = "Employees"
	persona.Skills = []PersonaAdminSkill{{ID: "hcmnext.skill.knowledge_search_with_citations", Name: "Search documents", Description: "Finds and cites policy documents.", Tier: "T0"}}
	persona.Installations = []PersonaAdminInstallation{{InstallationID: "i1", Version: "4", ConversationID: "general", Conversation: "general", Kind: "CHANNEL"}}
	setup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: PersonaAdminReady, Snapshot: agentUXSetup2Snapshot(persona), Client: &personaAdminTestClient{}, ViewerSubject: "ir-001-walt-brennan"}))
	english := ResolveProductLocale("en-US")
	skill := persona.Skills[0]
	for name, want := range map[string]string{
		"which agent":        "Policy Helper",
		"what it may do":     PersonaSkillLabelForLocale(english, skill.ID, skill.Name, skill.Description),
		"what that means":    PersonaSkillDescription(english, skill.ID, skill.Description),
		"who can use it":     personaAdminText(english, "who_can_use"),
		"who answers for it": "Walt Brennan",
		"where it is used":   "#general",
		"how to stop it":     agentUXR7Text(english, "pause"),
	} {
		if want == "" || !strings.Contains(setup, want) {
			t.Fatalf("Agent setup does not tell an owner %s (%q): %s", name, want, agentUX055Text(setup))
		}
	}

	// An owner on Agent operations: what it did, what it cost, how to stop it.
	locale := ResolveProductLocale("en-US").WithTimeZone("America/New_York")
	activity := agentUXR7Render(t, RenderAgentControls(locale, agentUX073Snapshot(), "done"))
	for _, want := range []string{">Pause agent<", agentUXR7Text(locale, "asked_by"), "Walt Brennan", agentUXR7Text(locale, "cost"), "Failed after 1 hour", agentControlsText(locale, "failure_model_unavailable")} {
		if !strings.Contains(activity, want) {
			t.Fatalf("Agent operations does not tell an owner %q", want)
		}
	}
	// Times are the viewer's, never UTC, and people are named in full.
	if strings.Contains(agentUX055Text(activity), "UTC") || strings.Contains(activity, ">Walt<") {
		t.Fatalf("Activity shows a UTC time or a first name: %s", agentUX055Text(activity))
	}

	// Rollout can move a conversation back to any earlier reviewed version.
	rollout := AgentRolloutSnapshot{Available: true, CanPreview: true, Personas: []AgentRolloutPersona{{ID: "policy", Name: "Policy Helper"}},
		Versions:      []AgentRolloutVersion{{PersonaID: "policy", Version: 2}, {PersonaID: "policy", Version: 4}, {PersonaID: "policy", Version: 6, Current: true}},
		Installations: []AgentRolloutInstallation{{ID: "general", PersonaID: "policy", Version: 6, Visible: true, Name: "general", ConversationKind: "PUBLIC_CHANNEL", CanaryEligible: true}}}
	form := agentUXR7Render(t, RenderAgentRolloutForm(locale, rollout))
	for _, want := range []string{agentUXR7Text(locale, "rollback_group"), `value="2"`, `value="4"`, `selected value="6"`} {
		if !strings.Contains(form, want) {
			t.Fatalf("Rollout does not offer an earlier reviewed version (%q): %s", want, form)
		}
	}
}

// The three pages hold their layout in the stylesheet the browser accepts and
// carry no inline style, which the page's content security policy refuses.
func TestTodo_AGENTUX_055_Browser(t *testing.T) {
	sheet := Stylesheet()
	for _, want := range []string{
		`.persona-admin-page [popover]:not(:popover-open){display:none}`,
		`.persona-admin-page .persona-admin-secondary-actions:popover-open,.persona-admin-page .persona-admin-add-placement-form:popover-open{position:fixed`,
		`inset-block-start:anchor(bottom)`,
		agentUX073LayoutStyles, agentUX074LayoutStyles,
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("the product stylesheet is missing %.90q", want)
		}
	}
	persona := agentUXSetup2Persona()
	persona.Lifecycle = PersonaPublished
	pages := map[string]string{
		"Agents":      renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, ViewerIsAdmin: true, Snapshot: agentUX055EmployeeSnapshot()}),
		"Agent setup": personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: PersonaAdminReady, Snapshot: agentUXSetup2Snapshot(persona), Client: &personaAdminTestClient{}})),
		"Activity":    agentUXR7Render(t, RenderAgentControls(ResolveProductLocale("en-US"), agentUX073Snapshot(), "done")),
	}
	for name, markup := range pages {
		if strings.Contains(markup, "<style") || strings.Contains(markup, ` style="`) {
			t.Errorf("%s carries an inline style the content security policy refuses", name)
		}
	}
	// The More menu and the add-to-conversation form are popovers opened by the
	// button that names them; closed, they take no room on the page.
	setup := pages["Agent setup"]
	if !strings.Contains(setup, `popovertarget="persona-admin-more-policy-helper"`) || !strings.Contains(setup, `id="persona-admin-more-policy-helper"`) || !strings.Contains(setup, `popover="auto"`) {
		t.Fatalf("the More menu is not a popover with its own opener: %s", setup)
	}
}

// The version badge sits directly after the agent's name, on the name's line.
func TestTodo_AGENTUX_055_CardHeader(t *testing.T) {
	persona := agentUXSetup2Persona()
	persona.Lifecycle, persona.Version = PersonaPublished, "2"
	card := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona, agentUXSetup2Snapshot(persona)))
	name := card[strings.Index(card, `<div class="persona-admin-card-name">`):]
	name = name[:strings.Index(name, "</div>")]
	title, badge := strings.Index(name, ">Policy Helper</h3>"), strings.Index(name, "Version 2 · Published")
	if title < 0 || badge < title || !strings.Contains(name, `class="persona-admin-statuses"`) {
		t.Fatalf("the version badge does not follow the name inside the name row: %s", name)
	}
	if strings.Count(card, "Version 2 · Published") != 1 {
		t.Fatalf("the version badge is drawn more than once in the card header: %s", card)
	}
	// The icon stays inside the title, where the icon controls replace it.
	header := card[strings.Index(card, `<div class="persona-admin-card-title">`):strings.Index(card, `class="persona-admin-card-controls"`)]
	if strings.Index(header, `class="agent-icon"`) > strings.Index(header, `class="persona-admin-card-name"`) || !strings.Contains(header, "@policy-helper") {
		t.Fatalf("the title lost its icon or its handle line: %s", header)
	}
	sheet := Stylesheet()
	for _, want := range []string{
		`.persona-admin-page .persona-admin-card-name{display:flex;flex-wrap:wrap;align-items:center;gap:.25rem .75rem;min-width:0}`,
		`.persona-admin-page .persona-admin-card-title{display:grid;grid-template-columns:auto minmax(0,1fr)`,
		`.persona-admin-page .persona-admin-card-title>.agent-icon{grid-row:1 / span 2`,
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("the served stylesheet is missing %q", want)
		}
	}
}

func TestTodo_AGENTUX_055_Accessibility(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		employee := renderAgentUXPage(t, language, AgentsAvailabilityProjection{Enabled: true, Snapshot: agentUX055EmployeeSnapshot()})
		for _, want := range []string{`role="tablist"`, `role="tab"`, `aria-selected="true"`, `<label for="agents-composer-input"`, `<legend>`, `aria-live="polite"`, `dir="` + string(locale.Direction) + `"`} {
			if !strings.Contains(employee, want) {
				t.Fatalf("%s Agents page is missing %q", language, want)
			}
		}
		// A task's state is said in words on the row link, not by colour alone.
		if !strings.Contains(employee, `aria-label="`+locale.Text("agents.open_named_task", map[string]string{"request": "Summarize the travel policy"})+`"`) {
			t.Fatalf("%s: a task row has no accessible name", language)
		}
		activity := agentUXR7Render(t, RenderAgentControls(locale, agentUX073Snapshot(), "done"))
		for _, want := range []string{`aria-labelledby="agent-activity-agents-title"`, `<ul class="agent-activity-agents">`, `<th scope="col">`, `<label for="agent-history-agent">`, `role="status"`, agentUX073Text(locale, "state_paused"), agentUX073Text(locale, "state_answering")} {
			if !strings.Contains(activity, want) {
				t.Fatalf("%s Activity is missing %q", language, want)
			}
		}
		// The agent icon is decorative beside the agent's written name.
		if strings.Count(activity, `class="agent-activity-icon"`) != strings.Count(activity, `aria-hidden="true" class="agent-activity-icon"`) {
			t.Fatalf("%s: an agent icon is exposed to assistive technology without a name", language)
		}
	}
}
