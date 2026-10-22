package productui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_AGENTUX_019 closes the 2026-09-30 adversarial review round 1.
// Each subtest pins one listed finding in the state the lane fixed it in,
// citing the owning todo; a regression fails here. Findings fixed by later
// product decisions (F8, governed by the test-pinned CHATBUG-028) assert the
// governed state. Two findings stay open as filed gaps, per the GREEN escape
// clause: F44 (the client-side task detail has no document-title mechanism to
// pin from the server) and the second-review scoring itself, whose later
// rounds are the AGENTUX-020+ todos.
func TestTodo_AGENTUX_019(t *testing.T) {
	t.Run("F2 status and tier pills do not wrap mid-word", func(t *testing.T) {
		css := agentsStylesheet()
		status := declarationsFor(css, ".agents-task-status")
		if !strings.Contains(status, "white-space:nowrap") {
			t.Fatalf("status pill can wrap: %s", status)
		}
		if label := PersonaTierLabel(ResolveProductLocale("en-US"), "T0"); label != "Read-only" {
			t.Fatalf("tier label = %q", label)
		}
	})

	t.Run("F4 hidden controls stay hidden under button styles", func(t *testing.T) {
		if css := agentsStylesheet(); !strings.Contains(css, ".agents-page [hidden]{display:none !important;}") {
			t.Fatalf("hidden override missing: %s", css[:min(400, len(css))])
		}
	})

	t.Run("F5 first-time task list selects the Active tab", func(t *testing.T) {
		markup := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true}})
		if !strings.Contains(markup, `data-selected-task-filter="active"`) {
			t.Fatalf("default task tab is not Active: %s", agentUX055Text(markup))
		}
	})

	t.Run("F16 composer tasks and view share one content width", func(t *testing.T) {
		css := agentsStylesheet()
		for _, selector := range []string{".agents-composer", ".agents-task-list", ".agents-task-view"} {
			declarations := declarationsFor(css, selector)
			if !strings.Contains(declarations, "width:100%") {
				t.Errorf("%s is not full width: %s", selector, declarations)
			}
		}
	})

	t.Run("F17 adjusted chip does not hang below its sentence", func(t *testing.T) {
		css := agentsStylesheet()
		declarations := declarationsFor(css, ".agents-plan-step")
		if !strings.Contains(declarations, "align-items:center") {
			t.Fatalf("plan step marker alignment = %s", declarations)
		}
	})

	t.Run("F18 context and progress pills hang off the tab edge", func(t *testing.T) {
		css := agentsStylesheet()
		declarations := declarationsFor(css, ".agents-task-filter")
		if !strings.Contains(declarations, "justify-content:center") {
			t.Fatalf("filter layout = %s", declarations)
		}
	})

	t.Run("F19 selected tab is not a filled near-black block", func(t *testing.T) {
		css := agentsStylesheet() + agentUXR7Stylesheet()
		declarations := declarationsFor(css, `.agents-task-filter[aria-selected="true"]`)
		if !strings.Contains(declarations, "background:var(--surface-subtle,var(--canvas))") || !strings.Contains(declarations, "box-shadow:none") {
			t.Fatalf("selected tab = %s", declarations)
		}
	})

	t.Run("F20 composer holes collapse and orphans cannot strand", func(t *testing.T) {
		css := agentsStylesheet()
		if !strings.Contains(css, ".agents-composer > :empty:not(textarea):not(input):not(select)") {
			t.Fatal("empty composer children take space")
		}
	})

	t.Run("F31 empty states read as sentences not bullets", func(t *testing.T) {
		empty := personaAdminRender(t, ui.CreateElement(EmptyState, EmptyStateProps{Title: "Nothing here yet", Description: "Start an agent to see it listed."}))
		if strings.Contains(empty, "<ul") || strings.Contains(empty, "<li") {
			t.Fatalf("empty state renders bullets: %s", empty)
		}
		if !strings.Contains(empty, "<h2") || !strings.Contains(empty, "<p") {
			t.Fatalf("empty state is not a titled sentence: %s", empty)
		}
	})

	t.Run("F41 hint text never drops to 11px", func(t *testing.T) {
		css := agentsStylesheet() + agentUXR7Stylesheet()
		sizes := regexp.MustCompile(`font-size:(?:([\d.]+)rem|([\d.]+)px)`).FindAllStringSubmatch(css, -1)
		if len(sizes) == 0 {
			t.Fatal("no font sizes found to bound")
		}
		for _, size := range sizes {
			points := 0.0
			if size[1] != "" {
				value, err := strconv.ParseFloat(size[1], 64)
				if err != nil {
					t.Fatal(err)
				}
				points = value * 16
			} else {
				value, err := strconv.ParseFloat(size[2], 64)
				if err != nil {
					t.Fatal(err)
				}
				points = value
			}
			if points < 12 {
				t.Fatalf("text below 12px: %q", size[0])
			}
		}
	})

	t.Run("F22 a completed task offers a next action", func(t *testing.T) {
		task := AgentTask{ID: "done", Goal: "Summarize leave balances", State: AgentTaskCompleted, AnswerText: "You have 12 days left.", AnsweringAgentDisplayName: "Policy Helper"}
		markup := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, SelectedTask: &task}})
		for _, want := range []string{"Ask a follow-up", "Copy answer"} {
			if !strings.Contains(markup, want) {
				t.Errorf("completed task offers no %q: %s", want, agentUX055Text(markup))
			}
		}
	})

	t.Run("F11 running agents list or explain the empty state", func(t *testing.T) {
		locale := ResolveProductLocale("en-US")
		withRuns := renderAgentOperationsTest(t, RenderAgentControls(locale, AgentControlsSnapshot{Available: true, Runs: []AgentControlRun{{ID: "run-1", Name: "Policy Helper", Version: "2", State: "RUNNING", Since: "9:42 AM", Revision: 7, Actions: []string{"pause"}}}}, "done"))
		for _, want := range []string{"agent-running-card", "Policy Helper, version 2", "Running"} {
			if !strings.Contains(withRuns, want) {
				t.Errorf("running list missing %q: %s", want, withRuns)
			}
		}
		empty := renderAgentOperationsTest(t, RenderAgentControls(locale, AgentControlsSnapshot{Available: true}, "done"))
		for _, want := range []string{"No agents are running right now.", "Recent finished runs are listed below."} {
			if !strings.Contains(empty, want) {
				t.Errorf("running empty state missing %q: %s", want, empty)
			}
		}
	})

	t.Run("F12 one refresh control in one size", func(t *testing.T) {
		markup := renderAgentOperationsTest(t, RenderAgentControls(ResolveProductLocale("en-US"), AgentControlsSnapshot{Available: true, Runs: []AgentControlRun{{ID: "run-1", Name: "Policy Helper", State: "RUNNING"}}}, "done"))
		if count := strings.Count(markup, `data-owner-refresh="true"`); count != 1 {
			t.Fatalf("refresh controls = %d: %s", count, markup)
		}
		if !strings.Contains(markup, `class="button secondary"`) || !strings.Contains(markup, "Refresh running agents") {
			t.Fatalf("refresh is not the single secondary control: %s", markup)
		}
	})

	t.Run("F13 the operations card keeps heading status and rows", func(t *testing.T) {
		markup := renderAgentOperationsTest(t, RenderAgentControls(ResolveProductLocale("en-US"), AgentControlsSnapshot{Available: true, Runs: []AgentControlRun{{ID: "run-1", Name: "Policy Helper", Version: "2", State: "RUNNING", Actions: []string{"pause"}}}}, "done"))
		for _, want := range []string{`<h2 id="agent-controls-title"`, `id="agent-controls-status"`, "agent-activity-heading", "agent-running-card"} {
			if !strings.Contains(markup, want) {
				t.Errorf("operations rhythm missing %q: %s", want, markup)
			}
		}
		if count := strings.Count(markup, "<h2"); count != 1 {
			t.Errorf("operations card headings = %d: %s", count, markup)
		}
	})

	t.Run("F6 the admin page creates an agent or says why not", func(t *testing.T) {
		locale := ResolveProductLocale("en-US")
		withStarters := PersonaAdminSnapshot{Available: true, StarterCatalogAvailable: true, Starters: []PersonaAdminStarter{{ID: "policy-helper", Version: 1, ManifestID: "manifest", SkillGrantIDs: []string{"grant"}, ChannelClasses: []string{"PRIVATE"}}}}
		ready := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: locale}, State: PersonaAdminReady, Snapshot: withStarters, Client: &personaAdminTestClient{snapshot: withStarters}}))
		if !strings.Contains(ready, "data-persona-new-agent") {
			t.Fatalf("no creation control with starters available: %s", agentUX055Text(ready))
		}
		bare := PersonaAdminSnapshot{Available: true}
		without := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: locale}, State: PersonaAdminReady, Snapshot: bare, Client: &personaAdminTestClient{snapshot: bare}}))
		if !strings.Contains(without, locale.Text("persona_admin.empty")) {
			t.Fatalf("no one-sentence notice without starters: %s", agentUX055Text(without))
		}
	})

	t.Run("F25 F26 F27 badge sentence and stepper agree", func(t *testing.T) {
		locale := ResolveProductLocale("en-US")
		persona := agentUXSetup2Persona()
		persona.Version = "6"
		persona.Lifecycle, persona.ReviewApproved, persona.EvaluationStatus = PersonaInReview, true, "RUNNING"
		card := personaAdminRender(t, personaAdminCard(locale, &agentDocInstr2ReviewClient{}, persona, agentUXSetup2Snapshot(persona)))
		for place, want := range map[string]string{"stepper": "Step 3 of 4: Evaluation running", "badge": "Version 6 · Evaluation running", "sentence": "Version 6 is being evaluated."} {
			if !strings.Contains(card, want) {
				t.Errorf("the %s does not say %q: %s", place, want, agentUX055Text(card))
			}
		}
		if count := strings.Count(card, `aria-current="step"`); count != 1 {
			t.Errorf("current steps marked = %d", count)
		}
	})

	t.Run("F29 the review card is state-aware and holds no editor", func(t *testing.T) {
		locale := ResolveProductLocale("en-US")
		persona := agentUXSetup2Persona()
		markup := personaAdminRender(t, personaAdminLifecycleBlock(locale, persona, agentUXSetup2Snapshot(persona)))
		if !strings.Contains(markup, "Waiting for Curtis Bell to review version 4.") {
			t.Fatalf("review copy is not state-aware: %s", agentUX055Text(markup))
		}
		if strings.Contains(markup, "<textarea") || strings.Contains(markup, `type="text"`) {
			t.Fatalf("an editor is nested in the review card: %s", markup)
		}
	})

	t.Run("F30 skills read as sentences not internal codes", func(t *testing.T) {
		locale := ResolveProductLocale("en-US")
		persona := agentUXSetup2Persona()
		persona.Skills = []PersonaAdminSkill{{ID: "hcmnext.skill.knowledge_search_with_citations", Description: "Finds approved policy passages and cites them.", Tier: "T0"}, {ID: "persona.chat_reply", Tier: "T2"}}
		card := personaAdminRender(t, personaAdminCard(locale, &agentDocInstr2ReviewClient{}, persona, agentUXSetup2Snapshot(persona)))
		for _, want := range []string{"Knowledge search with citations", "Chat reply"} {
			if !strings.Contains(card, want) {
				t.Errorf("skill sentence missing %q: %s", want, agentUX055Text(card))
			}
		}
		visible := strings.Split(card, `<details class="persona-admin-technical">`)[0]
		for _, forbidden := range []string{"knowledge_search_with_citations", "persona.chat_reply"} {
			if strings.Contains(visible, forbidden) {
				t.Errorf("business surface leaked %q: %s", forbidden, agentUX055Text(card))
			}
		}
	})

	t.Run("F32 owners stewards and reviewers read as full names", func(t *testing.T) {
		persona := agentUXSetup2Persona()
		if persona.OwnerName == "" || !strings.Contains(persona.OwnerName, " ") || persona.StewardName == "" || !strings.Contains(persona.StewardName, " ") {
			t.Fatalf("fixture owners are not full names: %+v", persona)
		}
		card := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &agentDocInstr2ReviewClient{}, persona, agentUXSetup2Snapshot(persona)))
		for _, want := range []string{persona.OwnerName, persona.StewardName} {
			if !strings.Contains(card, want) {
				t.Errorf("card omits %q: %s", want, agentUX055Text(card))
			}
		}
	})

	t.Run("F15 editable inputs use the surface token", func(t *testing.T) {
		css := agentAccessStyles + AgentAnnouncementsStyles
		for _, selector := range []string{
			".agent-admin-access-page :is(input:not([type=checkbox]):not([type=radio]),select,textarea){",
			".agent-spend-limits input{",
			".agent-announcements input:not([type=radio]):not([type=checkbox]),.agent-announcements select,.agent-announcements textarea{",
		} {
			start := strings.Index(css, selector)
			if start < 0 {
				t.Fatalf("input rule %q vanished", selector)
			}
			rule := css[start : start+strings.Index(css[start:], "}")]
			if !strings.Contains(rule, "background:var(--hcm-color-surface") {
				t.Errorf("editable inputs look disabled: %s", rule)
			}
		}
		for _, disabled := range []string{
			".agent-admin-access-page :is(input:not([type=checkbox]):not([type=radio]),select,textarea):disabled{background:var(--hcm-color-canvas)}",
			".agent-spend-limits input:disabled{background:var(--hcm-color-canvas)}",
		} {
			if !strings.Contains(css, disabled) {
				t.Errorf("disabled inputs lost their distinction: %q", disabled)
			}
		}
	})

	t.Run("F36 English inside Arabic keeps its own direction", func(t *testing.T) {
		task := AgentTask{ID: "done", Goal: "Summarize leave balances", State: AgentTaskCompleted, AnswerText: "You have 12 days left.", AnsweringAgentDisplayName: "Policy Helper"}
		markup := renderAgentUXPage(t, "ar", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, SelectedTask: &task}})
		if !strings.Contains(markup, `dir="auto"`) {
			t.Fatalf("no direction isolation in the Arabic task view: %s", agentUX055Text(markup))
		}
	})

	t.Run("F39 agent setup labels are translated", func(t *testing.T) {
		for _, localeID := range []string{"de-DE", "ar"} {
			persona := agentUXSetup2Persona()
			markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale(localeID)}, State: PersonaAdminReady, Snapshot: agentUXSetup2Snapshot(persona), Client: &personaAdminTestClient{snapshot: agentUXSetup2Snapshot(persona)}}))
			if strings.Contains(markup, "⟦") {
				t.Fatalf("%s setup leaks untranslated labels: %s", localeID, agentUX055Text(markup))
			}
		}
	})

	t.Run("F42 person chips are labeled names not bare placeholders", func(t *testing.T) {
		chip := personaAdminRender(t, personaAdminPersonChip("ir-001-walt-brennan", "Walt Brennan", "WB", "", "Unknown person"))
		if !strings.Contains(chip, ">Walt Brennan</a>") || !strings.Contains(chip, "WB") {
			t.Fatalf("person chip is unlabeled: %s", chip)
		}
		unknown := personaAdminRender(t, personaAdminPersonChip("", "", "", "", "Unknown person"))
		if !strings.Contains(unknown, "Unknown person") {
			t.Fatalf("unknown person has no label: %s", unknown)
		}
	})

	t.Run("F43 agent pages share one frame and logical gutters", func(t *testing.T) {
		agents := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true}})
		persona := agentUXSetup2Persona()
		admin := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: PersonaAdminReady, Snapshot: agentUXSetup2Snapshot(persona), Client: &personaAdminTestClient{snapshot: agentUXSetup2Snapshot(persona)}}))
		for name, markup := range map[string]string{"agents": agents, "setup": admin} {
			if !strings.Contains(markup, "agent-page-frame") {
				t.Errorf("%s left the shared frame: %s", name, agentUX055Text(markup))
			}
		}
		if css := agentsStylesheet(); strings.Contains(css, "padding-left") || strings.Contains(css, "padding-right") {
			t.Fatalf("agents gutters cannot mirror: %s", css)
		}
	})

	t.Run("F37 Chat loads in its locale with its direction", func(t *testing.T) {
		view := ApplyLocale(NewView(PageChat, "tenant-1", "owner-1", ""), ResolveProductLocale("ar"))
		markup := personaAdminRender(t, BuildChatPage(view, chatui.Model{}))
		if !strings.Contains(markup, `dir="rtl"`) {
			t.Fatalf("loading chat is unmirrored: %s", agentUX055Text(markup))
		}
		if !strings.Contains(markup, `data-chat-state="loading"`) {
			t.Fatalf("not the loading state: %s", agentUX055Text(markup))
		}
	})

	t.Run("F35 Agent setup and Agent operations are adjacent", func(t *testing.T) {
		for _, current := range []PageID{PagePersonaAdmin, PageAgentOperations} {
			_, items := projectNavigation(testView(current))
			admin, ok := projectedNavigationItem(items, PageAdmin)
			if !ok {
				t.Fatalf("%s has no Admin navigation", current)
			}
			setup, operations := -1, -1
			for index, child := range admin.Children {
				switch child.Page {
				case PagePersonaAdmin:
					setup = index
				case PageAgentOperations:
					operations = index
				}
			}
			if setup < 0 || operations != setup+1 {
				t.Fatalf("admin pages are not adjacent: %+v", admin.Children)
			}
		}
	})
}

