package chatui_test

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatbug042Rules returns the declarations of every rule in css whose selector
// satisfies keep.
func chatbug042Rules(css string, keep func(selector string) bool) map[string]string {
	out := map[string]string{}
	for _, match := range regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`).FindAllStringSubmatch(css, -1) {
		selector := strings.TrimSpace(match[1])
		if keep(selector) {
			out[selector] += match[2] + ";"
		}
	}
	return out
}

// chatbug042Channel is one 0-255 channel as the WCAG linear value.
func chatbug042Channel(hex string) float64 {
	v, _ := strconv.ParseUint(hex, 16, 8)
	c := float64(v) / 255
	if c <= 0.03928 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func chatbug042Luminance(color string) float64 {
	color = strings.TrimPrefix(color, "#")
	return 0.2126*chatbug042Channel(color[0:2]) + 0.7152*chatbug042Channel(color[2:4]) + 0.0722*chatbug042Channel(color[4:6])
}

func chatbug042Contrast(a, b string) float64 {
	la, lb := chatbug042Luminance(a), chatbug042Luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func TestTodo_CHATBUG_042(t *testing.T) {
	sheet := chatui.Stylesheet
	hardCoded := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|\brgba?\(|\bhsla?\(|\bhwb\(|\blab\(|\blch\(`)

	// 1. One rule draws every Chat switch, from the shell tokens alone.
	switchRules := chatbug042Rules(sheet, func(selector string) bool {
		return strings.Contains(selector, ".switch") || strings.Contains(selector, "-switch")
	})
	if len(switchRules) == 0 {
		t.Fatal("no switch rule in the Chat stylesheet")
	}
	for selector, declarations := range switchRules {
		if hardCoded.MatchString(declarations) {
			t.Errorf("the switch rule %q hard-codes a colour: %s", selector, hardCoded.FindString(declarations))
		}
	}
	for _, want := range []string{
		":root .chat-workspace :is(.switch,.chatmod-switch-track){",
		"border:2px solid var(--hcm-color-text-muted)",
		"background:var(--hcm-color-surface)",
		`:is(.switch:checked,.chatmod-switch[aria-checked="true"] .chatmod-switch-track){border-color:var(--accent);background:var(--accent)}`,
	} {
		if !strings.Contains(chatui.ChatBug042Styles, want) {
			t.Errorf("the switch rule does not hold %q", want)
		}
	}
	if !strings.Contains(sheet, chatui.ChatBug042Styles) {
		t.Fatal("the switch rule is not part of the Chat stylesheet")
	}
	// It outranks the older per-feature switch rules, which stay as they were.
	if strings.Index(sheet, chatui.ChatBug042Styles) < strings.LastIndex(sheet, ".chat-workspace .switch{") {
		t.Error("the one switch rule comes before an older switch rule and could lose to it")
	}

	// 2. Both colour modes, every palette the shell has: the edge and the thumb (the
	// muted text colour) are at least 3:1 against the surface they sit on, and so is
	// the accent a switch is filled with when on. The tokens are read from the
	// stylesheet the shell serves: the light values from each palette's own block,
	// the dark values from the preview scene of each palette in the dark mode.
	theme := productui.DefaultCustomerTheme()
	theme.ColorMode = "dark"
	css, err := productui.StylesheetForCustomerTheme(theme)
	if err != nil {
		t.Fatal(err)
	}
	tokens := regexp.MustCompile(`([^{}]*)\{([^{}]*--hcm-color-text-muted:[^{}]*)\}`)
	value := func(block, token string) string {
		found := regexp.MustCompile(token + `:\s*(#[0-9a-fA-F]{6})`).FindStringSubmatch(block)
		if found == nil {
			return ""
		}
		return found[1]
	}
	checked := map[string]int{}
	for _, match := range tokens.FindAllStringSubmatch(css, -1) {
		// Blocks that name system colours (forced colours) hold no hex values.
		if value(match[2], "--hcm-color-text-muted") == "" || value(match[2], "--hcm-color-surface") == "" || value(match[2], "--hcm-color-brand-primary") == "" {
			continue
		}
		mode := "light"
		if strings.Contains(match[1], `data-hcm-preview-color-mode="dark"`) {
			mode = "dark"
		}
		scene := strings.TrimSpace(match[1])
		edge, surface, accent := value(match[2], "--hcm-color-text-muted"), value(match[2], "--hcm-color-surface"), value(match[2], "--hcm-color-brand-primary")
		if ratio := chatbug042Contrast(edge, surface); ratio < 3 {
			t.Errorf("%s mode, %s: an off switch (%s on %s) is %.2f:1, want 3:1 or more", mode, scene, edge, surface, ratio)
		}
		if ratio := chatbug042Contrast(accent, surface); ratio < 3 {
			t.Errorf("%s mode, %s: an on switch (%s against %s) is %.2f:1, want 3:1 or more", mode, scene, accent, surface, ratio)
		}
		checked[mode]++
	}
	if checked["light"] == 0 || checked["dark"] == 0 {
		t.Fatalf("the contrast was checked for %v blocks, want both modes", checked)
	}

	// 3. The shell's dark colour mode redraws every unchecked checkbox with a more
	// specific selector than the switch rule (which made an off switch a dark
	// rectangle on a dark panel); the dark rules say the switch again with more
	// specificity, for the dark mode and for the system mode on a dark device.
	for _, want := range []string{
		`:root[data-hcm-color-mode="dark"] .chat-workspace input.switch:not(:checked){`,
		`@media(prefers-color-scheme:dark){:root[data-hcm-color-mode="system"] .chat-workspace input.switch:not(:checked){`,
	} {
		if !strings.Contains(chatui.ChatBug042Styles, want) {
			t.Errorf("the dark mode rule is missing: %s", want)
		}
	}
}

func TestTodo_CHATBUG_042_Browser(t *testing.T) {
	// The switches Chat draws, rendered with the real product catalog in all three
	// languages, print no copy key and carry the one switch class.
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		page := chatbug045Panel(t, locale, true)
		if strings.Contains(page, "⟦") {
			t.Errorf("%s: the preferences panel prints a copy key", locale)
		}
		if !strings.Contains(page, `class="switch"`) || !strings.Contains(page, `role="switch"`) {
			t.Errorf("%s: Quiet hours is not drawn as a switch", locale)
		}
		// The translate choice in the reading settings is a switch as well.
		if box := regexp.MustCompile(`<input[^>]*id="chatrender-translate"[^>]*>`).FindString(page); !strings.Contains(box, `class="switch"`) {
			t.Errorf("%s: the translate choice is not drawn as a switch: %s", locale, box)
		}
	}
}
