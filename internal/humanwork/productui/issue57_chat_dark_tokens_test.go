package productui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Issue 57 (and 59): in the dark shell the Chat settings lower panels (Location
// sharing, Voice messages) and the search result cards read the registered
// --hcm-color-* tokens directly. If the dark block only re-points the legacy
// --ink/--surface variables, those components keep the light values: white
// panels with black text, near-black author names on a dark card.
func TestIssue57_DarkModeOverridesEveryNeutralTokenChatReadsDirectly(t *testing.T) {
	css := darkModeStylesStylesheet()
	explicit := ruleBody(t, css, `:root[data-hcm-color-mode="dark"]{`)
	system := ruleBody(t, css, `@media (prefers-color-scheme:dark){:root[data-hcm-color-mode="system"]{`)

	chat := chatui.ScopedStylesheet() + chatui.TranslationAdminPageStyles + ChatRetentionStylesheet()
	used := map[string]bool{}
	for _, m := range regexp.MustCompile(`var\((--hcm-color-[a-z-]+)`).FindAllStringSubmatch(chat, -1) {
		used[m[1]] = true
	}
	// Brand colours are re-derived through --accent/--soft; every other colour
	// token a chat surface reads must carry a dark value of its own.
	for _, token := range registeredThemeTokens {
		if token.Kind != ThemeColor || !used[token.CSSVariable] {
			continue
		}
		switch token.CSSVariable {
		case "--hcm-color-brand-primary", "--hcm-color-brand-hover":
			continue
		}
		for name, body := range map[string]string{"explicit dark": explicit, "system dark": system} {
			if !strings.Contains(body, token.CSSVariable+":") {
				t.Errorf("%s palette leaves %s at its light value although chat reads it directly", name, token.CSSVariable)
			}
		}
	}
	for _, must := range []string{"--hcm-color-text", "--hcm-color-text-muted", "--hcm-color-surface", "--hcm-color-canvas", "--hcm-color-border"} {
		if !used[must] {
			t.Errorf("expected chat styles to read %s; the guard above would not cover it", must)
		}
	}
}

func TestIssue57_DarkModeTextIsReadableOnChatSurfaces(t *testing.T) {
	// Dark values from darkModeDeclarationRules for text, muted text, surface and canvas.
	body := ruleBody(t, darkModeStylesStylesheet(), `:root[data-hcm-color-mode="dark"]{`)
	value := func(name string) string {
		m := regexp.MustCompile(`--` + name + `:(#[0-9a-fA-F]{6})`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("dark palette has no hex value for --%s", name)
		}
		return m[1]
	}
	for _, pair := range [][2]string{{"ink", "surface"}, {"ink", "canvas"}, {"muted", "surface"}, {"muted", "canvas"}} {
		ratio, err := contrastRatio(value(pair[0]), value(pair[1]))
		if err != nil || ratio < 4.5 {
			t.Errorf("dark --%s on --%s = %.2f:1, want >= 4.5:1 (%v)", pair[0], pair[1], ratio, err)
		}
	}
	// The direct tokens must resolve to those same variables, not to a light constant.
	for token, want := range map[string]string{
		"hcm-color-text": "var(--ink)", "hcm-color-text-muted": "var(--muted)",
		"hcm-color-surface": "var(--surface)", "hcm-color-canvas": "var(--canvas)", "hcm-color-border": "var(--line)",
	} {
		if !strings.Contains(body, "--"+token+":"+want) {
			t.Errorf("dark palette must map --%s to %s", token, want)
		}
	}
}

// Search results and the settings panels must not hard-code a light colour.
func TestIssue59_ChatSearchAndSettingsStylesUseTokensOnly(t *testing.T) {
	chat := chatui.ScopedStylesheet() + chatui.TranslationAdminPageStyles
	rule := regexp.MustCompile(`[^{}]*(\.search-result|\.chatlangadmin|\.chatvoice-settings)[^{}]*\{([^{}]*)\}`)
	hardcoded := regexp.MustCompile(`(^|;)\s*(color|background|background-color)\s*:\s*(#[0-9a-fA-F]{3,8}|white|black|rgba?\()`)
	for _, m := range rule.FindAllStringSubmatch(chat, -1) {
		if hardcoded.MatchString(m[2]) {
			t.Errorf("rule %q hard-codes a colour that cannot follow the dark theme: %s", strings.TrimSpace(m[0]), m[2])
		}
	}
}

func ruleBody(t *testing.T, css, open string) string {
	t.Helper()
	start := strings.Index(css, open)
	if start < 0 {
		t.Fatalf("stylesheet has no %q block", open)
	}
	rest := css[start+len(open):]
	end := strings.Index(rest, "}")
	if end < 0 {
		t.Fatalf("unterminated %q block", open)
	}
	return rest[:end]
}
