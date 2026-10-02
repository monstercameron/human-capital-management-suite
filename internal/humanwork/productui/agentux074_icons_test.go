package productui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

var agentUX074SVG = regexp.MustCompile(`<svg[^>]*class="agent-icon".*?</svg>`)

// agentUX074IconNear returns the first agent icon drawn after the marker, the
// icon belonging to the row or card the marker opens.
func agentUX074IconNear(t *testing.T, surface, markup, marker string) string {
	t.Helper()
	start := strings.Index(markup, marker)
	if start < 0 {
		t.Fatalf("%s: marker %q not found", surface, marker)
	}
	icon := agentUX074SVG.FindString(markup[start:])
	if icon == "" {
		t.Fatalf("%s: no agent icon after %q", surface, marker)
	}
	return icon
}

// agentUX074IconBefore returns the last agent icon drawn before the marker.
func agentUX074IconBefore(t *testing.T, surface, markup, marker string) string {
	t.Helper()
	end := strings.Index(markup, marker)
	if end < 0 {
		t.Fatalf("%s: marker %q not found", surface, marker)
	}
	icons := agentUX074SVG.FindAllString(markup[:end], -1)
	if len(icons) == 0 {
		t.Fatalf("%s: no agent icon before %q", surface, marker)
	}
	return icons[len(icons)-1]
}

// agentUX074Surfaces draws one pair of agents on every surface that shows an
// agent and returns, per surface, the icon each agent wears there.
func agentUX074Surfaces(t *testing.T, assistant, policy agenticon.Value) map[string][2]string {
	t.Helper()
	render := func(node ui.Node) string {
		markup, err := ui.RenderToString(node)
		if err != nil {
			t.Fatal(err)
		}
		return markup
	}
	out := map[string][2]string{}

	// The Agents page: the Ask choices and the task list.
	task := AgentTask{ID: "t1", Title: "When is open enrollment?", State: AgentTaskCompleted, AnswerText: "November 2 to 20.", AnsweringAgentID: "assistant", AnsweringAgentDisplayName: "Assistant"}
	other := AgentTask{ID: "t2", Title: "What is the PTO policy?", State: AgentTaskCompleted, AnswerText: "Fifteen days.", AnsweringAgentID: "policy-helper", AnsweringAgentDisplayName: "Policy Helper"}
	agents := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true,
		Agents: []AgentSummary{{ID: "assistant", Name: "Assistant", Description: "Answers everyday questions.", Icon: assistant}, {ID: "policy-helper", Name: "Policy Helper", Description: "Answers policy questions.", Icon: policy}},
		Tasks:  []AgentTask{task, other},
	}})
	out["Agents page choice"] = [2]string{agentUX074IconNear(t, "Agents page choice", agents, `value="assistant"`), agentUX074IconNear(t, "Agents page choice", agents, `value="policy-helper"`)}
	out["Agents page task"] = [2]string{agentUX074IconNear(t, "Agents page task", agents, `data-task-id="t1"`), agentUX074IconNear(t, "Agents page task", agents, `data-task-id="t2"`)}

	// Agent setup: one card per agent.
	setupAssistant, setupPolicy := agentUXSetup2Persona(), agentUXSetup2Persona()
	setupAssistant.ID, setupAssistant.Handle, setupAssistant.Name, setupAssistant.Icon = "assistant", "assistant", "Assistant", assistant
	setupPolicy.Icon = policy
	snapshot := agentUXSetup2Snapshot(setupAssistant)
	snapshot.Personas = []PersonaAdminPersona{setupAssistant, setupPolicy}
	setup := render(PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{}}))
	out["Agent setup"] = [2]string{agentUX074IconNear(t, "Agent setup", setup, `data-persona-id="assistant"`), agentUX074IconNear(t, "Agent setup", setup, `data-persona-id="policy-helper"`)}

	// Agent operations: the Activity rows.
	live, paused := setupAssistant, setupPolicy
	live.Lifecycle, paused.Lifecycle = PersonaPublished, PersonaSuspended
	activity := render(RenderAgentControls(ResolveProductLocale("en-US"), AgentControlsSnapshot{Available: true, Agents: []PersonaAdminPersona{live, paused}}, "done"))
	out["Agent operations"] = [2]string{agentUX074IconNear(t, "Agent operations", activity, `data-persona-id="assistant"`), agentUX074IconNear(t, "Agent operations", activity, `data-persona-id="policy-helper"`)}

	// Chat: the sidebar rows and a message each agent wrote.
	model := chatui.Model{
		Locale: "en-US", State: chatui.StateReady, CurrentUser: "walt", CurrentTenantID: "tenant", SelectedID: "dm-assistant", AgentIconsReady: true,
		Conversations: []chatui.Conversation{
			{ID: "general", Name: "general", Kind: chatui.PublicChannel},
			{ID: "dm-assistant", Name: "Assistant", Kind: chatui.DirectMessage, Agent: true, AgentID: "assistant", Icon: assistant},
			{ID: "dm-policy", Name: "Policy Helper", Kind: chatui.DirectMessage, Agent: true, AgentID: "policy-helper", Icon: policy},
		},
		Messages:  []chatui.Message{{ID: "reply", AuthorID: "assistant", Author: "Assistant", Body: "Open enrollment runs November 2 to 20.", TimeLabel: "9:32", PersonaActor: &chatui.PersonaActor{PersonaID: "assistant", AgentID: "assistant", Trusted: true}}},
		Callbacks: chatui.Callbacks{SelectConversation: func(string) {}},
	}
	chat := render(chatui.Build(model))
	out["Chat sidebar"] = [2]string{agentUX074IconNear(t, "Chat sidebar", chat, `data-conversation-id="dm-assistant"`), agentUX074IconNear(t, "Chat sidebar", chat, `data-conversation-id="dm-policy"`)}
	out["Chat message"] = [2]string{agentUX074IconBefore(t, "Chat message", chat, "Open enrollment runs November 2 to 20."), out["Chat sidebar"][1]}
	return out
}

