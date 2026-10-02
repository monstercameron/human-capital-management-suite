package productui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type agentUX022StageError struct{ stage string }

func (e agentUX022StageError) Error() string                      { return "stage " + e.stage }
func (e agentUX022StageError) PersonaCatalogFailureStage() string { return e.stage }

type agentUX022FailingClient struct {
	personaAdminTestClient
	err error
}

func (c *agentUX022FailingClient) Snapshot(context.Context, PersonaAdminSnapshotRequest) (PersonaAdminSnapshot, error) {
	return PersonaAdminSnapshot{}, c.err
}

func agentUX022Regions() map[string]func(*PersonaAdminSnapshot) {
	return map[string]func(*PersonaAdminSnapshot){
		"catalog":   func(s *PersonaAdminSnapshot) { s.CatalogState = PersonaAdminRegionState{Unavailable: true, Omitted: 2} },
		"targets":   func(s *PersonaAdminSnapshot) { s.TargetsState = PersonaAdminRegionState{Unavailable: true} },
		"preview":   func(s *PersonaAdminSnapshot) { s.PreviewState = PersonaAdminRegionState{Unavailable: true, Omitted: 1} },
		"commands":  func(s *PersonaAdminSnapshot) { s.CommandsState = PersonaAdminRegionState{Unavailable: true} },
		"starters":  func(s *PersonaAdminSnapshot) { s.StartersState = PersonaAdminRegionState{Unavailable: true} },
		"documents": func(s *PersonaAdminSnapshot) { s.DocumentsState = PersonaAdminRegionState{Unavailable: true} },
	}
}

// Only an authorization refusal or a missing store takes Agent setup down, and
// the page says which. A read that failed is neither, and can be tried again.
func TestTodo_AGENTUX_022(t *testing.T) {
	view := NewView(PagePersonaAdmin, "ironridge", "ir-001-walt-brennan", "")
	view.EffectivePermissions = []RolePagePermission{{Page: PagePersonaAdmin, View: true}}
	english := ResolveProductLocale("en-US")

	for name, tc := range map[string]struct {
		err      error
		want     string
		contains string
		retry    bool
	}{
		"authorization refused": {agentUX022StageError{"authorization"}, "denied", "This page is for people who manage agents.", false},
		"store missing":         {agentUX022StageError{"persona_store"}, "store", "The agent store is not connected to this workspace.", false},
		"wrapped store missing": {fmt.Errorf("snapshot: %w", agentUX022StageError{"persona_store"}), "store", "The agent store is not connected to this workspace.", false},
		"a failed read":         {agentUX022StageError{"target_rooms"}, "load", "Agent setup could not be loaded just now. Nothing was changed.", true},
		"an unstaged error":     {errors.New("connection reset"), "load", "Agent setup could not be loaded just now. Nothing was changed.", true},
	} {
		if got := agentUX022SnapshotFailure(tc.err); got != tc.want {
			t.Errorf("%s is reported as %q, want %q", name, got, tc.want)
		}
		markup := personaAdminRender(t, BuildPersonaAdminPage(view, &agentUX022FailingClient{err: tc.err}))
		if !strings.Contains(markup, tc.contains) {
			t.Errorf("%s: the page does not say %q: %s", name, tc.contains, agentUX055Text(markup))
		}
		if strings.Contains(markup, `data-persona-admin-retry=`) != tc.retry {
			t.Errorf("%s: retry offered = %t, want %t", name, !tc.retry, tc.retry)
		}
		if tc.want == "load" && strings.Contains(markup, "not connected") {
			t.Errorf("%s: a failed read is blamed on a disconnected service: %s", name, agentUX055Text(markup))
		}
		if strings.Contains(markup, tc.err.Error()) || strings.Contains(markup, "target_rooms") {
			t.Errorf("%s: the page shows the internal error", name)
		}
	}
	if client := (&agentUX022FailingClient{err: agentUX022StageError{"authorization"}}); client != nil {
		markup := personaAdminRender(t, BuildPersonaAdminPage(view, client))
		if strings.Contains(markup, "Policy Helper") {
			t.Fatal("a refused page shows catalog data")
		}
	}
	// With no client at all the store is what is missing.
	if markup := personaAdminRender(t, BuildPersonaAdminPage(view, nil)); !strings.Contains(markup, personaAdminText(english, "persona_store_unavailable")) {
		t.Fatalf("an unbound page does not name the missing store: %s", agentUX055Text(markup))
	}

	// Each region's failure has its own sentence in every language.
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		seen := map[string]bool{}
		for region := range agentUX022Regions() {
			text := personaAdminText(locale, region+"_unavailable")
			if text == "" || text == region+"_unavailable" || seen[text] {
				t.Errorf("%s: region %s has no sentence of its own (%q)", language, region, text)
			}
			seen[text] = true
		}
		if text := agentUX022Text(locale, "load_failed"); text == "" || seen[text] {
			t.Errorf("%s: the whole-page failure has no sentence of its own", language)
		}
	}
}

