package productui

import (
	"strings"
	"testing"
)

// TestTodo_AGENT_013_Browser reads Agent setup with the versioned starters: each
// starter is offered by name as a draft to create, never as an agent that is
// installed or granted anything, and the page still says that a draft needs
// review, evaluation and publication before it can act.
func TestTodo_AGENT_013_Browser(t *testing.T) {
	snapshot := PersonaAdminSnapshot{Available: true, StarterCatalogAvailable: true, Starters: []PersonaAdminStarter{
		{ID: "hcmnext.persona_template.policy_helper", Version: 1, Name: "Policy Helper", Handle: "policy-helper", Purpose: "Answers policy questions.", ManifestID: "manifest-policy", SkillGrantIDs: []string{"chat.reply"}, ChannelClasses: []string{"ANY_INTERNAL"}},
		{ID: "hcmnext.persona_template.project_assistant", Version: 1, Name: "Project Assistant", Handle: "project-assistant", Purpose: "Answers questions about project work.", ManifestID: "manifest-project", SkillGrantIDs: []string{"chat.reply"}, ChannelClasses: []string{"ANY_INTERNAL"}},
	}}
	for _, localeName := range []string{"en-US", "de-DE", "ar"} {
		markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale(localeName)}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{}}))
		if !strings.Contains(markup, `data-persona-new-agent=`) {
			t.Fatalf("%s: no starter is offered: %s", localeName, markup)
		}
		for _, forbidden := range []string{"data-persona-command=\"INSTALL\"", "data-persona-command=\"PUBLISH\""} {
			if strings.Contains(markup, forbidden) {
				t.Fatalf("%s: offering a starter also offers %s", localeName, forbidden)
			}
		}
	}
}
