package productui

import (
	"regexp"
	"strings"
	"testing"
)

func TestTodo_AGENTP_018_PersonaEditorAccessibilityAndLocalization(t *testing.T) {
	tests := []struct {
		name, locale, title, direction string
	}{
		{name: "English", locale: "en-US", title: "Create a persona draft", direction: "ltr"},
		{name: "German", locale: "de-DE", title: "Persona-Entwurf erstellen", direction: "ltr"},
		{name: "Arabic RTL", locale: "ar", title: "إنشاء مسودة شخصية", direction: "rtl"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			markup := personaAdminRender(t, PersonaAdminEditor(ResolveProductLocale(tc.locale), &personaAdminTestClient{}, personaAdminEditorReadySnapshot()))
			for _, want := range []string{tc.title, `aria-live="polite"`, `aria-describedby="persona-admin-editor-help persona-admin-editor-status"`, `name="persona_id"`, `name="business_owner_id"`, `name="technical_steward_id"`, `name="manifest_id"`, `name="avatar_ref"`, `name="organization_scopes"`, `readonly`, `data-starter-instructions="server-owned"`} {
				if !strings.Contains(markup, want) {
					t.Errorf("%s editor missing %q", tc.locale, want)
				}
			}
			if strings.Contains(markup, `name="instructions"`) || strings.Contains(markup, `name="allowed_channels"`) {
				t.Fatal("starter-owned instructions or authority bounds are submitted as user-asserted values")
			}
			for _, id := range []string{"persona-admin-starter", "persona-admin-id", "persona-admin-handle", "persona-admin-name", "persona-admin-purpose-input", "persona-admin-owner", "persona-admin-steward", "persona-admin-manifest", "persona-admin-avatar", "persona-admin-scopes", "persona-admin-instructions", "persona-admin-channel-private", "persona-admin-channel-public", "persona-admin-channel-external"} {
				if !strings.Contains(markup, `for="`+id+`"`) || !strings.Contains(markup, `id="`+id+`"`) {
					t.Errorf("control %q lacks an associated accessible label", id)
				}
			}
			if !strings.Contains(markup, `data-starter-channels="PRIVATE,PUBLIC"`) || !strings.Contains(markup, `data-starter-manifest="manifest-policy-v1"`) {
				t.Errorf("server-approved starter bounds are absent: %s", markup)
			}
		})
	}
}

func TestTodo_AGENTP_018_PersonaEditorUnavailableDoesNotOfferSubmit(t *testing.T) {
	markup := personaAdminRender(t, PersonaAdminEditor(ResolveProductLocale("en-US"), &personaAdminTestClient{}, PersonaAdminSnapshot{StarterCatalogAvailable: true}))
	if !strings.Contains(markup, `id="persona-admin-create-submit"`) || !strings.Contains(markup, `disabled`) {
		t.Fatalf("empty starter catalog must keep create disabled: %s", markup)
	}
	if !strings.Contains(markup, "No starter has a registered manifest and active skill grants yet") || strings.Contains(markup, `value="hcmnext.persona_template.`) {
		t.Fatalf("empty catalog must explain provisioning state and omit hardcoded templates: %s", markup)
	}
}

func TestTodo_AGENTP_018_LocalDraftSetupRequiresServerAvailability(t *testing.T) {
	for _, available := range []bool{false, true} {
		snapshot := PersonaAdminSnapshot{StarterCatalogAvailable: true, LocalDevBootstrapAvailable: available}
		markup := personaAdminRender(t, PersonaAdminEditor(ResolveProductLocale("en-US"), &personaAdminTestClient{}, snapshot))
		if strings.Contains(markup, `data-persona-local-setup`) != available {
			t.Fatalf("setup action availability = %v, markup: %s", available, markup)
		}
	}
	snapshot := personaAdminEditorReadySnapshot()
	snapshot.LocalDevBootstrapAvailable = true
	markup := personaAdminRender(t, PersonaAdminEditor(ResolveProductLocale("en-US"), &personaAdminTestClient{}, snapshot))
	if strings.Contains(markup, `data-persona-local-setup`) {
		t.Fatal("ready starter should offer draft creation rather than setup")
	}
}

func TestTodo_AGENTP_018_ExistingDraftKeepsConfigurationAvailable(t *testing.T) {
	snapshot := PersonaAdminSnapshot{Available: true, LocalDevBootstrapAvailable: true, Personas: []PersonaAdminPersona{{ID: "existing"}}}
	markup := personaAdminRender(t, PersonaAdminEditor(ResolveProductLocale("en-US"), &personaAdminTestClient{}, snapshot))
	if !strings.Contains(markup, "Configure existing personas in the catalog") || !strings.Contains(markup, `id="persona-admin-editor-status"`) {
		t.Fatal("existing persona configuration lacks explanation and command feedback")
	}
	if strings.Contains(markup, `data-persona-local-setup`) || strings.Contains(markup, `id="persona-admin-create-form"`) {
		t.Fatal("already configured draft offered duplicate setup or an unusable form")
	}
}

