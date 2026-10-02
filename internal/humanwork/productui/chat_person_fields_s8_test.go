package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Person details for a fully seeded person prints the department by name and
// never "Not available", in every shipped chat locale.
func TestChatPersonDetailsSeededInEveryLocale(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			m := chatui.Model{State: chatui.StateReady, ShowPerson: true, Locale: locale,
				PersonDetails: &chatui.PersonDetails{ID: "greg", Name: "Greg Novak", JobTitle: "Project Manager", Manager: "Priya Raman", ManagerID: "priya",
					Department: "Project Management", Phone: "(303) 555-0105", Email: "greg.novak@ironridge.example", Location: "Denver, CO",
					Company: "Ironridge Builders, Inc.", BusinessUnit: "Operations", Ready: true},
				Callbacks: chatui.Callbacks{OpenPerson: func(string) {}, ClosePerson: func() {}},
			}
			if locale != "en-US" {
				table := chatTranslations(locale)
				m.Text = func(key string) string { return table[key] }
			}
			markup, err := ui.RenderToString(chatui.Build(m))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"Project Management", "(303) 555-0105", "greg.novak@ironridge.example"} {
				if !strings.Contains(markup, want) {
					t.Errorf("%s: missing %q", locale, want)
				}
			}
			// Matched as a whole element value: other parts of the page carry
			// longer phrases that contain the same words.
			for _, bad := range []string{chatui.EnglishCopy()[chatui.KeyNotAvailable], chatTranslations(locale)[chatui.KeyNotAvailable], "project-management"} {
				if bad != "" && strings.Contains(markup, ">"+bad+"<") {
					t.Errorf("%s: printed %q", locale, bad)
				}
			}
			if locale != "en-US" && !strings.Contains(markup, chatTranslations(locale)[chatui.KeyDepartment]) {
				t.Errorf("%s: department label not translated", locale)
			}
		})
	}
}
