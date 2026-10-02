package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// TestTodo_CHATUX_025_Literals: no rule of Chat's writes a colour as a literal
// that would stay light when the shell goes dark. A colour is a custom
// property of the shell, or a literal only where it belongs to a fixed
// surface in both modes: a mask (only its alpha is used), a scrim over the
// page (the dark modes have their own), and white text on the dark badge laid
// over a photograph.
func TestTodo_CHATUX_025_Literals(t *testing.T) {
	allowed := map[string]string{
		".rail-scroll":          "mask",
		".message-list":         "mask",
		".attachment-badge":     "white on a dark scrim over a photograph, in both modes",
		".chat-dialog-backdrop": "scrim; dark values follow",
		".chat-workspace .chatremove-overlay.chatremove-overlay-dialog": "scrim; dark values follow",
	}
	literal := regexp.MustCompile(`(#[0-9a-fA-F]{3,8}\b|rgba?\([^)]*\)|hsla?\([^)]*\)|(?:^|[\s:,(])(?:white|black)\b)`)
	fallback := regexp.MustCompile(`var\(--[a-z0-9-]+,\s*(?:[^()]|\([^()]*\))*\)`)
	for _, rule := range agentux058Rules(Stylesheet) {
		selector := strings.TrimSpace(rule[0])
		// A rule scoped to a dark mode is where a dark literal belongs.
		if strings.Contains(selector, `data-hcm-color-mode="dark"`) || strings.Contains(selector, `data-hcm-color-mode="system"`) {
			continue
		}
		body := fallback.ReplaceAllString(rule[1], "var()")
		for _, declaration := range strings.Split(body, ";") {
			name, value, ok := strings.Cut(declaration, ":")
			name = strings.TrimSpace(name)
			if !ok || strings.Contains(name, "mask") {
				continue
			}
			if literal.MatchString(value) {
				if _, fine := allowed[selector]; fine {
					continue
				}
				t.Errorf("%s{%s:%s} writes a colour as a literal: it does not follow the shell's mode", selector, name, strings.TrimSpace(value))
			}
		}
	}
	// The mention badge label follows the mode.
	if got := chatbugCascadeValue(Stylesheet, ".chat-badge.mention", "color"); !strings.Contains(got, "--hcm-color-on-brand") {
		t.Errorf("the mention badge label is %q", got)
	}
}
