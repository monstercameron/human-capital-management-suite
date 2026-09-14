package productui

import (
	"strings"
	"testing"
)

func TestUXBlind016HelpExplainsStagesAndEmptyRoleSelectorRecovery(t *testing.T) {
	doc, err := Render(testView(PageHelp))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Choose an employee", "Review promotion requests", "Review past decisions", "empty promotion role selector", "job ladder and target pay band", "Changing access roles does not create a promotion path"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("help guidance missing %q", want)
		}
	}
	if strings.Contains(doc, "Understand journey stages") {
		t.Fatal("help still promises stage guidance through the work inbox")
	}
}

func TestHelpGuidanceMatchesRoleAccessAcrossLocales(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, create := range []bool{false, true} {
			view := testView(PageHelp)
			view.Locale = ResolveProductLocale(locale)
			view.EffectivePermissions = []RolePagePermission{
				{Page: PageHelp, View: true}, {Page: PageMyself, View: true},
				{Page: PageSettings, View: true}, {Page: PageJourneys, View: create, Create: create},
			}
			doc, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"help.tasks", "help.my_profile", "help.account_settings"} {
				if !strings.Contains(doc, view.Locale.Text(key)) {
					t.Fatalf("%s: missing available task %s", locale, key)
				}
			}
			if strings.Contains(doc, view.Locale.Text("help.choose_employee")) || strings.Contains(doc, view.Locale.Text("help.organization")) {
				t.Fatal("help linked to a denied page")
			}
			if strings.Contains(doc, view.Locale.Text("help.promotion_title")) != create || strings.Contains(doc, view.Locale.Text("help.access_title")) == create {
				t.Fatalf("%s: guidance did not match create permission %v", locale, create)
			}
		}
	}
}

func TestUXBlind017InsightsUsesBusinessSummaryLanguage(t *testing.T) {
	doc, err := Render(testView(PageInsights))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Visible workflows", "Completed or closed", "summary covers promotion journeys you can view", "Broader workforce reporting is not available"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("insights copy missing %q", want)
		}
	}
	if strings.Contains(doc, ">Terminal<") || strings.Contains(doc, "analytics capability") {
		t.Fatal("insights exposes technical or capability-internal wording")
	}
}

func TestUXBlind022StudioNamesOnlyUnavailablePageBuilder(t *testing.T) {
	doc, err := Render(testView(PageStudio))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Custom pages cannot be edited here yet", "Brand &amp; appearance", "Roles &amp; access"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("studio copy missing %q", want)
		}
	}
	if strings.Contains(doc, "does not expose a governed page, brand, navigation, access-policy, or publication service") {
		t.Fatal("studio still makes a broad availability claim")
	}
}