// Each stage is failed in turn; the other regions still render, the failed one
// says what could not be loaded and offers a retry, and omitted items are counted.
func TestTodo_AGENTUX_022_Fault(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	persona := agentUXSetup2Persona()
	base := agentUXSetup2Snapshot(persona)
	base.StarterCatalogAvailable = true
	base.Starters = []PersonaAdminStarter{{ID: "hcmnext.persona_template.policy_helper", Version: 1, Name: "Policy Helper", Handle: "policy-helper", ManifestID: "m", ChannelClasses: []string{"PRIVATE"}, SkillGrantIDs: []string{"g"}}}
	render := func(snapshot PersonaAdminSnapshot) string {
		return personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: locale}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{snapshot: snapshot}}))
	}
	healthy := render(base)
	if strings.Contains(healthy, `data-persona-admin-region=`) {
		t.Fatalf("a healthy page reports a failed region: %s", agentUX055Text(healthy))
	}
	for region, fail := range agentUX022Regions() {
		t.Run(region, func(t *testing.T) {
			snapshot := base
			fail(&snapshot)
			markup := render(snapshot)
			sentence := personaAdminText(locale, region+"_unavailable")
			if strings.Count(markup, `data-persona-admin-region="`+region+`"`) != 1 || !strings.Contains(markup, sentence) || !strings.Contains(markup, `data-persona-admin-retry="`+region+`"`) {
				t.Fatalf("the %s failure has no sentence and retry of its own: %s", region, agentUX055Text(markup))
			}
			// No other region reports a failure, and the page is still the page.
			if strings.Count(markup, `data-persona-admin-region=`) != 1 {
				t.Fatalf("failing %s took other regions with it: %s", region, agentUX055Text(markup))
			}
			for _, want := range []string{`data-persona-admin-state="ready"`, `id="persona-admin-catalog-title"`, `id="persona-admin-preview-title"`, "Policy Helper", "Walt Brennan"} {
				if !strings.Contains(markup, want) {
					t.Fatalf("failing %s removed %q from the page", region, want)
				}
			}
			if strings.Contains(markup, personaAdminText(locale, "persona_store_unavailable")) || strings.Contains(markup, personaAdminText(locale, "service_unavailable")) {
				t.Fatalf("failing %s is reported as a disconnected service", region)
			}
		})
	}
	// Items that could not be resolved are counted on the region, not shown.
	counted := base
	counted.CatalogState = PersonaAdminRegionState{Unavailable: true, Omitted: 2}
	if markup := render(counted); !strings.Contains(markup, `data-omitted-count="2"`) {
		t.Fatalf("omitted items are not counted: %s", markup)
	}
	// A failed catalog with nothing listed is not presented as "no agents".
	empty := base
	empty.Personas = nil
	empty.CatalogState = PersonaAdminRegionState{Unavailable: true}
	if markup := render(empty); strings.Contains(markup, personaAdminText(locale, "empty")) || !strings.Contains(markup, personaAdminText(locale, "catalog_unavailable")) {
		t.Fatalf("a failed catalog reads as an empty one: %s", agentUX055Text(markup))
	}
}
