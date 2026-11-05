package productui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestTodo_AGENTP_018_CatalogListsLifecycleFacts proves every GREEN catalog
// fact is visible on each card: lifecycle state, owner, steward, version,
// skills with tiers, derived data classes, audience, installations and limits,
// with cards grouped under lifecycle headings once the catalog grows.
func TestTodo_AGENTP_018_CatalogListsLifecycleFacts(t *testing.T) {
	snapshot := personaAdminSnapshot()
	snapshot.Personas[0].OwnerName = "Priya Nair"
	snapshot.Personas[0].StewardName = "Maya Chen"
	markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{snapshot: snapshot}}))
	for _, want := range []string{
		`data-lifecycle="PUBLISHED"`, `data-agent-version="3"`,
		"Business owner", "Priya Nair",
		"Technical contact", "Maya Chen",
		"Version 3", "Published",
		"Read compensation", "Read-only",
		"Compensation",
		"Who can use it", "Compensation partners",
		"Where it is added", "#hr-ops", "Maya Chen · 1:1",
		"Usage limits", "30 / user / hour",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("catalog card missing %q:\n%s", want, markup)
		}
	}

	grouped := PersonaAdminSnapshot{Available: true, Personas: []PersonaAdminPersona{
		{ID: "draft-a", Name: "Draft A", Lifecycle: PersonaDraft, Version: "1"},
		{ID: "draft-b", Name: "Draft B", Lifecycle: PersonaDraft, Version: "1"},
		{ID: "review-a", Name: "Review A", Lifecycle: PersonaInReview, Version: "2"},
		{ID: "live-a", Name: "Live A", Lifecycle: PersonaPublished, Version: "3"},
		{ID: "paused-a", Name: "Paused A", Lifecycle: PersonaSuspended, Version: "3"},
	}}
	groupedMarkup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: PersonaAdminReady, Snapshot: grouped, Client: &personaAdminTestClient{snapshot: grouped}}))
	for _, lifecycle := range []string{"DRAFT", "IN_REVIEW", "PUBLISHED", "SUSPENDED"} {
		if !strings.Contains(groupedMarkup, `id="persona-admin-lifecycle-`+lifecycle+`"`) {
			t.Errorf("large catalog has no %q lifecycle group:\n%s", lifecycle, groupedMarkup)
		}
	}
}

// TestTodo_AGENTP_018_DesktopLayoutThemeAndDirection asserts the layout half of
// GREEN without a browser: the desktop grid is the default at 1280 px and
// 1920 px (every stacking breakpoint sits below 80rem), colors come only from
// theme tokens so light and dark both hold, and direction follows the locale.
func TestTodo_AGENTP_018_DesktopLayoutThemeAndDirection(t *testing.T) {
	css := personaAdminStylesheet()
	for _, want := range []string{
		`.persona-admin-preview-controls{display:grid;grid-template-columns:repeat(3,minmax(0,1fr))`,
		`.persona-admin-page .persona-admin-facts{display:grid;grid-template-columns:repeat(2,minmax(0,1fr))`,
		`.persona-admin-page .persona-admin-editor-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr))`,
		`var(--hcm-color-surface)`,
		`var(--hcm-color-text)`,
		`var(--hcm-color-border)`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("persona stylesheet missing desktop rule %q", want)
		}
	}
	breakpoints := regexp.MustCompile(`@media\(max-width:([0-9.]+)rem\)`).FindAllStringSubmatch(css, -1)
	if len(breakpoints) == 0 {
		t.Fatal("persona stylesheet has no stacking breakpoints")
	}
	for _, match := range breakpoints {
		width, err := strconv.ParseFloat(match[1], 64)
		if err != nil || width >= 80 {
			t.Errorf("breakpoint %qrem reaches the 1280 px desktop layout", match[1])
		}
	}
	if fixed := regexp.MustCompile(`#[0-9a-fA-F]{3}\b|rgba?\(|hsla?\(`).FindString(css); fixed != "" {
		t.Errorf("persona stylesheet pins a fixed color %q instead of a theme token", fixed)
	}

	for _, tc := range []struct{ locale, dir, lang string }{
		{"en-US", "ltr", "en-US"}, {"de-DE", "ltr", "de-DE"}, {"ar", "rtl", "ar"},
	} {
		markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale(tc.locale)}, State: PersonaAdminReady, Snapshot: personaAdminSnapshot(), Client: &personaAdminTestClient{}}))
		if !strings.Contains(markup, `dir="`+tc.dir+`"`) {
			t.Errorf("%s page did not set dir=%q:\n%s", tc.locale, tc.dir, markup)
		}
	}
}

// TestTodo_AGENTP_018_LifecycleControlsExecuteOrExplain proves publish,
// suspend and retire render as working command controls once the AGENTP-006
// review step passes, rollback links to the rollout tab with its agent and
// version, and every control names its unlock when the permission is missing.
func TestTodo_AGENTP_018_LifecycleControlsExecuteOrExplain(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	ready := PersonaAdminSnapshot{Available: true, Personas: []PersonaAdminPersona{{
		ID: "policy-helper", Name: "Policy Helper", Lifecycle: PersonaInReview, Version: "4",
		ReviewRequired: true, ReviewApproved: true, EvaluationRef: "eval-4",
		VersionHistory: []PersonaAdminVersionHistory{{Version: "4", Lifecycle: PersonaInReview}, {Version: "3", Lifecycle: PersonaPublished}},
	}}}
	markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: locale}, State: PersonaAdminReady, Snapshot: ready, Client: &personaAdminTestClient{snapshot: ready}}))
	for _, want := range []string{
		`data-persona-command="PUBLISH"`, "Publish version 4",
		`tab=rollout`, `agent=policy-helper`, `version=3`,
		`data-persona-command="RETIRE"`, "Confirm retirement",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("reviewed agent missing lifecycle control %q:\n%s", want, markup)
		}
	}
	if !strings.Contains(markup, Path(PageAgentOperations)) {
		t.Errorf("rollback did not target the operations rollout tab:\n%s", markup)
	}

	live := PersonaAdminSnapshot{Available: true, Personas: []PersonaAdminPersona{{
		ID: "comp-analyst", Name: "Comp Analyst", Lifecycle: PersonaPublished, Version: "3",
		VersionHistory: []PersonaAdminVersionHistory{{Version: "3", Lifecycle: PersonaPublished}, {Version: "2", Lifecycle: PersonaPublished}},
	}}}
	liveMarkup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: locale}, State: PersonaAdminReady, Snapshot: live, Client: &personaAdminTestClient{snapshot: live}}))
	if !strings.Contains(liveMarkup, `data-persona-command="SUSPEND"`) || !strings.Contains(liveMarkup, "Pause agent") {
		t.Errorf("published agent cannot suspend:\n%s", liveMarkup)
	}

	denied := ready
	denied.CommandPermissionsAvailable = true
	deniedMarkup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: locale}, State: PersonaAdminReady, Snapshot: denied, Client: &personaAdminTestClient{snapshot: denied}}))
	for _, want := range []string{"Publishing requires the Agent publisher role", "disabled"} {
		if !strings.Contains(deniedMarkup, want) {
			t.Errorf("unpermitted publish was not visibly disabled with its unlock %q:\n%s", want, deniedMarkup)
		}
	}
}
