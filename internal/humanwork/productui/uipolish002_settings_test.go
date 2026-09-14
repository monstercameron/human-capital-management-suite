package productui

import (
	"strings"
	"testing"
)

func TestTodo_UIPOLISH_002_SettingsSingleGroupUsesFullMeasure(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`.settings-page-stack .settings-overview-grid:has(>.settings-account-group:only-child){grid-template-columns:minmax(0,1fr);}`,
		`.settings-account-group .settings-group-content{`,
		`grid-template-columns:repeat(auto-fit,minmax(min(100%,22rem),1fr))`,
		`align-items:start`,
		`gap:calc(var(--hcm-space-3) * var(--hcm-density))`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("settings rhythm rule missing %q", want)
		}
	}
	doc, err := Render(testView(PageSettings))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `class="settings-group settings-account-group"`) || !strings.Contains(doc, `class="settings-group-content"`) {
		t.Fatal("production settings lost its account-group composition")
	}
	for _, want := range []string{`<h3 id="settings-access-title"`, `<h3 id="locale-preferences-title"`, `<h3 id="accessibility-title"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("settings subsection heading hierarchy missing %q", want)
		}
	}
}

func TestTodo_UXAUDIT_023_Regression_SettingsCardsShareIntentionalGeometry(t *testing.T) {
	view := testView(PageSettings)
	view.LogoutHref = "/workspace/logout"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-hcm-setting-group="tenant-appearance"`, `data-hcm-setting-group="account-security"`,
		`data-hcm-setting-group="signout"`, `data-hcm-setting-group="notifications"`,
		`data-hcm-setting-group="navigation"`, `class="accessibility-group accessibility-group-contrast"`,
		`class="accessibility-group accessibility-group-links"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("Settings task card or accessible choice group missing %q", want)
		}
	}
	css := Stylesheet()
	for _, want := range []string{
		`.settings-account-group .settings-group-content>:is(.settings-context,.settings-task-card,.settings-signout){`,
		`padding:calc(var(--hcm-space-3) * var(--hcm-density))`,
		`.settings-account-group .settings-group-content>.settings-signout{grid-column:1 / -1;}`,
		`.settings-preferences-group>.settings-group-content{display:grid;gap:calc(var(--hcm-space-3) * var(--hcm-density));grid-template-columns:minmax(0,1fr);`,
		`.settings-preferences-group>.settings-group-content>.settings-task-card{`,
		`.settings-preferences-group .locale-choice-list{grid-template-columns:repeat(3,minmax(0,1fr));`,
		`@media (max-width:600px){.settings-preferences-group .locale-choice-list{grid-template-columns:minmax(0,1fr);}}`,
		`.settings-preferences-group :is(.accessibility-group-contrast,.accessibility-group-links) .accessibility-options{grid-template-columns:repeat(2,minmax(0,1fr));`,
		`@media (max-width:760px){.settings-preferences-group :is(.accessibility-group-contrast,.accessibility-group-links) .accessibility-options{grid-template-columns:minmax(0,1fr);}}`,
		`min-height:44px`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("Settings card geometry rule missing %q", want)
		}
	}
}
