package productui

import (
	"strings"
	"testing"
)

// TestTodo_AGENTUX_056_Browser renders the general-purpose agent's setup card in
// each language and each state of its workspace index: indexed, behind, absent
// and nothing to search. Only the Assistant's card carries the line.
func TestTodo_AGENTUX_056_Browser(t *testing.T) {
	base := PersonaAdminPersona{ID: agentux056AssistantPersonaID, Name: "Assistant", Version: "1", Handle: "assistant", Purpose: "Answers general questions.", Lifecycle: PersonaDraft, Instructions: "Be brief."}
	for _, localeName := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeName)
		cases := map[string]struct {
			persona PersonaAdminPersona
			want    string
		}{
			"indexed": {withWorkspaceIndex(base, 663, "2026-10-01T18:30:00Z", 0), agentux056Text(locale, "indexed")},
			"behind":  {withWorkspaceIndex(base, 663, "2026-10-01T18:30:00Z", 12), agentux056Text(locale, "behind")},
			"absent":  {withWorkspaceIndex(base, 663, "", 663), agentux056Text(locale, "absent")},
			"none":    {withWorkspaceIndex(base, 0, "", 0), agentux056Text(locale, "none")},
		}
		for name, tc := range cases {
			markup := personaAdminRender(t, personaAdminCard(locale, &personaAdminTestClient{}, tc.persona, PersonaAdminSnapshot{}))
			if !strings.Contains(markup, `data-workspace-search="true"`) {
				t.Fatalf("%s/%s: the Assistant's card has no workspace search line: %s", localeName, name, markup)
			}
			// The sentence is checked on its fixed words, which sit on either side of the numbers.
			words := strings.Fields(strings.NewReplacer("{count}", " ", "{pending}", " ", "{time}", " ").Replace(tc.want))
			if len(words) == 0 || !strings.Contains(markup, words[0]) {
				t.Fatalf("%s/%s: the line does not say %q: %s", localeName, name, tc.want, markup)
			}
			if strings.Contains(markup, "{count}") || strings.Contains(markup, "{pending}") || strings.Contains(markup, "{time}") {
				t.Fatalf("%s/%s: a placeholder was printed: %s", localeName, name, markup)
			}
		}
		other := base
		other.ID, other.Name, other.Handle = "policy-helper", "Policy Helper", "policy-helper"
		if strings.Contains(personaAdminRender(t, personaAdminCard(locale, &personaAdminTestClient{}, other, PersonaAdminSnapshot{})), `data-workspace-search`) {
			t.Fatalf("%s: another agent's card shows the workspace search line", localeName)
		}
	}
	en := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, withWorkspaceIndex(base, 663, "2026-10-01T18:30:00Z", 0), PersonaAdminSnapshot{}))
	if !strings.Contains(en, "Searches 663 workspace documents (indexed ") || !strings.Contains(en, `datetime="2026-10-01T18:30:00Z"`) {
		t.Fatalf("the indexed line is wrong: %s", en)
	}
}

func withWorkspaceIndex(persona PersonaAdminPersona, documents int, indexedAt string, pending int) PersonaAdminPersona {
	persona.WorkspaceDocuments, persona.WorkspaceIndexedAt, persona.WorkspacePending = documents, indexedAt, pending
	return persona
}

// TestTodo_AGENTUX_049_Browser renders Agent setup with the two reviewed starters
// the product ships: "New agent" is a real action that offers Assistant beside
// Policy Helper, in each language, and the dead end "No template is installed"
// is not shown.
func TestTodo_AGENTUX_049_Browser(t *testing.T) {
	snapshot := PersonaAdminSnapshot{Available: true, StarterCatalogAvailable: true, Starters: []PersonaAdminStarter{
		{ID: "hcmnext.persona_template.policy_helper", Version: 1, Name: "Policy Helper", Handle: "policy-helper", Purpose: "Answers policy questions.", ManifestID: "manifest-policy", SkillGrantIDs: []string{"chat.reply"}, ChannelClasses: []string{"ANY_INTERNAL"}},
		{ID: "hcmnext.persona_template.assistant", Version: 1, Name: "Assistant", Handle: "assistant", Purpose: "Answers general questions from documents.", ManifestID: "manifest-assistant", SkillGrantIDs: []string{"chat.reply"}, ChannelClasses: []string{"ANY_INTERNAL"}},
	}}
	for _, localeName := range []string{"en-US", "de-DE", "ar"} {
		markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale(localeName)}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{}}))
		if !strings.Contains(markup, `data-persona-new-agent=`) || strings.Contains(markup, `id="persona-admin-new-agent-note"`) {
			t.Fatalf("%s: New agent is not offered with the shipped starters: %s", localeName, markup)
		}
		if strings.Contains(markup, "No template is installed") {
			t.Fatalf("%s: the dead end is still shown", localeName)
		}
	}
	en := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{}}))
	if !strings.Contains(en, "Assistant") {
		t.Fatalf("Assistant is not offered by name: %s", en)
	}
}
