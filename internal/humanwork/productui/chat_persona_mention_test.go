package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderPersonaMention(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func personaMentionFixture() PersonaMentionMenuProps {
	return PersonaMentionMenuProps{
		Locale: ResolveProductLocale("en-US"), Target: "composer", Query: "",
		People: []PersonaMentionPerson{{ID: "person-1", Name: "Camila Morales"}},
		Agents: []PersonaMentionPersona{
			{ID: "people-ops", Name: "People Guide", Handle: "people-guide", Purpose: "Answer policy questions", Owner: "People Operations", Version: "v3", Invocable: true, InstalledInConversation: true, AudienceIncludesViewer: true, Skills: []PersonaMentionSkill{{Name: "Read policy", Tier: "T0"}, {Name: "Draft a private answer", Tier: "T1"}}, DataClasses: []string{"People policy", "Organization data"}, CannotDo: []string{"Change a record", "Approve a request"}, ReplyPlacement: PersonaReplyInThread},
			{ID: "comp-analyst", Name: "Comp Analyst", Purpose: "Hidden compensation access", Invocable: false, InstalledInConversation: true, AudienceIncludesViewer: true},
			{ID: "not-installed", Name: "Not Installed", Purpose: "Not available here", Invocable: true, InstalledInConversation: false, AudienceIncludesViewer: true},
		},
	}
}

func TestTodo_AGENTP_019(t *testing.T) {
	props := personaMentionFixture()
	props.Active = 1
	props.Profile = &props.Agents[0]
	markup := renderPersonaMention(t, PersonaMentionMenu(props))
	for _, want := range []string{
		"People", "Agents", "@Camila Morales", "@people-guide", "Answer policy questions",
		"Purpose: </strong>Answer policy questions", "People Operations", "v3", "Read policy", "Read only", "Private draft",
		"People policy", "Acts with your current access.", "Change a record", "in this thread",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("persona mention surface missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "Comp Analyst") || strings.Contains(markup, "Not Installed") {
		t.Fatalf("ineligible personas appeared in the menu:\n%s", markup)
	}
	if got := NextPersonaMention(0, -1, 2); got != 1 {
		t.Fatalf("previous selection = %d, want 1", got)
	}
	if got := NextPersonaMention(1, 1, 2); got != 0 {
		t.Fatalf("wrapped selection = %d, want 0", got)
	}
}

func TestTodo_AGENTP_019_Browser(t *testing.T) {
	props := personaMentionFixture()
	markup := renderPersonaMention(t, PersonaMentionMenu(props))
	for _, want := range []string{
		`role="listbox"`, `role="option"`, `type="button"`, `data-action="mention-pick"`, `data-kind="agent"`,
		`data-action="persona-profile"`, `aria-label="View agent details: People Guide"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("browser menu missing %q:\n%s", want, markup)
		}
	}
	aria := PersonaMentionInputAria("composer", 1, true)
	if aria["controls"] != "composer-persona-mentions" || aria["activedescendant"] != "composer-persona-mention-2" || aria["keyshortcuts"] == "" {
		t.Fatalf("keyboard bridge aria = %#v", aria)
	}
}

func TestTodo_AGENTP_019_Accessibility(t *testing.T) {
	props := personaMentionFixture()
	props.Profile = &props.Agents[0]
	markup := renderPersonaMention(t, PersonaMentionMenu(props))
	for _, want := range []string{
		`aria-live="polite"`, `aria-labelledby="composer-persona-mention-heading"`, `aria-selected="true"`,
		`role="dialog"`, `aria-labelledby="persona-profile-people-ops-title"`, `aria-label="Close agent details"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("accessibility markup missing %q:\n%s", want, markup)
		}
	}
	if !strings.Contains(markup, `data-active="0"`) {
		t.Fatal("menu did not expose a deterministic active option")
	}
}

func TestTodo_AGENTP_019_I18n(t *testing.T) {
	for _, fixture := range []struct {
		locale string
		want   []string
		dir    string
	}{
		{locale: "en-US", want: []string{"People", "Agents", "Purpose: </strong>Answer policy questions", "Acts with your current access."}, dir: "ltr"},
		{locale: "de-DE", want: []string{"Personen", "Agenten", "Zweck: </strong>Answer policy questions", "Handelt mit Ihrem aktuellen Zugriff."}, dir: "ltr"},
		{locale: "ar", want: []string{"الأشخاص", "الوكلاء", "الغرض: </strong>Answer policy questions", "يعمل الوكيل بصلاحياتك الحالية."}, dir: "rtl"},
	} {
		props := personaMentionFixture()
		props.Locale = ResolveProductLocale(fixture.locale)
		props.Profile = &props.Agents[0]
		markup := renderPersonaMention(t, PersonaMentionMenu(props))
		for _, want := range fixture.want {
			if !strings.Contains(markup, want) {
				t.Errorf("%s missing translated copy %q:\n%s", fixture.locale, want, markup)
			}
		}
		if !strings.Contains(markup, `dir="`+fixture.dir+`"`) {
			t.Errorf("%s did not set dir=%s:\n%s", fixture.locale, fixture.dir, markup)
		}
	}
}

func TestTodo_AGENTP_019_PurposeValueEscapesMarkup(t *testing.T) {
	props := personaMentionFixture()
	persona := props.Agents[0]
	persona.Purpose = `<script>alert("x")</script> & review`
	markup := renderPersonaMention(t, PersonaProfileCard(PersonaProfileCardProps{
		Locale: props.Locale, Context: props.Context, Persona: persona,
	}))
	if strings.Contains(markup, `<script>alert("x")</script>`) || strings.Contains(markup, " & review") {
		t.Fatalf("persona purpose was rendered as raw markup: %s", markup)
	}
	for _, want := range []string{"Purpose: </strong>&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt; &amp; review", "class=\"persona-profile-purpose\""} {
		if !strings.Contains(markup, want) {
			t.Fatalf("escaped persona purpose missing %q: %s", want, markup)
		}
	}
}

func TestTodo_AGENTP_019_Security(t *testing.T) {
	props := personaMentionFixture()
	visible := FilterInvocablePersonas(props.Agents, props.Context)
	if len(visible) != 1 || visible[0].ID != "people-ops" {
		t.Fatalf("filtered menu payload = %+v", visible)
	}
	markup := renderPersonaMention(t, PersonaMentionMenu(props))
	for _, hidden := range []string{"comp-analyst", "Comp Analyst", "Hidden compensation access", "not-installed"} {
		if strings.Contains(markup, hidden) {
			t.Fatalf("hidden persona data leaked into menu markup: %q\n%s", hidden, markup)
		}
	}
	hiddenCard := renderPersonaMention(t, PersonaProfileCard(PersonaProfileCardProps{Locale: props.Locale, Context: props.Context, Persona: props.Agents[1]}))
	if strings.Contains(hiddenCard, "Hidden compensation access") || !strings.Contains(hiddenCard, `hidden`) {
		t.Fatalf("hidden persona profile was not denied:\n%s", hiddenCard)
	}
	oneToOne := PersonaMentionContext{OneToOne: true}
	if got := FilterInvocablePersonas([]PersonaMentionPersona{{ID: "dm-agent", Name: "DM Agent", Invocable: true, AudienceIncludesViewer: true}}, oneToOne); len(got) != 1 {
		t.Fatalf("one-to-one discovery returned %d personas, want 1", len(got))
	}
}

func TestPersonaAgentBadgeFailsClosedWithoutTrustedActor(t *testing.T) {
	missing := renderPersonaMention(t, PersonaAgentBadge(nil))
	if !strings.Contains(missing, "Agent identity unavailable") {
		t.Fatalf("missing actor contract was not announced: %s", missing)
	}
	valid := renderPersonaMention(t, PersonaAgentBadge(&PersonaChatActor{PersonaID: "p", AgentID: "a", InvokerHandle: "dana", Trusted: true}))
	for _, want := range []string{"Agent", "acting for @dana", `aria-label="Agent; acting for @dana"`} {
		if !strings.Contains(valid, want) {
			t.Fatalf("trusted actor badge missing %q: %s", want, valid)
		}
	}
}
