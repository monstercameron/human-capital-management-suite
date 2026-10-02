package productui

import (
	"strings"
	"testing"
)

func TestTodo_AGENTUX_004(t *testing.T) {
	props := personaMentionFixture()
	props.People = nil
	props.Agents[0].Handle = "people-guide"
	markup := renderPersonaMention(t, PersonaMentionMenu(props))
	for _, want := range []string{
		`role="listbox"`, `role="option"`, `aria-label="People Guide, @people-guide, Agent"`,
		`class="agent-icon"`, `class="persona-mention-name">People Guide`,
		`class="persona-mention-handle" dir="ltr">@people-guide`, `class="persona-mention-purpose">Answer policy questions`,
		`aria-label="Agent" class="agent-badge">Agent`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("agent mention row missing %q:\n%s", want, markup)
		}
	}
	for key, want := range map[string]PersonaMentionKeyAction{
		"ArrowDown": PersonaMentionKeyNext, "ArrowUp": PersonaMentionKeyPrevious,
		"Enter": PersonaMentionKeySelect, "Tab": PersonaMentionKeySelect, "Escape": PersonaMentionKeyClose,
	} {
		if got := PersonaMentionActionForKey(key); got != want {
			t.Errorf("key %q action = %q, want %q", key, got, want)
		}
	}
	aria := PersonaMentionInputAria("composer", 0, true)
	if aria["expanded"] != "true" || !strings.Contains(aria["keyshortcuts"], "Tab") {
		t.Fatalf("composer combobox contract = %#v", aria)
	}
}

func TestTodo_AGENTUX_004_Browser(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*PersonaMentionMenuProps)
		want   []string
	}{
		{name: "empty", mutate: func(p *PersonaMentionMenuProps) { p.People, p.Agents = nil, nil }, want: []string{"No agents are in this conversation yet. You can still ask on the Agents page.", `href="/workspace/app/chat/agents"`, `role="status"`}},
		{name: "loading", mutate: func(p *PersonaMentionMenuProps) { p.Loading = true }, want: []string{"Loading agents…", `class="persona-mention-state loading"`}},
		{name: "failed", mutate: func(p *PersonaMentionMenuProps) { p.Failed = true }, want: []string{"The agent list could not be loaded.", "Retry loading agents", `data-action="persona-mention-retry"`, `aria-live="assertive"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			props := personaMentionFixture()
			tc.mutate(&props)
			markup := renderPersonaMention(t, PersonaMentionMenu(props))
			for _, want := range tc.want {
				if !strings.Contains(markup, want) {
					t.Errorf("state missing %q:\n%s", want, markup)
				}
			}
			if strings.Contains(markup, ` style=`) || strings.Contains(markup, "#") {
				t.Fatalf("mention state bypassed token classes: %s", markup)
			}
		})
	}
}

func TestTodo_AGENTUX_004_I18n(t *testing.T) {
	for _, tc := range []struct {
		locale, empty, loading, failed, retry, agent, dir string
	}{
		{"en-US", "No agents are in this conversation yet. You can still ask on the Agents page.", "Loading agents…", "The agent list could not be loaded.", "Retry loading agents", "Agent", "ltr"},
		{"de-DE", "In dieser Unterhaltung gibt es noch keine Agenten. Sie können trotzdem auf der Seite „Agenten“ fragen.", "Agenten werden geladen…", "Die Agentenliste konnte nicht geladen werden.", "Agenten erneut laden", "Agent", "ltr"},
		{"ar", "لا يوجد وكلاء في هذه المحادثة حتى الآن. لا يزال بإمكانك طرح سؤالك في صفحة الوكلاء.", "جارٍ تحميل الوكلاء…", "تعذر تحميل قائمة الوكلاء.", "إعادة محاولة تحميل الوكلاء", "وكيل", "rtl"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			locale := ResolveProductLocale(tc.locale)
			for _, state := range []struct {
				props PersonaMentionMenuProps
				want  []string
			}{
				{props: PersonaMentionMenuProps{Locale: locale, Target: "composer"}, want: []string{tc.empty}},
				{props: PersonaMentionMenuProps{Locale: locale, Target: "composer", Loading: true}, want: []string{tc.loading}},
				{props: PersonaMentionMenuProps{Locale: locale, Target: "composer", Failed: true}, want: []string{tc.failed, tc.retry}},
				{props: PersonaMentionMenuProps{Locale: locale, Target: "composer", Agents: []PersonaMentionPersona{{ID: "guide", Name: "People Guide", Invocable: true, InstalledInConversation: true, AudienceIncludesViewer: true}}}, want: []string{`aria-label="` + tc.agent + `"`, `dir="` + tc.dir + `"`}},
			} {
				markup := renderPersonaMention(t, PersonaMentionMenu(state.props))
				for _, want := range state.want {
					if !strings.Contains(markup, want) {
						t.Errorf("localized state missing %q: %s", want, markup)
					}
				}
			}
		})
	}
}

func TestTodo_AGENTUX_018_Mention(t *testing.T) {
	for _, localeName := range []string{"en-US", "de-DE", "ar"} {
		props := personaMentionFixture()
		props.Locale = ResolveProductLocale(localeName)
		props.CanAdminAgents = true
		markup := renderPersonaMention(t, PersonaMentionMenu(props))
		if strings.Contains(strings.ToLower(markup), ">persona") || strings.Contains(markup, "Persona-") {
			t.Fatalf("%s exposed internal persona vocabulary: %s", localeName, markup)
		}
		agents := strings.Index(markup, `class="persona-mention-group">`+personaMentionText(props.Locale, "agents"))
		people := strings.Index(markup, `class="persona-mention-group">`+personaMentionText(props.Locale, "people"))
		if agents < 0 || people < 0 || agents > people {
			t.Fatalf("%s did not put agents first: %s", localeName, markup)
		}
		if !strings.Contains(markup, `dir="ltr">@people-guide`) {
			t.Errorf("%s did not isolate the handle LTR: %s", localeName, markup)
		}
	}

	empty := personaMentionFixture()
	empty.People, empty.Agents, empty.CanAdminAgents = nil, nil, true
	markup := renderPersonaMention(t, PersonaMentionMenu(empty))
	for _, want := range []string{`href="/workspace/app/chat/agents"`, `href="/workspace/app/admin/personas"`, "Add an agent to this conversation"} {
		if !strings.Contains(markup, want) {
			t.Errorf("empty recovery missing %q: %s", want, markup)
		}
	}
	query := personaMentionFixture()
	query.Query = "not-a-match"
	markup = renderPersonaMention(t, PersonaMentionMenu(query))
	if !strings.Contains(markup, "No agents match this search.") || strings.Contains(markup, "No agents are in this conversation yet") {
		t.Fatalf("query used the conversation-empty recovery state: %s", markup)
	}
}
