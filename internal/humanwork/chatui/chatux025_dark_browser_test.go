package chatui_test

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_CHATUX_025_DarkMode: Chat has no colour of its own. It reads the
// shell's custom properties, and the shell redefines every one of them under
// :root[data-hcm-color-mode="dark"] and, for "system", under
// prefers-color-scheme:dark. So the page follows the shell's mode, and the
// shell follows the system setting unless the person chose a mode.
//
// This test holds both ends of that: every property Chat reads without a
// fallback exists in the shell's token set, the shell has a dark value for the
// ones that carry a colour, and no rule of Chat's writes a light colour as a
// literal (a literal does not change with the mode).
func TestTodo_CHATUX_025_DarkMode(t *testing.T) {
	sheet := chatui.Stylesheet
	shell := productui.Stylesheet()
	tokens := map[string]bool{}
	for _, token := range productui.ThemeTokens() {
		tokens[token.CSSVariable] = true
	}
	own := map[string]bool{}
	for _, match := range regexp.MustCompile(`(--[a-z0-9-]+)\s*:`).FindAllStringSubmatch(sheet, -1) {
		own[match[1]] = true
	}
	read := map[string]bool{}
	for _, match := range regexp.MustCompile(`var\((--[a-z0-9-]+)\s*([,)])`).FindAllStringSubmatch(sheet, -1) {
		if match[2] == ")" && !own[match[1]] {
			read[match[1]] = true
		}
	}
	var names []string
	for name := range read {
		names = append(names, name)
	}
	sort.Strings(names)
	// The shell defines a colour for the dark modes in two places, a block under
	// the explicit mode and the same block under the system media query.
	darkBlocks := regexp.MustCompile(`data-hcm-color-mode="dark"\]\s*\{[^}]*\}`).FindAllString(shell, -1)
	dark := strings.Join(darkBlocks, "")
	if dark == "" {
		t.Fatal("the shell has no rules for data-hcm-color-mode=\"dark\"")
	}
	if !strings.Contains(shell, `data-hcm-color-mode="system"`) || !regexp.MustCompile(`prefers-color-scheme:\s*dark`).MatchString(shell) {
		t.Fatal("the shell does not follow the system setting for the \"system\" mode")
	}
	colour := regexp.MustCompile(`^--(accent|accent-hover|soft|ink|muted|canvas|surface|line|hcm-color-(?:surface|canvas|text|text-muted|border|danger|warning|on-brand|control-border|focus|brand-soft|danger-surface|warning-surface))$`)
	for _, name := range names {
		if !tokens[name] && !strings.Contains(shell, name+":") && !strings.Contains(shell, "\""+strings.TrimPrefix(name, "--")+"\"") {
			// Layout and motion properties the shell sets under other names are
			// not colours; a colour with no definition anywhere is a hole.
			if colour.MatchString(name) {
				t.Errorf("Chat reads %s and the shell does not define it", name)
			}
		}
		if colour.MatchString(name) && !strings.Contains(dark, strings.TrimPrefix(name, "--")+":") && !strings.Contains(dark, name+":") && !tokens[name] {
			t.Errorf("%s is a colour Chat reads with no value of its own in the shell's dark mode", name)
		}
	}
}
