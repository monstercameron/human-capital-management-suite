package chatui

import (
	"strings"
	"testing"
)

// The profile card tells a member what data an agent can reach before they ask
// it. Every data class the server names must be on the card in plain words; a
// class left off makes the agent look narrower than it is.
func TestTodo_AGENTP_019_ProfileCardNamesEveryDataClass(t *testing.T) {
	persona := ResolvedPersonaMention{
		Reference:   ChatReference{Display: "Comp Analyst"},
		DataClasses: []string{"POLICY_DOCUMENT", "COMPENSATION", "WORKFORCE", "SCHEDULE", "ONBOARDING", "INTERNAL", "PUBLIC", "COMPENSATION", "PAYROLL_LEDGER", "Team calendars"},
	}
	markup := renderNode(t, personaProfileCard("en-US", persona))
	for _, want := range []string{
		"Reads policy documents", "Compensation information", "Workforce records", "Schedules", "Onboarding records",
		"Internal information", "Public information", "Payroll ledger", "Team calendars",
	} {
		if !strings.Contains(markup, "<li>"+want+"</li>") {
			t.Errorf("profile card does not name %q: %s", want, markup)
		}
	}
	// The registry's identifiers are not words a member reads.
	for _, raw := range []string{"COMPENSATION", "WORKFORCE", "PAYROLL_LEDGER", "POLICY_DOCUMENT"} {
		if strings.Contains(markup, raw) {
			t.Errorf("profile card shows the identifier %q: %s", raw, markup)
		}
	}
	if got := strings.Count(markup, "Compensation information"); got != 1 {
		t.Errorf("a data class named twice by the server is listed %d times", got)
	}
	if strings.Contains(markup, "Not provided") && len(persona.DataClasses) > 0 {
		// "Not provided" belongs to the other, empty sections only.
		section := markup[strings.Index(markup, "Data this agent can reach"):]
		section = section[:strings.Index(section, "</section>")]
		if strings.Contains(section, "Not provided") {
			t.Errorf("data section says nothing is provided: %s", section)
		}
	}
	for locale, want := range map[string][]string{
		"de-DE": {"Vergütungsinformationen", "Personaldaten", "Dienstpläne"},
		"ar":    {"معلومات التعويضات", "سجلات القوى العاملة", "جداول العمل"},
	} {
		localized := renderNode(t, personaProfileCard(locale, persona))
		for _, text := range want {
			if !strings.Contains(localized, text) {
				t.Errorf("%s profile card does not name %q: %s", locale, text, localized)
			}
		}
	}
}
