package productui

import (
	"regexp"
	"strings"
	"testing"
)

func TestTodo_UIPOLISH_003_ProductionShapeSemantics(t *testing.T) {
	css := Stylesheet()
	for _, rule := range []struct {
		name    string
		pattern string
	}{
		{"resting surface", `:where\(\.app-shell\) :is\(\.surface,\.panel,\.people-workspace,\.settings-shell,\.metric,\.choice,\.org-node\)\{[^}]*box-shadow:none`},
		{"focused surface", `:where\(\.app-shell\) :is\(\.surface,\.panel,\.people-workspace,\.settings-shell,\.metric,\.choice,\.org-node\):focus-within\{[^}]*box-shadow:var\(--hcm-focus-ring\)`},
		{"search overlay", `\.global-search-panel\{[^}]*box-shadow:var\(--hcm-shadow-raised\)`},
		{"search input", `\.global-search-input\{[^}]*box-shadow:none`},
		{"search focus", `\.global-search-input:focus\{[^}]*box-shadow:var\(--hcm-focus-ring\)`},
		{"popover overlay", `\.popover-surface\{[^}]*box-shadow:var\(--hcm-shadow-raised`},
		{"preview surface", `\.mini-page\{[^}]*box-shadow:none`},
		{"button boundary", `\.button\{[^}]*border:1px solid var\(--control-border\)`},
		{"select boundary", `\.settings-form select\{[^}]*border:1px solid var\(--control-border\)`},
		{"status shape definition", `:root\{[^}]*--hcm-radius-status:var\(--hcm-radius-control\)`},
		{"navigation count shape", `\.nav-count\{[^}]*border-radius:var\(--hcm-radius-status\)`},
		{"count shape", `\.count\{[^}]*border-radius:var\(--hcm-radius-status\)`},
		{"status shape", `\.status\{[^}]*border-radius:var\(--hcm-radius-status\)`},
	} {
		if !regexp.MustCompile(rule.pattern).MatchString(css) {
			t.Errorf("production CSS lost %s semantics", rule.name)
		}
	}
	if strings.Contains(css, "0 22px 60px color-mix") {
		t.Error("search overlay still has a bespoke elevation")
	}
	for _, selector := range []string{
		`.loading-panel`,
		`.appearance-preview-card`,
		`.surface:hover`,
		`.jn-embedded .jn-btn[data-variant="primary"]`,
	} {
		pattern := regexp.QuoteMeta(selector) + `\{[^}]*box-shadow:var\(--hcm-shadow-resting\)`
		if regexp.MustCompile(pattern).MatchString(css) {
			t.Errorf("ordinary surface still uses elevation: %q", selector)
		}
	}
}

func TestTodo_UIPOLISH_003_StatusShapeFollowsCustomerControlRadius(t *testing.T) {
	for _, radius := range []string{"4px", "12px", "1rem"} {
		css, err := StylesheetForTheme(map[string]string{"radius.control": radius})
		if err != nil {
			t.Fatalf("radius %s: %v", radius, err)
		}
		if !strings.Contains(css, "--hcm-radius-control:"+radius+";") ||
			!strings.Contains(css, "--hcm-radius-status:var(--hcm-radius-control)") {
			t.Errorf("status radius no longer follows admitted customer radius %s", radius)
		}
	}
}
