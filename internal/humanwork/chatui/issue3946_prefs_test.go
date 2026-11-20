package chatui

import (
	"regexp"
	"strings"
	"testing"
)

func issue3946Names(t *testing.T, locale string) (groups, switches map[string]string, markup string) {
	t.Helper()
	admin := chattoneRewordMember
	admin.CanAdmin = true
	admin.ChoiceAllowed = false
	markup = chattoneRewordMarkup(t, chattoneRewordProbeProps{Locale: locale, View: admin})
	// The text an aria-labelledby reference resolves to.
	text := func(id string) string {
		m := regexp.MustCompile(`id="` + regexp.QuoteMeta(id) + `"[^>]*>([^<]*)<`).FindStringSubmatch(markup)
		if m == nil {
			t.Fatalf("%s: aria reference %q points at nothing: %s", locale, id, markup)
		}
		return strings.TrimSpace(m[1])
	}
	name := func(tag string) string {
		ref := regexp.MustCompile(`aria-labelledby="([^"]*)"`).FindStringSubmatch(tag)
		if ref == nil {
			t.Fatalf("%s: no aria-labelledby on %s", locale, tag)
		}
		parts := []string{}
		for _, id := range strings.Fields(ref[1]) {
			parts = append(parts, text(id))
		}
		return strings.Join(parts, " ")
	}
	groups, switches = map[string]string{}, map[string]string{}
	for _, m := range regexp.MustCompile(`<div[^>]*id="(workspace|channel)-mode"[^>]*>`).FindAllStringSubmatch(markup, -1) {
		groups[m[1]] = name(m[0])
	}
	for _, m := range regexp.MustCompile(`<button[^>]*id="(workspace|channel)-view"[^>]*>`).FindAllStringSubmatch(markup, -1) {
		switches[m[1]] = name(m[0])
	}
	return groups, switches, markup
}

// TestIssue3946_ScopeIsPartOfTheAccessibleName: the workspace and channel
// choice groups and switches no longer share one name; each carries its visible
// scope heading as well as the setting name.
func TestIssue3946_ScopeIsPartOfTheAccessibleName(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		groups, switches, _ := issue3946Names(t, locale)
		for _, scope := range []string{"workspace", "channel"} {
			for kind, got := range map[string]string{"group": groups[scope], "switch": switches[scope]} {
				setting := ChattoneRewordText(locale, "mode")
				if kind == "switch" {
					setting = ChattoneRewordText(locale, "view")
				}
				if !strings.Contains(got, ChattoneRewordText(locale, scope)) || !strings.Contains(got, setting) {
					t.Errorf("%s %s %s: accessible name %q lacks its scope or setting", locale, scope, kind, got)
				}
			}
		}
		if groups["workspace"] == groups["channel"] || switches["workspace"] == switches["channel"] {
			t.Errorf("%s: the two scopes still share a name: %v %v", locale, groups, switches)
		}
	}
}