func TestTodo_AGENTP_018_CommandFeedbackAndSelectionSurviveRender(t *testing.T) {
	snapshot := personaAdminSnapshot()
	snapshot.CommandStatus = "success"
	snapshot.PreviewPersonaID = snapshot.Personas[0].ID
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		resolved := ResolveProductLocale(locale)
		markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: resolved}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{}}))
		if !strings.Contains(markup, PersonaAdminCommandStatusText(resolved, "success")) || !strings.Contains(markup, `id="persona-admin-preview-persona"`) || !strings.Contains(markup, personaAdminText(resolved, "choose_persona")) {
			t.Fatalf("%s dropped command feedback or persona selector", locale)
		}
	}
}

func TestTodo_AGENTP_018_PreviewTargetsRemainSelectedWhenUnavailable(t *testing.T) {
	snapshot := PersonaAdminSnapshot{
		SubjectOptions:   []PersonaAdminTarget{{ID: "first-user", Label: "First"}, {ID: "chosen-user", Label: "Chosen"}},
		Conversations:    []PersonaAdminTarget{{ID: "first-room", Label: "First"}, {ID: "chosen-room", Label: "Chosen"}},
		PreviewSubjectID: "chosen-user", PreviewConversationID: "chosen-room", PreviewUnavailable: true,
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		resolved := ResolveProductLocale(locale)
		markup := personaAdminRender(t, personaAdminPreview(resolved, &personaAdminTestClient{}, snapshot))
		for _, id := range []string{"chosen-user", "chosen-room"} {
			if !regexp.MustCompile(`<option[^>]*selected[^>]*value="` + id + `"|<option[^>]*value="` + id + `"[^>]*selected`).MatchString(markup) {
				t.Fatalf("%s lost selected target %s: %s", locale, id, markup)
			}
		}
		if !strings.Contains(markup, personaAdminText(resolved, "preview_unavailable")) || strings.Contains(markup, personaAdminText(resolved, "no_preview_warnings")) {
			t.Fatalf("%s concealed preview failure: %s", locale, markup)
		}
	}
}

func TestTodo_AGENTP_018_CommandPermissionsDisableVersionEditor(t *testing.T) {
	snapshot := personaAdminSnapshot()
	snapshot.CommandPermissionsAvailable = true
	snapshot.AllowedCommands = []string{"REVIEW"}
	snapshot.Personas[0].StarterID = "policy-helper"
	snapshot.Personas[0].StarterVersion = 1
	snapshot.Personas[0].ChannelClasses = []string{"PRIVATE"}
	markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{}}))
	if !regexp.MustCompile(`<fieldset[^>]*\bdisabled[ >]`).MatchString(markup) || personaAdminCommandAllowed(snapshot, "RETIRE") || !personaAdminCommandAllowed(snapshot, "REVIEW") {
		t.Fatal("reviewer was offered configuration or retirement")
	}
}

func TestTodo_AGENTP_018_PersonaStarterReadinessFailsClosed(t *testing.T) {
	base := personaAdminEditorReadySnapshot()
	cases := []struct {
		name     string
		mutate   func(*PersonaAdminSnapshot)
		wantText string
	}{
		{name: "source unavailable", mutate: func(snapshot *PersonaAdminSnapshot) { snapshot.StarterCatalogAvailable = false }, wantText: "manifest and skill grant catalog is not connected"},
		{name: "no registered starters", mutate: func(snapshot *PersonaAdminSnapshot) { snapshot.Starters = nil }, wantText: "No starter has a registered manifest"},
		{name: "missing active grant", mutate: func(snapshot *PersonaAdminSnapshot) { snapshot.Starters[0].SkillGrantIDs = nil }, wantText: "No starter has a registered manifest"},
		{name: "missing manifest", mutate: func(snapshot *PersonaAdminSnapshot) { snapshot.Starters[0].ManifestID = " " }, wantText: "No starter has a registered manifest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := base
			snapshot.Starters = append([]PersonaAdminStarter(nil), base.Starters...)
			snapshot.Starters[0].SkillGrantIDs = append([]string(nil), base.Starters[0].SkillGrantIDs...)
			snapshot.Starters[0].ChannelClasses = append([]string(nil), base.Starters[0].ChannelClasses...)
			tc.mutate(&snapshot)
			markup := personaAdminRender(t, PersonaAdminEditor(ResolveProductLocale("en-US"), &personaAdminTestClient{}, snapshot))
			if !strings.Contains(markup, tc.wantText) || !strings.Contains(markup, `id="persona-admin-create-submit"`) || !strings.Contains(markup, `disabled`) {
				t.Fatalf("unready starter state must explain why creation is disabled: %s", markup)
			}
		})
	}
}