func TestTodo_AGENTUX_074(t *testing.T) {
	assistant, policy := agenticon.Generate(agenticon.Input{Name: "Assistant"}), agenticon.Generate(agenticon.Input{Name: "Policy Helper"})
	if assistant == policy {
		t.Fatal("the fixture agents need two different stored icons")
	}
	render := func(node ui.Node) string {
		markup, err := ui.RenderToString(node)
		if err != nil {
			t.Fatal(err)
		}
		return markup
	}

	// One agent, one icon: the stored icon on every surface.
	want := [2]string{render(agenticon.Node(assistant)), render(agenticon.Node(policy))}
	for surface, got := range agentUX074Surfaces(t, assistant, policy) {
		if got != want {
			t.Errorf("%s draws a different icon for an agent with a stored icon:\n got %v\nwant %v", surface, got, want)
		}
	}

	// The mention menu draws the stored icon too.
	props := personaMentionFixture()
	props.People = nil
	props.Agents = []PersonaMentionPersona{{ID: "assistant", Name: "Assistant", Handle: "assistant", Purpose: "Answers everyday questions.", Invocable: true, InstalledInConversation: true, AudienceIncludesViewer: true, Icon: assistant}}
	if menu := render(PersonaMentionMenu(props)); !strings.Contains(menu, want[0]) {
		t.Errorf("the mention menu does not draw the agent's stored icon: %s", menu)
	}

	// An agent with no stored icon wears one fallback everywhere: the one taken
	// from its id among the agents listed with it.
	fallbacks := agenticon.Fallbacks([]string{"assistant", "policy-helper"})
	wantFallback := [2]string{render(agenticon.Node(fallbacks["assistant"])), render(agenticon.Node(fallbacks["policy-helper"]))}
	if wantFallback[0] == wantFallback[1] {
		t.Fatal("two agents share a fallback")
	}
	for surface, got := range agentUX074Surfaces(t, agenticon.Value{}, agenticon.Value{}) {
		if got != wantFallback {
			t.Errorf("%s draws a different fallback for an agent with no stored icon:\n got %v\nwant %v", surface, got, wantFallback)
		}
	}
	if agentUX074IconValue(agenticon.Value{}, "assistant", []string{"assistant", "policy-helper"}) != fallbacks["assistant"] || agentUX074IconValue(assistant, "assistant", nil) != assistant {
		t.Fatal("the pages' icon function is not the one Chat ends in")
	}

	// A task answered by the general agent wears the general choice's icon.
	general := AgentTask{ID: "g", Title: "Welcome a teammate", State: AgentTaskCompleted, AnswerText: "Say hello."}
	page := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, Tasks: []AgentTask{general}}})
	if agentUX074IconNear(t, "general choice", page, `id="agents-choice-general"`) != agentUX074IconNear(t, "general task", page, `data-task-id="g"`) {
		t.Fatal("the general agent wears one icon as a choice and another on its task")
	}
}

