package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestSettingsLocaleSwitcherPreservesNavigationState(t *testing.T) {
	view := ApplyRequest(testView(PageSettings), PageRequest{
		Locale: "de-DE", NavCollapsed: true, MenuQuery: "work", FavoritePages: []PageID{PagePeople},
	})
	view.Navigate = func(string) {}
	props := localePreferencesProps(view)
	if len(props.Options) != 3 {
		t.Fatalf("locale options = %d", len(props.Options))
	}
	for _, option := range props.Options {
		for _, want := range []string{"locale=" + option.Code, "nav=collapsed", "menu_q=work", "favorites=people"} {
			if !strings.Contains(option.Href, want) {
				t.Fatalf("%s locale href lost %q: %s", option.Code, want, option.Href)
			}
		}
		if option.Navigate == nil {
			t.Fatalf("%s locale option has no software-navigation callback", option.Code)
		}
	}
	if !props.Options[1].Current || props.Options[0].Current || props.Options[2].Current {
		t.Fatalf("current locale projection = %+v", props.Options)
	}
}

func TestLocalePreferencesPanelIsAccessibleAndDirectionAware(t *testing.T) {
	view := ApplyRequest(testView(PageSettings), PageRequest{Locale: "en-US"})
	markup, err := ui.RenderToString(ui.CreateElement(LocalePreferencesPanel, localePreferencesProps(view)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`aria-labelledby="locale-preferences-title"`, `aria-current="page"`,
		`dir="ltr" lang="en-US"`, `dir="rtl" lang="ar"`,
		`href="/workspace/app/settings?locale=de-DE"`, `>Language &amp; region</h3>`, `>Current</span>`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("locale preferences missing %q in %s", want, markup)
		}
	}
}

func TestSettingsPageSubtitleIsLocalized(t *testing.T) {
	german := ApplyRequest(testView(PageSettings), PageRequest{Locale: "de-DE"})
	if german.Subtitle != "Verwalten Sie Sprache, Barrierefreiheit und persönliche Einstellungen." {
		t.Fatalf("German settings subtitle = %q", german.Subtitle)
	}
	arabic := ApplyRequest(testView(PageSettings), PageRequest{Locale: "ar"})
	if arabic.Subtitle != "أدر لغتك وإعدادات إمكانية الوصول وتفضيلاتك الشخصية." {
		t.Fatalf("Arabic settings subtitle = %q", arabic.Subtitle)
	}
}
