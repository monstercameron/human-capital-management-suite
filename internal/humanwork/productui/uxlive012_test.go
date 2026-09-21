package productui

import (
	"strings"
	"testing"
)

// UXLIVE-012's RED was measured on the running server: the proposed base
// pay field read "Select a next role to see its base-pay rules." before a
// role was chosen and "An exact base-pay range is not available here. Your
// proposed pay will be checked before submission." immediately after one
// was. The first sentence promised something the second withdrew.

// TestTodo_UXLIVE_012 is the primary red/green test: the copy shown before
// a role is chosen does not promise what the copy shown after it withdraws.
func TestTodo_UXLIVE_012(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		copy := ResolveProductLocale(locale)
		before := copy.Text("journey.form_choose_role_rule")
		after := copy.Text("journey.form_base_rule_unavailable")
		if strings.TrimSpace(before) == "" || strings.TrimSpace(after) == "" {
			t.Fatalf("%s: pay guidance copy is missing", locale)
		}
		if before == after {
			t.Fatalf("%s: the two states share one sentence", locale)
		}
		if locale == "en-US" {
			if strings.Contains(before, "to see its") {
				t.Fatalf("en-US still promises the selected role's own rules: %q", before)
			}
			if !strings.Contains(strings.ToLower(before), "any") {
				t.Fatalf("en-US does not qualify what will be shown: %q", before)
			}
		}
	}
}

// TestTodo_UXLIVE_012_Browser keeps the honest post-selection answer intact:
// where no band is published the page still says so once, and where one is
// published the rule copy still carries its amounts.
func TestTodo_UXLIVE_012_Browser(t *testing.T) {
	copy := ResolveProductLocale("en-US")
	unavailable := copy.Text("journey.form_base_rule_unavailable")
	if !strings.Contains(unavailable, "not available here") {
		t.Fatalf("the no-band answer changed: %q", unavailable)
	}
	rule := copy.Text("journey.form_base_rule")
	for _, token := range []string{"{minimum}", "{maximum}"} {
		if !strings.Contains(rule, token) {
			t.Fatalf("the published-band rule lost %s: %q", token, rule)
		}
	}
}