func TestTodo_AGENTUX_074_SetupNeverTellsAnAdministratorToAskOne(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		// The reader is the only person the directory lists as looking after
		// agents: the page says what is true and what they can do.
		self := personaAdminRender(t, personaAdminNoStarterNotice(locale, []PersonaAdminTarget{{ID: "ir-001-walt-brennan", Label: "Walt Brennan", Role: "agent_administrator"}}, "ir-001-walt-brennan"))
		for _, want := range []string{personaAdminR5SetupText(locale, "no_template"), personaAdminR5SetupText(locale, "no_template_next")} {
			if want == "" || !strings.Contains(self, want) {
				t.Fatalf("%s: the notice is missing %q: %s", language, want, self)
			}
		}
		if strings.Contains(self, "Walt Brennan") || strings.Contains(self, "<a ") {
			t.Fatalf("%s: the notice sends the administrator to themselves: %s", language, self)
		}
	}
	english := ResolveProductLocale("en-US")
	nobody := personaAdminRender(t, personaAdminNoStarterNotice(english, nil, "ir-001-walt-brennan"))
	if strings.Contains(nobody, "Ask the person who manages") || strings.Contains(nobody, "Ask your") || !strings.Contains(nobody, "this build ships with none") || !strings.Contains(nobody, "choose Edit on its card") {
		t.Fatalf("with nobody to name, the notice must say plainly that no template ships: %s", nobody)
	}
	// Someone else looks after the installation: they are named, with a way to
	// reach them.
	other := personaAdminRender(t, personaAdminNoStarterNotice(english, []PersonaAdminTarget{{ID: "ir-001-walt-brennan", Label: "Walt Brennan", Role: "platform_administrator"}, {ID: "ir-009-alex-morgan", Label: "Alex Morgan", Role: "agent_administrator"}}, "ir-001-walt-brennan"))
	if !strings.Contains(other, "Alex Morgan") || !strings.Contains(other, "Message Alex Morgan") || strings.Contains(other, "Walt Brennan") {
		t.Fatalf("another administrator is not offered: %s", other)
	}
	// The whole page, as the administrator who is reading it.
	persona := agentUXSetup2Persona()
	snapshot := agentUXSetup2Snapshot(persona)
	snapshot.StarterCatalogAvailable = true
	page := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: english}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{}, ViewerSubject: "ir-001-walt-brennan"}))
	if !strings.Contains(page, `id="persona-admin-new-agent-note"`) || strings.Contains(page, "Ask the person who manages this workspace") {
		t.Fatalf("Agent setup still tells its administrator to ask an administrator: %s", page)
	}
}

