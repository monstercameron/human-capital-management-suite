package productui

import (
	"strings"
	"testing"
)

// #3610: with no template and no agent, the setup notice must not tell the
// administrator to edit agents that do not exist or to choose Edit on a card.
// With an agent and no template, editing stays on offer.
func TestIssue3610_NoTemplateNoticeAgreesWithTheAgentsThatExist(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		t.Run(localeID, func(t *testing.T) {
			locale := ResolveProductLocale(localeID)
			render := func(snapshot PersonaAdminSnapshot) string {
				return personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: locale}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{snapshot: snapshot}}))
			}
			editClaim := personaAdminR5SetupText(locale, "no_template")
			editHow := personaAdminR5SetupText(locale, "no_template_next")

			none := render(PersonaAdminSnapshot{Available: true})
			if strings.Contains(none, editClaim) || strings.Contains(none, editHow) {
				t.Fatalf("zero agents: the notice still tells the reader to edit agents below: %s", none)
			}
			explained := personaAdminR5SetupText(locale, "no_template_no_agents")
			if explained == "" || explained == "no_template_no_agents" || !strings.Contains(none, explained) {
				t.Fatalf("zero agents: the notice does not explain that none can be created: %s", none)
			}
			if strings.Contains(none, "data-persona-new-agent") {
				t.Fatal("zero templates: a New agent control is rendered")
			}

			existing := render(PersonaAdminSnapshot{Available: true, Personas: []PersonaAdminPersona{{ID: "policy-helper", Name: "Policy Helper", Lifecycle: PersonaPublished}}})
			if !strings.Contains(existing, editClaim) {
				t.Fatalf("one agent, no template: editing guidance is gone: %s", existing)
			}
			if strings.Contains(existing, explained) {
				t.Fatal("one agent: the no-agents explanation is shown")
			}
		})
	}
}

// #3610: the no-agents notice keeps the one real next step: a named person
// who looks after agents can be messaged, unless the viewer is that person.
func TestIssue3610_NoAgentsNoticeKeepsTheNamedContact(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	options := []PersonaAdminTarget{{ID: "ir-admin", Label: "Alex Morgan", Role: "platform_administrator"}}
	named := personaAdminRender(t, personaAdminNoAgentsNotice(locale, options, "someone-else"))
	if !strings.Contains(named, "Message Alex Morgan") || !strings.Contains(named, personaAdminR5SetupText(locale, "no_template_no_agents")) || strings.Contains(named, "choose Edit on its card") {
		t.Fatalf("named contact missing or notice contradicts itself: %s", named)
	}
	self := personaAdminRender(t, personaAdminNoAgentsNotice(locale, options, "ir-admin"))
	if strings.Contains(self, "Message ") || strings.Contains(self, "<a ") {
		t.Fatalf("the viewer is offered a message to themselves: %s", self)
	}
}
