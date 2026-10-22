package productui

import (
	"strings"
	"testing"
)

// TestTodo_AGENTUX_022_Browser pins the browser-visible contract of a
// partially failed Agent setup page: each failed region keeps its sentence,
// a named keyboard-operable retry and its region marker in every locale,
// while the page stays in its ready frame with the other regions intact.
func TestTodo_AGENTUX_022_Browser(t *testing.T) {
	persona := agentUXSetup2Persona()
	base := agentUXSetup2Snapshot(persona)
	base.StarterCatalogAvailable = true
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		for region, fail := range agentUX022Regions() {
			snapshot := base
			fail(&snapshot)
			markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: locale}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{snapshot: snapshot}}))
			sentence := personaAdminText(locale, region+"_unavailable")
			if !strings.Contains(markup, `data-persona-admin-region="`+region+`"`) || !strings.Contains(markup, sentence) {
				t.Errorf("%s %s: failure sentence or region marker missing", language, region)
			}
			retry := personaAdminText(locale, "retry")
			if retry == "" || retry == "retry" {
				t.Errorf("%s: retry has no localized name", language)
			}
			if !strings.Contains(markup, `data-persona-admin-retry="`+region+`"`) || !strings.Contains(markup, ">"+retry+"<") || !strings.Contains(markup, "<button") {
				t.Errorf("%s %s: retry is not a named button", language, region)
			}
			if !strings.Contains(markup, `data-persona-admin-state="ready"`) || !strings.Contains(markup, `id="persona-admin-catalog-title"`) {
				t.Errorf("%s %s: page left its ready frame", language, region)
			}
			if strings.Count(markup, `data-persona-admin-region=`) != 1 {
				t.Errorf("%s %s: other regions report failure too", language, region)
			}
		}
	}
}