func TestTodo_AGENTUX_074_PeopleWearTheirPhotographs(t *testing.T) {
	persona := agentUXSetup2Persona()
	client := &personaAdminTestClient{snapshot: agentUXSetup2Snapshot(persona)}
	view := NewView(PagePersonaAdmin, "ironridge", "ir-001-walt-brennan", "")
	view.EffectivePermissions = []RolePagePermission{{Page: PagePersonaAdmin, View: true}}
	view.People = []Person{
		{ID: "w1", SubjectID: "ir-001-walt-brennan", Name: "Walt Brennan", PhotoURL: "/workspace/assets/person-ir-001-small.jpg"},
		{ID: "w3", WorkerID: "ir-003-loretta-haynes", Name: "Loretta Haynes", PhotoURL: "/workspace/assets/person-ir-003-small.jpg"},
		{ID: "w8", SubjectID: "ir-008-curtis-bell", Name: "Curtis Bell"},
	}
	markup := personaAdminRender(t, BuildPersonaAdminPage(view, client))
	for _, photo := range []string{"/workspace/assets/person-ir-001-small.jpg", "/workspace/assets/person-ir-003-small.jpg"} {
		if !strings.Contains(markup, `class="avatar tiny"`) || !strings.Contains(markup, `src="`+photo+`"`) {
			t.Fatalf("Agent setup does not draw the photograph %s the directory holds: %s", photo, markup)
		}
	}
	// A person with no photograph keeps the initials, and a page with no
	// directory changes nothing.
	decorated := agentUX074WithPeoplePhotos(view, client.snapshot)
	if decorated.Personas[0].ReviewerAvatarURL != "" {
		t.Fatal("a person without a photograph was given one")
	}
	if client.snapshot.Personas[0].OwnerAvatarURL != "" {
		t.Fatal("decorating the snapshot changed the client's copy")
	}
	bare := view
	bare.People = nil
	if got := agentUX074WithPeoplePhotos(bare, client.snapshot); got.Personas[0].OwnerAvatarURL != "" {
		t.Fatal("a page with no directory invented a photograph")
	}
}

// The Ask form and the task list use the page: the rules are in the stylesheet
// the browser accepts, and a task is two lines of markup, not five.
func TestTodo_AGENTUX_074_Browser(t *testing.T) {
	sheet := Stylesheet()
	for _, want := range []string{
		agentUX074LayoutStyles,
		`.agents-task-summary{display:flex;align-items:center;gap:.5rem`,
		`.agents-task-summary :is(.agents-task-preview,.agents-task-failure-reason){flex:1 1 0;min-width:0;margin:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}`,
		`.agents-tasks .agents-task-list,.agents-tasks .agents-task-row,.agents-task-listing{width:100%;max-width:none`,
		`.agent-page-frame p,.agent-page-frame label{max-inline-size:none`,
		`.agents-composer-help,.agents-composer-actions-help`,
		`max-inline-size:72ch`,
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("the product stylesheet is missing %.90q", want)
		}
	}
	task := AgentTask{ID: "t1", Title: "When is open enrollment?", State: AgentTaskCompleted, AnswerText: "Open enrollment runs November 2 to 20.", AnsweringAgentID: "assistant", AnsweringAgentDisplayName: "Assistant"}
	page := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, Agents: []AgentSummary{{ID: "assistant", Name: "Assistant"}}, Tasks: []AgentTask{task}}})
	if strings.Contains(page, "<style") {
		t.Fatal("the Agents page emits a style element the content security policy refuses")
	}
	start := strings.Index(page, `data-task-id="t1"`)
	row := page[start:]
	row = row[:strings.Index(row, "</li>")]
	// Line one: question and time. Line two: who answered and the excerpt.
	for _, want := range []string{`class="agents-task-row-heading"`, "When is open enrollment?", `class="agents-task-time"`, `<p class="agents-task-summary">`, "Answered by Assistant", "Open enrollment runs November 2 to 20."} {
		if !strings.Contains(row, want) {
			t.Fatalf("task row is missing %q: %s", want, row)
		}
	}
	if strings.Count(row, "<p ") != 1 || strings.Contains(row, `class="agents-task-row-meta"`) {
		t.Fatalf("a completed task with no documents is its heading and one summary line: %s", row)
	}
}