func TestTodo_AGENTP_018_PersonaCommandStatusTextIsActionableAndLocalized(t *testing.T) {
	cases := []struct{ code, locale, want string }{
		{"invalid", "en-US", "registered manifest and active skill grants"},
		{"forbidden", "en-US", "do not have permission"},
		{"unavailable", "en-US", "No change was confirmed"},
		{"conflict", "de-DE", "Aktualisieren Sie die Seite"},
		{"invalid", "ar", "بيان مسجل"},
		{"backend-secret", "en-US", "The server could not complete this command"},
	}
	for _, tc := range cases {
		t.Run(tc.code+"/"+tc.locale, func(t *testing.T) {
			got := PersonaAdminCommandStatusText(ResolveProductLocale(tc.locale), tc.code)
			if !strings.Contains(got, tc.want) {
				t.Errorf("status text %q does not contain actionable message %q", got, tc.want)
			}
			if strings.Contains(got, "backend-secret") {
				t.Fatal("backend error detail leaked into visible status")
			}
		})
	}
}

func personaAdminEditorReadySnapshot() PersonaAdminSnapshot {
	return PersonaAdminSnapshot{Available: true, StarterCatalogAvailable: true, Starters: []PersonaAdminStarter{{
		ID: "hcmnext.persona_template.policy_helper", Version: 1, Name: "Policy Helper", Handle: "policy-helper",
		Purpose: "Answer tenant policy questions with citations.", ManifestID: "manifest-policy-v1",
		ChannelClasses: []string{"PRIVATE", "PUBLIC"}, SkillGrantIDs: []string{"policy.read"},
	}}}
}

func TestTodo_AGENTP_018_PersonaAdminEditorLayoutIsResponsiveAndScoped(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{
		I18nProps: I18nProps{Locale: locale}, State: PersonaAdminReady,
		Snapshot: PersonaAdminSnapshot{Available: true}, Client: &personaAdminTestClient{},
	}))
	for _, want := range []string{`persona-admin-editor"`, `class="persona-admin-editor-grid"`, `id="persona-admin-name"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("rendered persona administration page missing %q", want)
		}
	}
	css := Stylesheet()
	for _, want := range []string{
		`.persona-admin-page .persona-admin-editor{order:1;`,
		`.persona-admin-page .persona-admin-editor-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));`,
		`.persona-admin-page .persona-admin-editor-field input,`,
		`@media(max-width:48rem){.persona-admin-page{`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("product stylesheet missing responsive persona editor rule %q", want)
		}
	}
}

func TestTodo_AGENTP_018_PersonaVersionEditorUsesCurrentBoundaries(t *testing.T) {
	persona := PersonaAdminPersona{
		ID: "policy-helper", StarterID: "hcmnext.persona_template.policy_helper", StarterVersion: 1,
		Handle: "policy-helper", Name: "Policy Helper", Purpose: "Answer tenant policies with citations.",
		ChannelClasses: []string{"PRIVATE", "PUBLIC"},
	}
	markup := personaAdminRender(t, personaAdminVersionEditor(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona))
	for _, want := range []string{`data-persona-admin-command-form="CREATE_VERSION"`, `name="starter_id"`, `name="starter_version"`, `name="handle"`, `name="display_name"`, `name="purpose"`, `name="allowed_channels"`, `value="PRIVATE"`, `value="PUBLIC"`, `readonly="readonly"`, "Create version"} {
		if !strings.Contains(markup, want) {
			t.Errorf("version editor missing %q", want)
		}
	}
	if strings.Contains(markup, `value="EXTERNAL"`) || strings.Contains(markup, `name="instructions"`) {
		t.Fatalf("version form widened channels or accepted instructions: %s", markup)
	}
	for _, locale := range []string{"de-DE", "ar"} {
		localized := personaAdminRender(t, personaAdminVersionEditor(ResolveProductLocale(locale), &personaAdminTestClient{}, persona))
		if strings.Contains(localized, "persona_admin.") || strings.Contains(localized, "⟦") {
			t.Errorf("%s version editor is missing localization: %s", locale, localized)
		}
	}
}
