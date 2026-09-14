package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXSCAN_009(t *testing.T) {
	view := testView(PageSettings)
	view.LogoutHref = "/workspace/logout"
	markup, err := ui.RenderToString(settingsPage(view))
	if err != nil {
		t.Fatal(err)
	}
	account := strings.Index(markup, `data-hcm-setting-group="account-security"`)
	organization := strings.Index(markup, `data-hcm-setting-group="organization-configuration"`)
	preferences := strings.Index(markup, `data-hcm-setting-group="personal-preferences"`)
	if account < 0 || organization <= account || preferences <= organization {
		t.Fatalf("settings groups are not clearly ordered: %s", markup)
	}
	if !strings.Contains(markup[account:organization], `data-hcm-setting-group="signout"`) || strings.Contains(markup[account:organization], `data-hcm-setting-group="tenant-appearance"`) {
		t.Fatal("organization appearance leaked into account security")
	}
	if !strings.Contains(markup[organization:preferences], `data-hcm-setting-group="tenant-appearance"`) || !strings.Contains(markup[organization:preferences], "Changes here affect everyone") {
		t.Fatal("organization settings do not explain their shared scope")
	}

	admin, err := ui.RenderToString(adminPage(testView(PageAdmin)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(admin, "only shows the capabilities available") || !strings.Contains(admin, "what is planned") || !strings.Contains(admin, ">Planned</strong>") {
		t.Fatalf("admin does not distinguish active and planned capabilities: %s", admin)
	}
	if !strings.Contains(admin, `data-action-state="unavailable"`) {
		t.Fatal("planned admin capability acquired a live action")
	}
}

func TestTodo_UXSCAN_009_I18N(t *testing.T) {
	for _, code := range []string{"en-US", "de-DE", "ar"} {
		t.Run(code, func(t *testing.T) {
			locale := ResolveProductLocale(code)
			for _, key := range []string{"settings.organization_group_title", "settings.organization_group_description", "organization_visibility.scope_title", "admin.hero_eyebrow", "admin.hero_description", "admin.planned"} {
				value := locale.Text(key)
				if value == "" || value == key {
					t.Fatalf("%s missing %s", code, key)
				}
			}
			if strings.Contains(locale.Text("organization_visibility.scope_title"), "Who can people discover?") {
				t.Fatal("ambiguous visibility question survived")
			}
			view := ApplyLocale(testView(PageSettings), locale)
			markup, err := ui.RenderToString(settingsPage(view))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(markup, locale.Text("settings.organization_group_title")) {
				t.Fatal("localized organization heading absent from settings")
			}
		})
	}
}

func TestTodo_UXSCAN_009_Regression(t *testing.T) {
	props := SettingsTaskGroupsProps{
		Title: "Account", AccountDescription: "Personal account", PreferencesTitle: "Preferences",
		OrganizationTitle: "Organization", OrganizationDescription: "Shared configuration",
		SettingsLocale: ResolveProductLocale("en-US"),
	}
	markup, err := ui.RenderToString(SettingsTaskGroups(props))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, `data-hcm-setting-group="organization-configuration"`) {
		t.Fatal("organization settings are exposed without an authorized appearance action")
	}
	css := Stylesheet()
	if !strings.Contains(css, `.settings-organization-group .settings-task-card{`) || !strings.Contains(css, `.settings-overview-grid{`) {
		t.Fatal("organization setting card is missing responsive shared styling")
	}
}
