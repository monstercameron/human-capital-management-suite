package productui

import (
	"regexp"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// #3772: the number in a count pill is legible in both themes, for the neutral
// fill and for the accent fill of mentions and direct messages.
func TestChatDesign_3772_CountPillsMeetContrast(t *testing.T) {
	neutral := regexp.MustCompile(`--chat-pill-neutral:([^;}]+)`).FindStringSubmatch(chatui.Stylesheet)
	if neutral == nil {
		t.Fatal("the Chat stylesheet does not declare --chat-pill-neutral")
	}
	for _, theme := range []struct {
		name string
		dark bool
	}{{"light", false}, {"dark", true}} {
		vars := chatDesignThemeVars(t, theme.dark)
		for _, pair := range []struct{ name, ink, fill string }{
			{"neutral pill", "var(--ink)", neutral[1]},
			{"accent pill", "var(--hcm-color-on-brand)", "var(--accent)"},
		} {
			if ratio := chatDesignContrast(t, vars, pair.ink, pair.fill); ratio < 4.5 {
				t.Errorf("%s %s: the count is %.2f:1 on its fill, below 4.5:1", theme.name, pair.name, ratio)
			}
		}
		// The two fills are told apart, and the neutral one is visible on the list.
		if ratio := chatDesignContrast(t, vars, neutral[1], "var(--accent)"); ratio < 1.5 {
			t.Errorf("%s: the neutral and the accent pill are %.2f:1 apart", theme.name, ratio)
		}
		if ratio := chatDesignContrast(t, vars, neutral[1], "var(--surface)"); ratio < 1.15 {
			t.Errorf("%s: the neutral pill is %.2f:1 against the list surface", theme.name, ratio)
		}
	}
}