// TestTodo_AGENTUX_019_Browser re-pins the width- and locale-sensitive
// round-1 fixes at component level: the 390px rules, logical-only
// positioning, and the translated agent surfaces in both directions.
func TestTodo_AGENTUX_019_Browser(t *testing.T) {
	css := agentsStylesheet() + agentUXR7Stylesheet()
	if !strings.Contains(css, "@media (max-width:390px){.agents-composer-actions .button,.agents-task-actions .button{flex:1 1 100%;") {
		t.Error("Agents actions do not become full-width at 390px")
	}
	for _, physical := range []string{"left:", "right:", "margin-left", "margin-right", "padding-left", "padding-right"} {
		if strings.Contains(css, physical) {
			t.Errorf("agents styles use physical %q and cannot mirror", physical)
		}
	}
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		task := AgentTask{ID: "done", Goal: "Summarize leave balances", State: AgentTaskCompleted, AnswerText: "You have 12 days left.", AnsweringAgentDisplayName: "Policy Helper"}
		markup := renderAgentUXPage(t, localeID, AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, SelectedTask: &task}})
		if strings.Contains(markup, "⟦") {
			t.Errorf("%s agents surface leaks untranslated copy", localeID)
		}
		if !strings.Contains(markup, `dir="`+string(locale.Direction)+`"`) {
			t.Errorf("%s agents surface lost its direction", localeID)
		}
		persona := agentUXSetup2Persona()
		setup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: locale}, State: PersonaAdminReady, Snapshot: agentUXSetup2Snapshot(persona), Client: &personaAdminTestClient{snapshot: agentUXSetup2Snapshot(persona)}}))
		if strings.Contains(setup, "⟦") {
			t.Errorf("%s setup leaks untranslated copy", localeID)
		}
		ops := renderAgentOperationsTest(t, RenderAgentControls(locale, AgentControlsSnapshot{Available: true, Runs: []AgentControlRun{{ID: "run-1", Name: "Policy Helper", State: "RUNNING"}}}, "done"))
		if strings.Contains(ops, "⟦") || !strings.Contains(ops, `dir="`+string(locale.Direction)+`"`) {
			t.Errorf("%s operations lost translation or direction", localeID)
		}
	}
	for localeID, next := range map[string]string{"de-DE": "Versuchen Sie eine andere Schreibweise.", "ar": "جرّب تهجئة مختلفة."} {
		none := chatTranslations(localeID)[chatui.KeyMentionNone]
		if !strings.Contains(none, "{query}") || !strings.Contains(none, next) {
			t.Errorf("%s mention empty state has no reason or next step: %q", localeID, none)
		}
	}
}
